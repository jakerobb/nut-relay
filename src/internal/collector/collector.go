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
	s        *store.Store
	writer   *influx.Writer
	interval *time.Duration
}

// New creates a Collector for the given UPS configuration.
func New(cfg config.UPSConfig, s *store.Store, writer *influx.Writer, interval *time.Duration) *Collector {
	return &Collector{cfg: cfg, s: s, writer: writer, interval: interval}
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

	stats := parseVars(vars, c.cfg.Label, c.cfg.UPSName)
	c.s.Set(c.cfg.Label, stats)

	if err := c.writer.Write(stats); err != nil {
		log.Error("influxdb write failed", "err", err)
	}
}

// parseVars converts a NUT VarMap into a UpsStats struct.
func parseVars(vars nut.VarMap, label, upsName string) *store.UpsStats {
	s := &store.UpsStats{
		Label:       label,
		UpsName:     upsName,
		CollectedAt: time.Now().UTC(),
	}

	for k, v := range vars {
		switch k {
		case "battery.charge":
			s.BatteryCharge = parseFloat(v)
		case "battery.voltage":
			s.BatteryVoltage = parseFloat(v)
		case "battery.runtime":
			s.BatteryRuntime = parseInt(v)
		case "battery.low":
			s.BatteryLow = parseFloat(v)
		case "input.voltage":
			s.InputVoltage = parseFloat(v)
		case "input.frequency":
			s.InputFrequency = parseFloat(v)
		case "output.voltage":
			s.OutputVoltage = parseFloat(v)
		case "output.current":
			s.OutputCurrent = parseFloat(v)
		case "output.power":
			s.OutputPower = parseFloat(v)
		case "output.frequency":
			s.OutputFrequency = parseFloat(v)
		case "ups.status":
			s.Status = strings.TrimSpace(v)
		case "ups.load":
			s.Load = parseFloat(v)
		case "ups.model":
			s.Model = v
		case "ups.serial":
			s.Serial = v
		case "ups.mfr":
			s.Manufacturer = v
		}
	}

	return s
}

func parseFloat(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

func parseInt(s string) *int64 {
	// NUT battery.runtime is in seconds, may be a float string like "3600.0"
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	i := int64(f)
	return &i
}
