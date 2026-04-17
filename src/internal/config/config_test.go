package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInterpolateEnvVars(t *testing.T) {
	t.Setenv("TEST_TOKEN", "my-token")
	t.Setenv("NUT_PASS", "secret")

	cfg := &Config{
		PollIntervalString: "10s",
		HTTPPort:           8080,
		InfluxDB: InfluxDB{
			URL:         "http://influxdb:8086",
			Token:       "${TEST_TOKEN}",
			Org:         "home",
			Bucket:      "telegraf",
			Measurement: "upsd",
		},
		UPSes: []UPSConfig{
			{
				Label:    "rack",
				Host:     "nut-upsd",
				Port:     3493,
				UPSName:  "cyberpower",
				Password: "${NUT_PASS}",
			},
		},
	}

	cfg.interpolateEnvVars()

	if cfg.InfluxDB.Token != "my-token" {
		t.Errorf("expected token 'my-token', got %q", cfg.InfluxDB.Token)
	}
	if cfg.UPSes[0].Password != "secret" {
		t.Errorf("expected password 'secret', got %q", cfg.UPSes[0].Password)
	}
}

func TestInterpolateUnsetVar(t *testing.T) {
	err := os.Unsetenv("UNSET_VAR")
	if err != nil {
		t.Errorf("failed to unset env var: %s", err.Error())
	}
	result := interpolate("prefix_${UNSET_VAR}_suffix")
	if result != "prefix__suffix" {
		t.Errorf("expected 'prefix__suffix', got %q", result)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("INFLUXDB_TOKEN", "tok123")
	t.Setenv("NUT_USER", "admin")
	t.Setenv("NUT_PASSWORD", "pass456")

	content := `
poll_interval: 10s
http_port: 8080
influxdb:
  url: http://influxdb:8086
  token: ${INFLUXDB_TOKEN}
  org: home
  bucket: telegraf
  measurement: upsd
upses:
  - label: rack
    host: nut-upsd
    port: 3493
    ups_name: cyberpower
    tls: false
    username: ${NUT_USER}
    password: ${NUT_PASSWORD}
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.InfluxDB.Token != "tok123" {
		t.Errorf("token: got %q, want 'tok123'", cfg.InfluxDB.Token)
	}
	if cfg.UPSes[0].Username != "admin" {
		t.Errorf("username: got %q, want 'admin'", cfg.UPSes[0].Username)
	}
	if cfg.UPSes[0].Password != "pass456" {
		t.Errorf("password: got %q, want 'pass456'", cfg.UPSes[0].Password)
	}
}

func TestFieldMappings_Explicit(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
field_mappings:
  - nut_var: battery.charge
    influx_field: batt_pct
    type: float
  - nut_var: ups.status
    influx_field: status
    type: string
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.FieldMappings) != 2 {
		t.Fatalf("expected 2 field mappings, got %d", len(cfg.FieldMappings))
	}
	if cfg.FieldMappings[0].NUTVar != "battery.charge" || cfg.FieldMappings[0].InfluxField != "batt_pct" || cfg.FieldMappings[0].Type != "float" {
		t.Errorf("unexpected first mapping: %+v", cfg.FieldMappings[0])
	}
}

func TestFieldMappings_Defaults(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.FieldMappings) != len(DefaultFieldMappings) {
		t.Errorf("expected %d default mappings, got %d", len(DefaultFieldMappings), len(cfg.FieldMappings))
	}
}

func TestFieldMappings_InvalidType(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
field_mappings:
  - nut_var: battery.charge
    influx_field: batt_pct
    type: badtype
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPath(tmp)
	if err == nil {
		t.Error("expected error for invalid field type, got nil")
	}
}

func TestFieldMappings_DefaultTypeIsString(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
field_mappings:
  - nut_var: ups.status
    influx_field: ups_status
    # no type — should default to string
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.FieldMappings[0].Type != "string" {
		t.Errorf("expected default type 'string', got %q", cfg.FieldMappings[0].Type)
	}
}

func TestFieldMappings_ExtraAppendsToDefaults(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
extra_field_mappings:
  - nut_var: ups.realpower
    influx_field: real_power_watts
    type: float
  - nut_var: ups.power
    influx_field: apparent_power_va
    type: float
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	wantLen := len(DefaultFieldMappings) + 2
	if len(cfg.FieldMappings) != wantLen {
		t.Errorf("expected %d mappings (defaults + 2 extra), got %d", wantLen, len(cfg.FieldMappings))
	}

	// Last two should be the extra ones
	last := cfg.FieldMappings[len(cfg.FieldMappings)-2:]
	if last[0].NUTVar != "ups.realpower" || last[1].NUTVar != "ups.power" {
		t.Errorf("unexpected trailing mappings: %+v", last)
	}
}

func TestFieldMappings_ExtraAppendsToExplicit(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
field_mappings:
  - nut_var: battery.charge
    influx_field: battery_charge_percent
    type: float
extra_field_mappings:
  - nut_var: ups.status
    influx_field: ups_status
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.FieldMappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(cfg.FieldMappings))
	}
	if cfg.FieldMappings[1].NUTVar != "ups.status" {
		t.Errorf("expected extra mapping at end, got %+v", cfg.FieldMappings[1])
	}
}

func TestDefaultPort(t *testing.T) {
	content := `
poll_interval: 10s
influxdb:
  url: http://influxdb:8086
  org: home
  bucket: telegraf
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.UPSes[0].Port != 3493 {
		t.Errorf("default port: got %d, want 3493", cfg.UPSes[0].Port)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("default http_port: got %d, want 8080", cfg.HTTPPort)
	}
	if cfg.InfluxDB.Measurement != "upsd" {
		t.Errorf("default measurement: got %q, want 'upsd'", cfg.InfluxDB.Measurement)
	}
}
