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
