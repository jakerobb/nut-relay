package influx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/store"
)

func TestStatusFlags(t *testing.T) {
	tests := []struct {
		status string
		want   int64
	}{
		{"OL", 8},
		{"OB", 16},
		{"LB", 32},
		{"CHRG", 256},
		{"DISCHRG", 512},
		{"OL CHRG", 8 | 256},    // 264
		{"OB LB", 16 | 32},      // 48
		{"OL DISCHRG", 8 | 512}, // 520
		{"", 0},
		{"UNKNOWN", 0},
		{"OL UNKNOWN CHRG", 8 | 256}, // unknown tokens ignored
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			got := StatusFlags(tc.status)
			if got != tc.want {
				t.Errorf("StatusFlags(%q) = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestBuildLine(t *testing.T) {
	ts := time.Date(2025, 4, 17, 12, 0, 0, 0, time.UTC)

	stats := &store.UpsStats{
		Label:          "rack",
		Serial:         "ABC123",
		CollectedAt:    ts,
		Status:         "OL CHRG",
		BatteryCharge:  ptr(100.0),
		BatteryRuntime: ptr(int64(3106)),
		Load:           ptr(10.0),
		InputVoltage:   ptr(123.3),
		OutputVoltage:  ptr(123.3),
	}

	line := BuildLine("upsd", stats)

	if line == "" {
		t.Fatal("BuildLine returned empty string")
	}

	// Check measurement and tags
	if !strings.HasPrefix(line, "upsd,ups_label=rack,serial=ABC123 ") {
		t.Errorf("unexpected prefix: %s", line)
	}

	// Check status_flags = 8|256 = 264
	if !strings.Contains(line, "status_flags=264i") {
		t.Errorf("expected status_flags=264i in: %s", line)
	}

	// Check ups_status field
	if !strings.Contains(line, `ups_status="OL CHRG"`) {
		t.Errorf("expected ups_status field in: %s", line)
	}

	// Check time_left_ns = 3106 * 1e9
	expected := "time_left_ns=3106000000000i"
	if !strings.Contains(line, expected) {
		t.Errorf("expected %s in: %s", expected, line)
	}

	// Check timestamp
	wantTS := ts.UnixNano()
	if !strings.HasSuffix(line, " "+fmt.Sprintf("%d", wantTS)) {
		t.Errorf("expected timestamp %d at end of: %s", wantTS, line)
	}
}

func TestBuildLine_NilFieldsSkipped(t *testing.T) {
	ts := time.Now()
	stats := &store.UpsStats{
		Label:       "office",
		Serial:      "XYZ",
		CollectedAt: ts,
		Status:      "OL",
		// All numeric fields nil
	}

	line := BuildLine("upsd", stats)

	// Should still have ups_status and status_flags
	if !strings.Contains(line, `ups_status="OL"`) {
		t.Errorf("expected ups_status field: %s", line)
	}
	if !strings.Contains(line, "status_flags=8i") {
		t.Errorf("expected status_flags=8i: %s", line)
	}

	// Should NOT contain voltage fields
	if strings.Contains(line, "battery_charge_percent") {
		t.Errorf("unexpected battery_charge_percent: %s", line)
	}
}

func TestBuildLine_TagEscaping(t *testing.T) {
	ts := time.Now()
	stats := &store.UpsStats{
		Label:       "my ups",
		Serial:      "A,B=C",
		CollectedAt: ts,
		Status:      "OL",
	}

	line := BuildLine("upsd", stats)

	if !strings.Contains(line, `ups_label=my\ ups`) {
		t.Errorf("expected escaped label: %s", line)
	}
	if !strings.Contains(line, `serial=A\,B\=C`) {
		t.Errorf("expected escaped serial: %s", line)
	}
}
