package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return tmp
}

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
	if cfg.Output != OutputInfluxDB {
		t.Errorf("default output: got %q, want %q", cfg.Output, OutputInfluxDB)
	}
}

func TestLoadPrometheusConfig(t *testing.T) {
	t.Setenv("NUT_USER", "admin")

	cfg, err := LoadFromPath(writeConfig(t, `
output: Prometheus
poll_interval: 10s
http_port: 9199
variables: [battery.charge, " ups.status "]
upses:
  - label: rack
    host: nut-upsd
    ups_name: cyberpower
    username: ${NUT_USER}
  - label: office
    host: 192.168.0.9
    ups_name: office-ups
    tls_mode: STARTTLS
    tls_skip_verify: true
`))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Output != OutputPrometheus {
		t.Errorf("output: got %q, want lowercased %q", cfg.Output, OutputPrometheus)
	}
	if cfg.StaleAfter() != 30*time.Second {
		t.Errorf("StaleAfter: got %v, want 30s", cfg.StaleAfter())
	}
	if len(cfg.Variables) != 2 || cfg.Variables[1] != "ups.status" {
		t.Errorf("variables: got %q, want trimmed [battery.charge ups.status]", cfg.Variables)
	}
	if len(cfg.FieldMappings) != 0 {
		t.Errorf("field mappings: got %d, want none (defaults are influxdb-only)", len(cfg.FieldMappings))
	}
	if cfg.UPSes[0].Username != "admin" {
		t.Errorf("username: got %q, want 'admin'", cfg.UPSes[0].Username)
	}
	if cfg.UPSes[1].TLSMode != "starttls" || !cfg.UPSes[1].TLSSkipVerify {
		t.Errorf("office TLS: got %q skip=%v", cfg.UPSes[1].TLSMode, cfg.UPSes[1].TLSSkipVerify)
	}
}

func TestLoadDefaultPaths(t *testing.T) {
	t.Setenv("CONFIG_PATH", "")
	dir := t.TempDir()
	newPath := filepath.Join(dir, "nut-relay.yaml")
	oldPath := filepath.Join(dir, "nut-influx-relay.yaml")
	saved := DefaultPaths
	DefaultPaths = []string{newPath, oldPath}
	t.Cleanup(func() { DefaultPaths = saved })

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "no config found") {
		t.Fatalf("expected 'no config found', got %v", err)
	}

	// Only the old name exists: it's used.
	old := "poll_interval: 20s\ninfluxdb: {url: http://influxdb:8086}\nupses: [{label: rack, host: h, ups_name: u}]\n"
	if err := os.WriteFile(oldPath, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || *cfg.PollInterval != 20*time.Second {
		t.Fatalf("old path: cfg=%+v err=%v", cfg, err)
	}

	// Both exist: the new name wins.
	if err := os.WriteFile(newPath, []byte(strings.Replace(old, "20s", "30s", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || *cfg.PollInterval != 30*time.Second {
		t.Fatalf("new path: cfg=%+v err=%v", cfg, err)
	}
}

func TestValidationErrors(t *testing.T) {
	const ups = "upses:\n  - {label: rack, host: h, ups_name: u}\n"
	const influx = "influxdb: {url: http://influxdb:8086}\n"
	cases := map[string]struct {
		content string
		want    string
	}{
		"missing poll_interval": {influx + ups, "poll_interval is required"},
		"bad poll_interval":     {"poll_interval: often\n" + influx + ups, "invalid poll_interval"},
		"zero poll_interval":    {"poll_interval: 0s\n" + influx + ups, "must be positive"},
		"no upses":              {"poll_interval: 10s\n" + influx, "at least one entry in upses"},
		"missing label": {"poll_interval: 10s\n" + influx + "upses:\n  - {host: h, ups_name: u}\n",
			"label is required"},
		"duplicate label": {"poll_interval: 10s\n" + influx + "upses:\n  - {label: rack, host: h, ups_name: u}\n  - {label: rack, host: h2, ups_name: u2}\n",
			`duplicate label "rack"`},
		"missing host": {"poll_interval: 10s\n" + influx + "upses:\n  - {label: rack, ups_name: u}\n",
			"host is required"},
		"missing ups_name": {"poll_interval: 10s\n" + influx + "upses:\n  - {label: rack, host: h}\n",
			"ups_name is required"},
		"bad tls_mode": {"poll_interval: 10s\n" + influx + "upses:\n  - {label: rack, host: h, ups_name: u, tls_mode: ssl}\n",
			"invalid tls_mode"},
		"bad output":           {"output: statsd\npoll_interval: 10s\n" + ups, `invalid output "statsd"`},
		"influxdb without url": {"poll_interval: 10s\n" + ups, "influxdb.url is required"},
		"variables with influxdb": {"poll_interval: 10s\nvariables: [battery.charge]\n" + influx + ups,
			"variables only applies to output: prometheus"},
		"influxdb block with prometheus": {"output: prometheus\npoll_interval: 10s\n" + influx + ups,
			"influxdb only applies to output: influxdb"},
		"field_mappings with prometheus": {"output: prometheus\npoll_interval: 10s\nfield_mappings: [{nut_var: ups.load, influx_field: load}]\n" + ups,
			"only apply to output: influxdb"},
		"extra_field_mappings with prometheus": {"output: prometheus\npoll_interval: 10s\nextra_field_mappings: [{nut_var: ups.load, influx_field: load}]\n" + ups,
			"only apply to output: influxdb"},
		"empty variable": {"output: prometheus\npoll_interval: 10s\nvariables: [battery.charge, \"\"]\n" + ups,
			"variables[1]: empty variable name"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := LoadFromPath(writeConfig(t, tc.content))
			if err == nil {
				t.Fatalf("expected error containing %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q doesn't contain %q", err, tc.want)
			}
		})
	}
}
