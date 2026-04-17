package influx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/store"
)

func TestBuildLine(t *testing.T) {
	ts := time.Date(2025, 4, 17, 12, 0, 0, 0, time.UTC)

	stats := &store.UpsStats{
		Label:       "rack",
		Serial:      "ABC123",
		CollectedAt: ts,
		Fields: map[string]any{
			"battery_charge_percent":  float64(100),
			"battery_runtime_seconds": int64(3106),
			"load_percent":            float64(10),
			"input_voltage":           float64(123.3),
			"ups_status":              "OL CHRG",
		},
	}

	line := BuildLine("upsd", stats)

	if line == "" {
		t.Fatal("BuildLine returned empty string")
	}

	// Check measurement and tags
	if !strings.HasPrefix(line, "upsd,ups_label=rack,serial=ABC123 ") {
		t.Errorf("unexpected prefix: %q", line)
	}

	// Check float field
	if !strings.Contains(line, "battery_charge_percent=100") {
		t.Errorf("expected battery_charge_percent=100 in: %s", line)
	}

	// Check int field (must have 'i' suffix)
	if !strings.Contains(line, "battery_runtime_seconds=3106i") {
		t.Errorf("expected battery_runtime_seconds=3106i in: %s", line)
	}

	// Check string field
	if !strings.Contains(line, `ups_status="OL CHRG"`) {
		t.Errorf("expected ups_status field in: %s", line)
	}

	// Check timestamp
	if !strings.HasSuffix(line, " "+fmt.Sprintf("%d", ts.UnixNano())) {
		t.Errorf("expected timestamp %d at end of: %s", ts.UnixNano(), line)
	}
}

func TestBuildLine_EmptyFields(t *testing.T) {
	stats := &store.UpsStats{
		Label:       "office",
		Serial:      "XYZ",
		CollectedAt: time.Now(),
		Fields:      map[string]any{},
	}

	line := BuildLine("upsd", stats)
	if line != "" {
		t.Errorf("expected empty string for empty fields, got: %s", line)
	}
}

func TestBuildLine_TagEscaping(t *testing.T) {
	stats := &store.UpsStats{
		Label:       "my ups",
		Serial:      "A,B=C",
		CollectedAt: time.Now(),
		Fields:      map[string]any{"load_percent": float64(5)},
	}

	line := BuildLine("upsd", stats)

	if !strings.Contains(line, `ups_label=my\ ups`) {
		t.Errorf("expected escaped label: %s", line)
	}
	if !strings.Contains(line, `serial=A\,B\=C`) {
		t.Errorf("expected escaped serial: %s", line)
	}
}

func TestBuildLine_StringFieldEscaping(t *testing.T) {
	stats := &store.UpsStats{
		Label:       "rack",
		Serial:      "S1",
		CollectedAt: time.Now(),
		Fields:      map[string]any{"ups_status": `OL "QUOTED"`},
	}

	line := BuildLine("upsd", stats)

	if !strings.Contains(line, `ups_status="OL \"QUOTED\""`) {
		t.Errorf("expected escaped quotes in string field: %s", line)
	}
}
