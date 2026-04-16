package influx

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/store"
	"github.com/jakerobb/nut-influx-relay/internal/util"
)

// statusFlagMap maps NUT status tokens to their flag values (matching inputs.upsd convention).
var statusFlagMap = map[string]int64{
	"OL":      8,
	"OB":      16,
	"LB":      32,
	"CHRG":    256,
	"DISCHRG": 512,
}

// StatusFlags computes the bitwise OR of all recognized status tokens in the NUT status string.
func StatusFlags(status string) int64 {
	var flags int64
	for _, token := range strings.Fields(status) {
		if f, ok := statusFlagMap[token]; ok {
			flags |= f
		}
	}
	return flags
}

// Writer writes UPS stats to InfluxDB using the v2 line protocol over HTTP.
type Writer struct {
	url         string // full write endpoint URL
	token       string
	measurement string
	client      *http.Client
}

// New creates a new Writer.
func New(baseURL, token, org, bucket, measurement string) *Writer {
	url := fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns",
		strings.TrimRight(baseURL, "/"), org, bucket)
	return &Writer{
		url:         url,
		token:       token,
		measurement: measurement,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Write serializes stats to InfluxDB line protocol and POSTs it to InfluxDB.
func (w *Writer) Write(stats *store.UpsStats) error {
	line := BuildLine(w.measurement, stats)
	if line == "" {
		return nil
	}

	req, err := http.NewRequest(http.MethodPost, w.url, bytes.NewBufferString(line))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+w.token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	slog.Debug("sending request to influxdb", "url", w.url, "command", line)
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("posting to influxdb: %w", err)
	}
	defer util.CloseCleanly(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("influxdb returned %d with body %s", resp.StatusCode, bodyAsString(resp))
	}
	return nil
}

func bodyAsString(resp *http.Response) string {
	buf := new(strings.Builder)
	n, err := io.Copy(buf, resp.Body)
	if err != nil {
		return fmt.Sprintf("failed to read body: %s", err.Error())
	}
	if n == 0 {
		return "[empty]"
	}
	return buf.String()
}

// BuildLine builds an InfluxDB line protocol string for the given UPS stats.
// Returns an empty string if there are no fields to write.
func BuildLine(measurement string, stats *store.UpsStats) string {
	// Tags: ups_label, serial (escape special chars)
	tags := fmt.Sprintf(",ups_label=%s,serial=%s",
		escapeTag(stats.Label),
		escapeTag(stats.Serial),
	)

	var fields []string

	// Numeric fields
	if stats.BatteryCharge != nil {
		fields = append(fields, fmt.Sprintf("battery_charge_percent=%g", *stats.BatteryCharge))
	}
	if stats.BatteryRuntime != nil {
		// Convert seconds to nanoseconds
		fields = append(fields, fmt.Sprintf("time_left_ns=%di", *stats.BatteryRuntime*int64(time.Second)))
	}
	if stats.Load != nil {
		fields = append(fields, fmt.Sprintf("load_percent=%g", *stats.Load))
	}
	if stats.InputVoltage != nil {
		fields = append(fields, fmt.Sprintf("input_voltage=%g", *stats.InputVoltage))
	}
	if stats.OutputVoltage != nil {
		fields = append(fields, fmt.Sprintf("output_voltage=%g", *stats.OutputVoltage))
	}
	if stats.OutputCurrent != nil {
		fields = append(fields, fmt.Sprintf("output_current=%g", *stats.OutputCurrent))
	}
	if stats.OutputPower != nil {
		fields = append(fields, fmt.Sprintf("output_power=%g", *stats.OutputPower))
	}
	if stats.BatteryVoltage != nil {
		fields = append(fields, fmt.Sprintf("battery_voltage=%g", *stats.BatteryVoltage))
	}
	if stats.InputFrequency != nil {
		fields = append(fields, fmt.Sprintf("input_frequency=%g", *stats.InputFrequency))
	}
	if stats.OutputFrequency != nil {
		fields = append(fields, fmt.Sprintf("output_frequency=%g", *stats.OutputFrequency))
	}

	// String field: ups_status
	if stats.Status != "" {
		fields = append(fields, fmt.Sprintf(`ups_status="%s"`, escapeStringField(stats.Status)))
	}

	// Integer field: status_flags
	flags := StatusFlags(stats.Status)
	fields = append(fields, fmt.Sprintf("status_flags=%di", flags))

	if len(fields) == 0 {
		return ""
	}

	timestamp := stats.CollectedAt.UnixNano()
	return fmt.Sprintf("%s%s %s %d", measurement, tags, strings.Join(fields, ","), timestamp)
}

// escapeTag escapes special characters in InfluxDB line protocol tag keys/values.
func escapeTag(s string) string {
	s = strings.ReplaceAll(s, `,`, `\,`)
	s = strings.ReplaceAll(s, `=`, `\=`)
	s = strings.ReplaceAll(s, ` `, `\ `)
	return s
}

// escapeStringField escapes double quotes within a string field value.
func escapeStringField(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
