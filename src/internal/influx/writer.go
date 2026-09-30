package influx

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jakerobb/nut-relay/internal/config"
	"github.com/jakerobb/nut-relay/internal/util"
)

// Writer writes UPS stats to InfluxDB using the v2 line protocol over HTTP.
type Writer struct {
	url         string // full write endpoint URL
	token       string
	measurement string
	mappings    []config.FieldMapping
	client      *http.Client
}

// New creates a new Writer.
func New(baseURL, token, org, bucket, measurement string, mappings []config.FieldMapping) *Writer {
	url := fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns",
		strings.TrimRight(baseURL, "/"), org, bucket)
	return &Writer{
		url:         url,
		token:       token,
		measurement: measurement,
		mappings:    mappings,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Write maps one poll's NUT variables to fields, serializes them to InfluxDB
// line protocol, and POSTs them to InfluxDB. It implements collector.Sink.
func (w *Writer) Write(label string, vars map[string]string, at time.Time) error {
	line := BuildLine(w.measurement, BuildPoint(vars, label, w.mappings, at))
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

// BuildLine builds an InfluxDB line protocol string for the given point.
// Field values must be float64, int64, or string — the types BuildPoint produces.
// Returns an empty string if there are no fields to write.
func BuildLine(measurement string, stats *Point) string {
	// Tags: ups_label, serial (escape special chars)
	tags := fmt.Sprintf(",ups_label=%s,serial=%s",
		escapeTag(stats.Label),
		escapeTag(stats.Serial),
	)

	var fields []string
	for name, value := range stats.Fields {
		switch v := value.(type) {
		case float64:
			fields = append(fields, fmt.Sprintf("%s=%g", name, v))
		case int64:
			fields = append(fields, fmt.Sprintf("%s=%di", name, v))
		case string:
			fields = append(fields, fmt.Sprintf(`%s="%s"`, name, escapeStringField(v)))
		}
	}

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
