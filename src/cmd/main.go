package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/jakerobb/nut-relay/internal/api"
	"github.com/jakerobb/nut-relay/internal/collector"
	"github.com/jakerobb/nut-relay/internal/config"
	"github.com/jakerobb/nut-relay/internal/influx"
	"github.com/jakerobb/nut-relay/internal/metrics"
	"github.com/jakerobb/nut-relay/internal/store"
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
		"output", cfg.Output,
		"poll_interval", cfg.PollInterval,
		"http_port", cfg.HTTPPort,
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

	// One output per process: InfluxDB pushes each poll through a sink;
	// Prometheus serves /metrics from the store.
	var sink collector.Sink
	var renderer *metrics.Renderer
	switch cfg.Output {
	case config.OutputInfluxDB:
		slog.Info("writing to InfluxDB",
			"influxdb_url", cfg.InfluxDB.URL,
			"measurement", cfg.InfluxDB.Measurement,
		)
		sink = influx.New(
			cfg.InfluxDB.URL,
			cfg.InfluxDB.Token,
			cfg.InfluxDB.Org,
			cfg.InfluxDB.Bucket,
			cfg.InfluxDB.Measurement,
			cfg.FieldMappings,
		)
	case config.OutputPrometheus:
		slog.Info("serving Prometheus metrics on /metrics", "variables", cfg.Variables)
		renderer = &metrics.Renderer{
			StaleAfter: cfg.StaleAfter(),
			Include:    cfg.Variables,
			Exclude:    config.DefaultExcludedVariables,
		}
	}

	for _, upsCfg := range cfg.UPSes {
		c := collector.New(upsCfg, s, sink, cfg.PollInterval)
		c.Start()
	}

	srv := api.New(s, renderer, cfg.HTTPPort)
	slog.Info("HTTP server starting", "port", cfg.HTTPPort)
	if err := srv.Start(); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}
