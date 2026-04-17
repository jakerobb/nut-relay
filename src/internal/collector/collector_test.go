package collector

import (
	"testing"

	"github.com/jakerobb/nut-influx-relay/internal/config"
	"github.com/jakerobb/nut-influx-relay/internal/nut"
)

func TestParseVars_FieldTypes(t *testing.T) {
	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
		{NUTVar: "battery.runtime", InfluxField: "battery_runtime_seconds", Type: "int"},
		{NUTVar: "ups.status", InfluxField: "ups_status", Type: "string"},
	}

	vars := nut.VarMap{
		"battery.charge":  "85.5",
		"battery.runtime": "3600.0",
		"ups.status":      "OL CHRG",
		"ups.serial":      "SN12345",
	}

	stats := parseVars(vars, "rack", "cyberpower", mappings)

	if stats.Label != "rack" {
		t.Errorf("label: got %q, want 'rack'", stats.Label)
	}
	if stats.Serial != "SN12345" {
		t.Errorf("serial: got %q, want 'SN12345'", stats.Serial)
	}

	// float
	f, ok := stats.Fields["battery_charge_percent"].(float64)
	if !ok || f != 85.5 {
		t.Errorf("battery_charge_percent: got %v (%T), want float64(85.5)", stats.Fields["battery_charge_percent"], stats.Fields["battery_charge_percent"])
	}

	// int (parsed from float string)
	i, ok := stats.Fields["battery_runtime_seconds"].(int64)
	if !ok || i != 3600 {
		t.Errorf("battery_runtime_seconds: got %v (%T), want int64(3600)", stats.Fields["battery_runtime_seconds"], stats.Fields["battery_runtime_seconds"])
	}

	// string
	s, ok := stats.Fields["ups_status"].(string)
	if !ok || s != "OL CHRG" {
		t.Errorf("ups_status: got %v (%T), want string('OL CHRG')", stats.Fields["ups_status"], stats.Fields["ups_status"])
	}
}

func TestParseVars_MissingNUTVarSkipped(t *testing.T) {
	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
		{NUTVar: "input.voltage", InfluxField: "input_voltage", Type: "float"},
	}

	vars := nut.VarMap{
		"battery.charge": "100",
		// input.voltage intentionally absent
	}

	stats := parseVars(vars, "office", "ups1", mappings)

	if _, ok := stats.Fields["battery_charge_percent"]; !ok {
		t.Error("expected battery_charge_percent in fields")
	}
	if _, ok := stats.Fields["input_voltage"]; ok {
		t.Error("input_voltage should not be present when NUT var is absent")
	}
}

func TestParseVars_UnmappedNUTVarsIgnored(t *testing.T) {
	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
	}

	vars := nut.VarMap{
		"battery.charge":   "90",
		"battery.voltage":  "13.2", // not in mappings
		"ups.manufacturer": "APC",  // not in mappings
	}

	stats := parseVars(vars, "rack", "ups1", mappings)

	if len(stats.Fields) != 1 {
		t.Errorf("expected 1 field, got %d: %v", len(stats.Fields), stats.Fields)
	}
}

func TestParseVars_SerialAlwaysPopulated(t *testing.T) {
	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
		// ups.serial intentionally not in mappings
	}

	vars := nut.VarMap{
		"battery.charge": "95",
		"ups.serial":     "XYZ789",
	}

	stats := parseVars(vars, "rack", "ups1", mappings)

	if stats.Serial != "XYZ789" {
		t.Errorf("serial: got %q, want 'XYZ789'", stats.Serial)
	}
	// Serial should not appear in Fields (it's a tag, not a field)
	if _, ok := stats.Fields["serial"]; ok {
		t.Error("serial should not appear in Fields")
	}
}

func TestParseVars_InvalidFloatSkipped(t *testing.T) {
	mappings := []config.FieldMapping{
		{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
	}

	vars := nut.VarMap{
		"battery.charge": "not-a-number",
	}

	stats := parseVars(vars, "rack", "ups1", mappings)

	if _, ok := stats.Fields["battery_charge_percent"]; ok {
		t.Error("invalid float value should not appear in Fields")
	}
}

func TestParseVars_DefaultMappings(t *testing.T) {
	vars := nut.VarMap{
		"battery.charge":  "100",
		"battery.runtime": "3600",
		"ups.status":      "OL",
		"ups.serial":      "SN001",
		"ups.load":        "12.5",
	}

	stats := parseVars(vars, "rack", "cyberpower", config.DefaultFieldMappings)

	if _, ok := stats.Fields["battery_charge_percent"]; !ok {
		t.Error("expected battery_charge_percent")
	}
	if _, ok := stats.Fields["battery_runtime_seconds"]; !ok {
		t.Error("expected battery_runtime_seconds")
	}
	if _, ok := stats.Fields["ups_status"]; !ok {
		t.Error("expected ups_status")
	}
}
