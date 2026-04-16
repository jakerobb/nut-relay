package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort           int         `yaml:"http_port"`
	InfluxDB           InfluxDB    `yaml:"influxdb"`
	UPSes              []UPSConfig `yaml:"upses"`
	PollIntervalString string      `yaml:"poll_interval"`
	PollInterval       *time.Duration
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
	TLS           bool   `yaml:"tls"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
}

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

func Load() (*Config, error) {
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "/etc/nut-influx-relay/config.yaml"
	}

	return LoadFromPath(path)
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

	return &cfg, nil
}

func validate(cfg *Config) error {
	if cfg.PollIntervalString == "" {
		return fmt.Errorf("poll_interval is required")
	}
	pollInterval, err := time.ParseDuration(cfg.PollIntervalString)
	if err != nil {
		return fmt.Errorf("invalid poll_interval `%s`: %w", cfg.PollInterval, err)
	}
	cfg.PollInterval = &pollInterval

	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.InfluxDB.URL == "" {
		return fmt.Errorf("influxdb.url is required")
	}
	if cfg.InfluxDB.Measurement == "" {
		cfg.InfluxDB.Measurement = "upsd"
	}
	for i, u := range cfg.UPSes {
		if u.Label == "" {
			return fmt.Errorf("upses[%d]: label is required", i)
		}
		if u.Host == "" {
			return fmt.Errorf("upses[%d]: host is required", i)
		}
		if u.Port == 0 {
			cfg.UPSes[i].Port = 3493
		}
		if u.UPSName == "" {
			return fmt.Errorf("upses[%d]: ups_name is required", i)
		}
	}
	return nil
}
