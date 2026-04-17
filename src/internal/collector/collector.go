package collector

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/config"
	"github.com/jakerobb/nut-influx-relay/internal/influx"
	"github.com/jakerobb/nut-influx-relay/internal/nut"
	"github.com/jakerobb/nut-influx-relay/internal/store"
)

// Collector polls a single UPS on a fixed interval.
type Collector struct {
	cfg      config.UPSConfig
	mappings []config.FieldMapping
	s        *store.Store
	writer   *influx.Writer
	interval *time.Duration
}

// New creates a Collector for the given UPS configuration.
func New(cfg config.UPSConfig, mappings []config.FieldMapping, s *store.Store, writer *influx.Writer, interval *time.Duration) *Collector {
	return &Collector{cfg: cfg, mappings: mappings, s: s, writer: writer, interval: interval}
}

// Start launches the polling goroutine. It runs until the process exits.
func (c *Collector) Start() {
	log := slog.With(
		"label", c.cfg.Label,
		"host", c.cfg.Host,
		"port", c.cfg.Port,
		"ups_name", c.cfg.UPSName,
		"tls_mode", c.cfg.TLSMode,
	)
	log.Info("starting collector")

	go func() {
		// Poll immediately on start, then on each tick.
		c.poll(log)
		ticker := time.NewTicker(*c.interval)
		defer ticker.Stop()
		for range ticker.C {
			c.poll(log)
		}
	}()
}

func (c *Collector) poll(log *slog.Logger) {
	vars, err := nut.FetchVars(
		c.cfg.Host, c.cfg.Port, c.cfg.UPSName,
		c.cfg.TLSMode, c.cfg.TLSSkipVerify,
		c.cfg.Username, c.cfg.Password,
	)
	if err != nil {
		log.Error("poll failed", "err", err)
		return
	}

	stats := parseVars(vars, c.cfg.Label, c.cfg.UPSName, c.mappings)
	c.s.Set(c.cfg.Label, stats)

	if err := c.writer.Write(stats); err != nil {
		log.Error("influxdb write failed", "err", err)
	}
}

// parseVars converts a NUT VarMap into a UpsStats using the configured field mappings.
// ups.serial is always used to populate Serial (the InfluxDB tag), regardless of mappings.
func parseVars(vars nut.VarMap, label, upsName string, mappings []config.FieldMapping) *store.UpsStats {
	s := &store.UpsStats{
		Label:       label,
		UpsName:     upsName,
		CollectedAt: time.Now().UTC(),
		Serial:      strings.TrimSpace(vars["ups.serial"]),
		Fields:      make(map[string]any),
	}

	for _, m := range mappings {
		rawVal, ok := vars[m.NUTVar]
		if !ok {
			continue
		}
		rawVal = strings.TrimSpace(rawVal)

		switch m.Type {
		case "float":
			if f, err := strconv.ParseFloat(rawVal, 64); err == nil {
				s.Fields[m.InfluxField] = f
			}
		case "int":
			// Parse as float first to handle values like "3600.0"
			if f, err := strconv.ParseFloat(rawVal, 64); err == nil {
				s.Fields[m.InfluxField] = int64(f)
			}
		case "string":
			s.Fields[m.InfluxField] = rawVal
		}
	}

	return s
}
