package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-relay/internal/metrics"
	"github.com/jakerobb/nut-relay/internal/store"
	"github.com/jakerobb/nut-relay/internal/util"
)

func newTestServer() *httptest.Server {
	s := &store.Store{}
	s.Register("rack", "cyberpower", "nut-upsd")
	s.RecordSuccess("rack", map[string]string{"battery.charge": "100"}, time.Now())
	srv := New(s, &metrics.Renderer{StaleAfter: time.Minute}, 0)
	return httptest.NewServer(srv.Handler())
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer util.CloseCleanly(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestMetricsEndpoint(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, body := get(t, ts.URL+"/metrics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type: %q", ct)
	}
	for _, want := range []string{`nut_up{ups="rack"} 1`, `nut_battery_charge{ups="rack"} 100`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestUPSEndpoints(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, body := get(t, ts.URL+"/ups/rack")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st store.UpsState
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if st.Vars["battery.charge"] != "100" || st.UpsName != "cyberpower" {
		t.Errorf("got %+v", st)
	}

	if resp, _ := get(t, ts.URL+"/ups/nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown label: status %d, want 404", resp.StatusCode)
	}

	resp, body = get(t, ts.URL+"/ups")
	var all []store.UpsState
	if err := json.Unmarshal([]byte(body), &all); err != nil || len(all) != 1 {
		t.Errorf("list: status %d, %d entries, err %v", resp.StatusCode, len(all), err)
	}
}

func TestNoMetricsWithoutRenderer(t *testing.T) {
	s := &store.Store{}
	s.Register("rack", "cyberpower", "nut-upsd")
	ts := httptest.NewServer(New(s, nil, 0).Handler())
	defer ts.Close()

	if resp, _ := get(t, ts.URL+"/metrics"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/metrics with influxdb output: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(t, ts.URL+"/ups/rack"); resp.StatusCode != http.StatusOK {
		t.Errorf("/ups/rack: status %d, want 200", resp.StatusCode)
	}
}
