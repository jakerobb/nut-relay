package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/api"
	"github.com/jakerobb/nut-influx-relay/internal/collector"
	"github.com/jakerobb/nut-influx-relay/internal/config"
	"github.com/jakerobb/nut-influx-relay/internal/influx"
	"github.com/jakerobb/nut-influx-relay/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/nut-influx-relay/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "path", configPath, "err", err)
		os.Exit(1)
	}

	pollInterval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		slog.Error("invalid poll_interval", "value", cfg.PollInterval, "err", err)
		os.Exit(1)
	}

	slog.Info("configuration loaded",
		"poll_interval", pollInterval,
		"http_port", cfg.HTTPPort,
		"influxdb_url", cfg.InfluxDB.URL,
		"ups_count", len(cfg.UPSes),
	)

	for _, u := range cfg.UPSes {
		slog.Info("configured UPS",
			"label", u.Label,
			"host", u.Host,
			"port", u.Port,
			"ups_name", u.UPSName,
			"tls", u.TLS,
		)
	}

	s := &store.Store{}

	writer := influx.New(
		cfg.InfluxDB.URL,
		cfg.InfluxDB.Token,
		cfg.InfluxDB.Org,
		cfg.InfluxDB.Bucket,
		cfg.InfluxDB.Measurement,
	)

	for _, upsCfg := range cfg.UPSes {
		c := collector.New(upsCfg, s, writer, pollInterval)
		c.Start()
	}

	srv := api.New(s, cfg.HTTPPort)
	slog.Info("HTTP server starting", "port", cfg.HTTPPort)
	if err := srv.Start(); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}
