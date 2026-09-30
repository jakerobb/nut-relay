package store

import (
	"maps"
	"slices"
	"sync"
	"time"
)

// UpsState is what's known about one UPS: its latest successful poll and
// how polling is going.
type UpsState struct {
	Label   string `json:"label"`
	UpsName string `json:"ups_name"`
	Host    string `json:"host"`
	// Vars holds every NUT variable from the last successful poll, as strings,
	// exactly as the NUT server sent them. Nil until a poll has succeeded.
	Vars        map[string]string `json:"vars"`
	LastSuccess *time.Time        `json:"last_success"`
	LastAttempt *time.Time        `json:"last_attempt"`
	// LastError is the error from the latest poll, or empty if it succeeded.
	LastError string `json:"last_error,omitempty"`
	Polls     uint64 `json:"polls"`
	Failures  uint64 `json:"failures"`
}

// Store holds the state of every configured UPS, keyed by label.
type Store struct {
	mu sync.RWMutex
	m  map[string]*UpsState
}

// Register adds a UPS before its first poll, so it's reported (as down)
// even if no poll ever succeeds.
func (s *Store) Register(label, upsName, host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]*UpsState)
	}
	s.m[label] = &UpsState{Label: label, UpsName: upsName, Host: host}
}

// RecordSuccess stores the variables from a successful poll.
func (s *Store) RecordSuccess(label string, vars map[string]string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[label]
	if !ok {
		return
	}
	st.Vars = vars
	st.LastSuccess = &at
	st.LastAttempt = &at
	st.LastError = ""
	st.Polls++
}

// RecordFailure notes a failed poll. The last good variables are kept for
// the JSON API; the metrics endpoint decides whether they're still fresh.
func (s *Store) RecordFailure(label string, err error, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[label]
	if !ok {
		return
	}
	st.LastAttempt = &at
	st.LastError = err.Error()
	st.Polls++
	st.Failures++
}

// Get returns a copy of the given UPS's state, or nil if it isn't registered.
func (s *Store) Get(label string) *UpsState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.m[label]
	if !ok {
		return nil
	}
	c := *st
	return &c
}

// All returns copies of every UPS's state, sorted by label. Vars maps are
// replaced wholesale on each poll, never modified, so sharing them is safe.
func (s *Store) All() []*UpsState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*UpsState, 0, len(s.m))
	for _, label := range slices.Sorted(maps.Keys(s.m)) {
		c := *s.m[label]
		result = append(result, &c)
	}
	return result
}
