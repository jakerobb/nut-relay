package influx

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-relay/internal/config"
)

func TestBuildLine(t *testing.T) {
	ts := time.Date(2025, 4, 17, 12, 0, 0, 0, time.UTC)

	stats := &Point{
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
	stats := &Point{
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
	stats := &Point{
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
	stats := &Point{
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

func TestWrite_MapsVarsAndPosts(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
		{NUTVar: "ups.status", InfluxField: "ups_status", Type: "string"},
	}
	w := New(srv.URL+"/", "tok", "home", "telegraf", "upsd", mappings)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	err := w.Write("rack", map[string]string{"battery.charge": "100", "ups.status": "OL", "ups.serial": "SN1", "ups.load": "20"}, at)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if gotPath != "/api/v2/write?org=home&bucket=telegraf&precision=ns" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAuth != "Token tok" {
		t.Errorf("auth: %q", gotAuth)
	}
	for _, want := range []string{"upsd,ups_label=rack,serial=SN1 ", "battery_charge_percent=100", `ups_status="OL"`, fmt.Sprintf(" %d", at.UnixNano())} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body %q missing %q", gotBody, want)
		}
	}
	if strings.Contains(gotBody, "load") {
		t.Errorf("unmapped ups.load written: %s", gotBody)
	}
}

func TestWrite_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	w := New(srv.URL, "bad", "home", "telegraf", "upsd", config.DefaultFieldMappings)
	err := w.Write("rack", map[string]string{"battery.charge": "100"}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected a 401 error, got %v", err)
	}
}
