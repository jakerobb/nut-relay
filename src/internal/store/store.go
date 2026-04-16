package store

import (
	"sync"
	"time"
)

// UpsStats holds the latest metrics collected from a single UPS.
// All numeric fields are pointers so that fields not reported by a given UPS
// serialize as null in JSON and are skipped in InfluxDB writes.
type UpsStats struct {
	Label       string    `json:"label"`
	UpsName     string    `json:"ups_name"`
	CollectedAt time.Time `json:"collected_at"`

	// Battery
	BatteryCharge  *float64 `json:"battery_charge_percent"`
	BatteryVoltage *float64 `json:"battery_voltage"`
	BatteryRuntime *int64   `json:"battery_runtime_seconds"`
	BatteryLow     *float64 `json:"battery_low"`

	// Input
	InputVoltage   *float64 `json:"input_voltage"`
	InputFrequency *float64 `json:"input_frequency"`

	// Output
	OutputVoltage   *float64 `json:"output_voltage"`
	OutputCurrent   *float64 `json:"output_current"`
	OutputPower     *float64 `json:"output_power"`
	OutputFrequency *float64 `json:"output_frequency"`

	// Power
	RealPower     *float64 `json:"real_power_watts"`
	ApparentPower *float64 `json:"apparent_power_va"`

	// UPS
	Status       string   `json:"status"` // raw NUT ups.status, e.g. "OL CHRG"
	Load         *float64 `json:"load_percent"`
	Model        string   `json:"model"`
	Serial       string   `json:"serial"`
	Manufacturer string   `json:"manufacturer"`
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
