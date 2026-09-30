package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Output modes. One per process: run two instances to feed both.
const (
	OutputInfluxDB   = "influxdb"
	OutputPrometheus = "prometheus"
)

type Config struct {
	// Output is influxdb (the default, so configs from before this setting
	// existed keep working) or prometheus.
	Output             string      `yaml:"output"`
	HTTPPort           int         `yaml:"http_port"`
	UPSes              []UPSConfig `yaml:"upses"`
	PollIntervalString string      `yaml:"poll_interval"`
	PollInterval       *time.Duration

	// influxdb output only.
	InfluxDB           InfluxDB       `yaml:"influxdb"`
	FieldMappings      []FieldMapping `yaml:"field_mappings"`
	ExtraFieldMappings []FieldMapping `yaml:"extra_field_mappings"`

	// prometheus output only. If set, exactly these NUT variables are
	// exported (ups.status included). Empty means every numeric variable
	// except DefaultExcludedVariables.
	Variables []string `yaml:"variables"`
}

type InfluxDB struct {
	URL         string `yaml:"url"`
	Token       string `yaml:"token"`
	Org         string `yaml:"org"`
	Bucket      string `yaml:"bucket"`
	Measurement string `yaml:"measurement"`
}

type UPSConfig struct {
	Label         string `yaml:"label"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	UPSName       string `yaml:"ups_name"`
	TLSMode       string `yaml:"tls_mode"` // plain, tls, starttls
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
}

// FieldMapping defines how a single NUT variable is mapped to an InfluxDB field.
type FieldMapping struct {
	NUTVar      string `yaml:"nut_var"`
	InfluxField string `yaml:"influx_field"`
	// Type is one of: float, int, string.
	Type string `yaml:"type"`
}

// DefaultFieldMappings is used when field_mappings is omitted from the config.
var DefaultFieldMappings = []FieldMapping{
	{NUTVar: "battery.charge", InfluxField: "battery_charge_percent", Type: "float"},
	{NUTVar: "battery.runtime", InfluxField: "battery_runtime_seconds", Type: "int"},
	{NUTVar: "battery.voltage", InfluxField: "battery_voltage", Type: "float"},
	{NUTVar: "battery.low", InfluxField: "battery_low", Type: "float"},
	{NUTVar: "input.voltage", InfluxField: "input_voltage", Type: "float"},
	{NUTVar: "input.frequency", InfluxField: "input_frequency", Type: "float"},
	{NUTVar: "output.voltage", InfluxField: "output_voltage", Type: "float"},
	{NUTVar: "output.current", InfluxField: "output_current", Type: "float"},
	{NUTVar: "output.power", InfluxField: "output_power", Type: "float"},
	{NUTVar: "output.frequency", InfluxField: "output_frequency", Type: "float"},
	{NUTVar: "ups.realpower", InfluxField: "real_power_watts", Type: "float"},
	{NUTVar: "ups.power", InfluxField: "apparent_power_va", Type: "float"},
	{NUTVar: "ups.status", InfluxField: "ups_status", Type: "string"},
	{NUTVar: "ups.load", InfluxField: "load_percent", Type: "float"},
	{NUTVar: "ups.model", InfluxField: "model", Type: "string"},
	{NUTVar: "ups.mfr", InfluxField: "manufacturer", Type: "string"},
}

// DefaultExcludedVariables are left out of the prometheus output when
// Variables is empty: numbers that describe the driver or USB device rather
// than the UPS's state. An entry ending in "." excludes every variable with
// that prefix.
var DefaultExcludedVariables = []string{"driver.", "ups.productid", "ups.vendorid"}

// Default config locations, tried in order. The second is where
// nut-influx-relay (this app's old name) read it.
var DefaultPaths = []string{"/etc/nut-relay/config.yaml", "/etc/nut-influx-relay/config.yaml"}

var envVarRe = regexp.MustCompile(`\$\{([^}]+)}`)

// interpolate replaces ${VAR_NAME} references with environment variable values.
func interpolate(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarRe.FindStringSubmatch(match)[1]
		return os.Getenv(name)
	})
}

func (c *Config) interpolateEnvVars() {
	c.InfluxDB.Token = interpolate(c.InfluxDB.Token)
	c.InfluxDB.URL = interpolate(c.InfluxDB.URL)
	for i := range c.UPSes {
		c.UPSes[i].Username = interpolate(c.UPSes[i].Username)
		c.UPSes[i].Password = interpolate(c.UPSes[i].Password)
	}
}

// StaleAfter is how old a UPS's last successful poll can be before its
// values stop being exported as Prometheus metrics: three missed polls.
func (c *Config) StaleAfter() time.Duration {
	return 3 * *c.PollInterval
}

// Load reads the config from CONFIG_PATH if set, else the first of
// DefaultPaths that exists.
func Load() (*Config, error) {
	if path := os.Getenv("CONFIG_PATH"); path != "" {
		return LoadFromPath(path)
	}
	for _, path := range DefaultPaths {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return LoadFromPath(path)
	}
	return nil, fmt.Errorf("no config found at %s", strings.Join(DefaultPaths, " or "))
}

func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config from path %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config from path %s: %w", path, err)
	}

	cfg.interpolateEnvVars()

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config from path %s: %w", path, err)
	}

	if cfg.Output == OutputInfluxDB {
		slog.Debug("loaded config", "token", truncTokenForLogging(cfg.InfluxDB.Token))
	}

	return &cfg, nil
}

func truncTokenForLogging(token string) string {
	tokenLength := len(token)
	if tokenLength == 0 {
		return "[empty token]"
	}

	var offset int
	if tokenLength > 30 {
		offset = 10
	} else if tokenLength > 10 {
		offset = 3
	} else {
		return fmt.Sprintf("[redacted - %d chars]", tokenLength)
	}
	return fmt.Sprintf("%s...%s", token[:offset], token[tokenLength-offset:])
}

func validate(cfg *Config) error {
	if cfg.PollIntervalString == "" {
		return fmt.Errorf("poll_interval is required")
	}
	pollInterval, err := time.ParseDuration(cfg.PollIntervalString)
	if err != nil {
		return fmt.Errorf("invalid poll_interval `%s`: %w", cfg.PollIntervalString, err)
	}
	if pollInterval <= 0 {
		return fmt.Errorf("poll_interval must be positive, got %s", cfg.PollIntervalString)
	}
	cfg.PollInterval = &pollInterval

	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}

	if len(cfg.UPSes) == 0 {
		return fmt.Errorf("at least one entry in upses is required")
	}
	labels := make(map[string]bool)
	for i, u := range cfg.UPSes {
		if u.Label == "" {
			return fmt.Errorf("upses[%d]: label is required", i)
		}
		if labels[u.Label] {
			return fmt.Errorf("upses[%d]: duplicate label %q", i, u.Label)
		}
		labels[u.Label] = true
		if u.Host == "" {
			return fmt.Errorf("upses[%d]: host is required", i)
		}
		if u.Port == 0 {
			cfg.UPSes[i].Port = 3493
		}
		if u.UPSName == "" {
			return fmt.Errorf("upses[%d]: ups_name is required", i)
		}
		u.TLSMode = strings.ToLower(u.TLSMode) // case insensitive (Postel's law!)
		cfg.UPSes[i].TLSMode = u.TLSMode       // copy it back to the slice
		if u.TLSMode == "" {
			cfg.UPSes[i].TLSMode = "plain"
		} else if !validTLSMode(u.TLSMode) {
			return fmt.Errorf("upses[%d]: invalid tls_mode %q (must be plain, tls, or starttls)", i, u.TLSMode)
		}
	}

	cfg.Output = strings.ToLower(strings.TrimSpace(cfg.Output))
	switch cfg.Output {
	case "", OutputInfluxDB:
		cfg.Output = OutputInfluxDB
		return validateInfluxDB(cfg)
	case OutputPrometheus:
		return validatePrometheus(cfg)
	default:
		return fmt.Errorf("invalid output %q (must be %s or %s)", cfg.Output, OutputInfluxDB, OutputPrometheus)
	}
}

func validateInfluxDB(cfg *Config) error {
	if len(cfg.Variables) > 0 {
		return fmt.Errorf("variables only applies to output: %s (use field_mappings for %s)", OutputPrometheus, OutputInfluxDB)
	}
	if cfg.InfluxDB.URL == "" {
		return fmt.Errorf("influxdb.url is required")
	}
	if cfg.InfluxDB.Measurement == "" {
		cfg.InfluxDB.Measurement = "upsd"
	}

	if len(cfg.FieldMappings) == 0 {
		cfg.FieldMappings = DefaultFieldMappings
	} else {
		validated, err := validateFieldMappings(cfg.FieldMappings, "field_mappings")
		if err != nil {
			return err
		}
		cfg.FieldMappings = validated
	}

	if len(cfg.ExtraFieldMappings) > 0 {
		validated, err := validateFieldMappings(cfg.ExtraFieldMappings, "extra_field_mappings")
		if err != nil {
			return err
		}
		cfg.FieldMappings = append(cfg.FieldMappings, validated...)
	}

	return nil
}

// validatePrometheus rejects InfluxDB-only settings rather than ignoring
// them, so a config that mixes the two fails loudly.
func validatePrometheus(cfg *Config) error {
	if cfg.InfluxDB != (InfluxDB{}) {
		return fmt.Errorf("influxdb only applies to output: %s", OutputInfluxDB)
	}
	if len(cfg.FieldMappings) > 0 || len(cfg.ExtraFieldMappings) > 0 {
		return fmt.Errorf("field_mappings and extra_field_mappings only apply to output: %s (use variables for %s)", OutputInfluxDB, OutputPrometheus)
	}
	for i, v := range cfg.Variables {
		v = strings.TrimSpace(v)
		if v == "" {
			return fmt.Errorf("variables[%d]: empty variable name", i)
		}
		cfg.Variables[i] = v
	}
	return nil
}

func validateFieldMappings(mappings []FieldMapping, prefix string) ([]FieldMapping, error) {
	result := make([]FieldMapping, len(mappings))
	for i, m := range mappings {
		if m.NUTVar == "" {
			return nil, fmt.Errorf("%s[%d]: nut_var is required", prefix, i)
		}
		if m.InfluxField == "" {
			return nil, fmt.Errorf("%s[%d]: influx_field is required", prefix, i)
		}
		m.Type = strings.ToLower(m.Type) // case insensitive (Postel's law!)
		if m.Type == "" {
			m.Type = "string"
		} else if !validFieldType(m.Type) {
			return nil, fmt.Errorf("%s[%d]: invalid type %q (must be float, int, or string)", prefix, i, m.Type)
		}
		result[i] = m
	}
	return result, nil
}

func validTLSMode(mode string) bool {
	switch mode {
	case "plain", "tls", "starttls":
		return true
	default:
		return false
	}
}

func validFieldType(t string) bool {
	switch t {
	case "float", "int", "string":
		return true
	default:
		return false
	}
}
