package store

import (
	"sync"
	"time"
)

// UpsStats holds the latest metrics collected from a single UPS.
// Fields contains all configured NUT variable mappings keyed by their InfluxDB field name.
// Values are float64, int64, or string depending on the field type in the mapping.
type UpsStats struct {
	Label       string         `json:"label"`
	UpsName     string         `json:"ups_name"`
	CollectedAt time.Time      `json:"collected_at"`
	Serial      string         `json:"serial"`
	Fields      map[string]any `json:"fields"`
}

// Store is an in-memory store for UPS statistics keyed by UPS label.
// Each UPS label has exactly one writer goroutine, so sync.Map is appropriate.
type Store struct {
	m sync.Map
}

// Set stores the stats for the given UPS label.
func (s *Store) Set(label string, stats *UpsStats) {
	s.m.Store(label, stats)
}

// Get returns the stats for the given UPS label, or nil if not found.
func (s *Store) Get(label string) *UpsStats {
	v, ok := s.m.Load(label)
	if !ok {
		return nil
	}
	return v.(*UpsStats)
}

// All returns a slice of all currently stored UPS stats.
func (s *Store) All() []*UpsStats {
	var result []*UpsStats
	s.m.Range(func(_, v any) bool {
		result = append(result, v.(*UpsStats))
		return true
	})
	return result
}
