package collector

import (
	"log/slog"
	"time"

	"github.com/jakerobb/nut-relay/internal/config"
	"github.com/jakerobb/nut-relay/internal/nut"
	"github.com/jakerobb/nut-relay/internal/store"
)

// fetchFunc matches nut.FetchVars; tests substitute a fake.
type fetchFunc func(host string, port int, upsName string, tlsMode string, tlsSkipVerify bool, username, password string) (nut.VarMap, error)

// Sink receives every successful poll, for outputs that push (InfluxDB).
// Pull outputs (Prometheus) read the store instead and need no sink.
type Sink interface {
	Write(label string, vars map[string]string, at time.Time) error
}

// Collector polls a single UPS on a fixed interval.
type Collector struct {
	cfg      config.UPSConfig
	s        *store.Store
	sink     Sink
	interval *time.Duration
	fetch    fetchFunc
}

// New creates a Collector for the given UPS configuration and registers the
// UPS in the store. sink may be nil.
func New(cfg config.UPSConfig, s *store.Store, sink Sink, interval *time.Duration) *Collector {
	s.Register(cfg.Label, cfg.UPSName, cfg.Host)
	return &Collector{cfg: cfg, s: s, sink: sink, interval: interval, fetch: nut.FetchVars}
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
	vars, err := c.fetch(
		c.cfg.Host, c.cfg.Port, c.cfg.UPSName,
		c.cfg.TLSMode, c.cfg.TLSSkipVerify,
		c.cfg.Username, c.cfg.Password,
	)
	now := time.Now().UTC()
	if err != nil {
		log.Error("poll failed", "err", err)
		c.s.RecordFailure(c.cfg.Label, err, now)
		return
	}
	log.Debug("poll succeeded", "vars", len(vars))
	c.s.RecordSuccess(c.cfg.Label, vars, now)

	if c.sink != nil {
		if err := c.sink.Write(c.cfg.Label, vars, now); err != nil {
			log.Error("output write failed", "err", err)
		}
	}
}
