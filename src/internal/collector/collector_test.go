package collector

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jakerobb/nut-relay/internal/config"
	"github.com/jakerobb/nut-relay/internal/nut"
	"github.com/jakerobb/nut-relay/internal/store"
)

func newTestCollector(s *store.Store, sink Sink, fetch fetchFunc) *Collector {
	interval := 10 * time.Second
	c := New(config.UPSConfig{Label: "rack", Host: "nut-upsd", Port: 3493, UPSName: "cyberpower"}, s, sink, &interval)
	c.fetch = fetch
	return c
}

// fakeSink records writes, optionally failing them.
type fakeSink struct {
	labels []string
	vars   []map[string]string
	err    error
}

func (f *fakeSink) Write(label string, vars map[string]string, _ time.Time) error {
	f.labels = append(f.labels, label)
	f.vars = append(f.vars, vars)
	return f.err
}

func TestNewRegistersUPS(t *testing.T) {
	s := &store.Store{}
	newTestCollector(s, nil, nil)

	st := s.Get("rack")
	if st == nil {
		t.Fatal("UPS not registered")
	}
	if st.UpsName != "cyberpower" || st.Host != "nut-upsd" {
		t.Errorf("got %+v", st)
	}
	if st.LastSuccess != nil || st.Polls != 0 {
		t.Errorf("expected no polls yet, got %+v", st)
	}
}

func TestPollSuccess(t *testing.T) {
	s := &store.Store{}
	c := newTestCollector(s, nil, func(host string, port int, upsName, tlsMode string, skip bool, user, pass string) (nut.VarMap, error) {
		if host != "nut-upsd" || port != 3493 || upsName != "cyberpower" {
			t.Errorf("fetch called with %s:%d %s", host, port, upsName)
		}
		return nut.VarMap{"battery.charge": "100"}, nil
	})

	c.poll(slog.Default())

	st := s.Get("rack")
	if st.Vars["battery.charge"] != "100" {
		t.Errorf("vars: got %v", st.Vars)
	}
	if st.LastSuccess == nil || st.LastAttempt == nil {
		t.Error("expected LastSuccess and LastAttempt to be set")
	}
	if st.Polls != 1 || st.Failures != 0 || st.LastError != "" {
		t.Errorf("got polls=%d failures=%d err=%q", st.Polls, st.Failures, st.LastError)
	}
}

func TestPollFailureKeepsLastGoodVars(t *testing.T) {
	s := &store.Store{}
	fail := false
	c := newTestCollector(s, nil, func(string, int, string, string, bool, string, string) (nut.VarMap, error) {
		if fail {
			return nil, errors.New("connection refused")
		}
		return nut.VarMap{"battery.charge": "100"}, nil
	})

	c.poll(slog.Default())
	fail = true
	c.poll(slog.Default())

	st := s.Get("rack")
	if st.Vars["battery.charge"] != "100" {
		t.Errorf("last good vars lost: %v", st.Vars)
	}
	if st.LastError != "connection refused" {
		t.Errorf("LastError: got %q", st.LastError)
	}
	if st.Polls != 2 || st.Failures != 1 {
		t.Errorf("got polls=%d failures=%d, want 2 and 1", st.Polls, st.Failures)
	}
	if !st.LastAttempt.After(*st.LastSuccess) && !st.LastAttempt.Equal(*st.LastSuccess) {
		t.Errorf("LastAttempt %v before LastSuccess %v", st.LastAttempt, st.LastSuccess)
	}
}

func TestPollWritesToSink(t *testing.T) {
	s := &store.Store{}
	sink := &fakeSink{}
	fail := false
	c := newTestCollector(s, sink, func(string, int, string, string, bool, string, string) (nut.VarMap, error) {
		if fail {
			return nil, errors.New("connection refused")
		}
		return nut.VarMap{"battery.charge": "100"}, nil
	})

	c.poll(slog.Default())
	fail = true
	c.poll(slog.Default())

	// Only the successful poll reaches the sink.
	if len(sink.labels) != 1 || sink.labels[0] != "rack" || sink.vars[0]["battery.charge"] != "100" {
		t.Errorf("sink got labels=%v vars=%v", sink.labels, sink.vars)
	}
}

func TestSinkFailureDoesNotFailPoll(t *testing.T) {
	s := &store.Store{}
	sink := &fakeSink{err: errors.New("influxdb returned 401")}
	c := newTestCollector(s, sink, func(string, int, string, string, bool, string, string) (nut.VarMap, error) {
		return nut.VarMap{"battery.charge": "100"}, nil
	})

	c.poll(slog.Default())

	// The poll itself succeeded; a failed write is logged, not recorded as a poll failure.
	st := s.Get("rack")
	if st.Failures != 0 || st.LastError != "" || st.Vars["battery.charge"] != "100" {
		t.Errorf("got %+v", st)
	}
}
