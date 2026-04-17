package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/jakerobb/nut-influx-relay/internal/api"
	"github.com/jakerobb/nut-influx-relay/internal/collector"
	"github.com/jakerobb/nut-influx-relay/internal/config"
	"github.com/jakerobb/nut-influx-relay/internal/influx"
	"github.com/jakerobb/nut-influx-relay/internal/store"
)

func main() {
	logLevel := slog.LevelInfo
	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	slog.Info("configuration loaded",
		"poll_interval", cfg.PollInterval,
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
			"tls_mode", u.TLSMode,
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
		c := collector.New(upsCfg, cfg.FieldMappings, s, writer, cfg.PollInterval)
		c.Start()
	}

	srv := api.New(s, cfg.HTTPPort)
	slog.Info("HTTP server starting", "port", cfg.HTTPPort)
	if err := srv.Start(); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}
