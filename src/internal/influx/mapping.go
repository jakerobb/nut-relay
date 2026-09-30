package influx

import (
	"strconv"
	"strings"
	"time"

	"github.com/jakerobb/nut-relay/internal/config"
)

// Point is one UPS poll, mapped to InfluxDB fields.
type Point struct {
	Label       string
	Serial      string
	CollectedAt time.Time
	// Fields contains all configured NUT variable mappings keyed by their
	// InfluxDB field name. Values are float64, int64, or string depending on
	// the field type in the mapping.
	Fields map[string]any
}

// BuildPoint converts a poll's NUT variables into a Point using the
// configured field mappings. ups.serial is always used to populate Serial
// (the InfluxDB tag), regardless of mappings.
func BuildPoint(vars map[string]string, label string, mappings []config.FieldMapping, at time.Time) *Point {
	p := &Point{
		Label:       label,
		CollectedAt: at,
		Serial:      strings.TrimSpace(vars["ups.serial"]),
		Fields:      make(map[string]any),
	}

	for _, m := range mappings {
		rawVal, ok := vars[m.NUTVar]
		if !ok {
			continue
		}
		rawVal = strings.TrimSpace(rawVal)

		switch m.Type {
		case "float":
			if f, err := strconv.ParseFloat(rawVal, 64); err == nil {
				p.Fields[m.InfluxField] = f
			}
		case "int":
			// Parse as float first to handle values like "3600.0"
			if f, err := strconv.ParseFloat(rawVal, 64); err == nil {
				p.Fields[m.InfluxField] = int64(f)
			}
		case "string":
			p.Fields[m.InfluxField] = rawVal
		}
	}

	return p
}
