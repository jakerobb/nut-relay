package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-relay/internal/store"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func render(t *testing.T, r *Renderer, states ...*store.UpsState) string {
	t.Helper()
	var buf bytes.Buffer
	if err := r.Write(&buf, states, now); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func state(label string, vars map[string]string, lastSuccessAgo time.Duration) *store.UpsState {
	ls := now.Add(-lastSuccessAgo)
	return &store.UpsState{
		Label: label, UpsName: label + "-ups", Host: "h",
		Vars: vars, LastSuccess: &ls, LastAttempt: &ls, Polls: 5, Failures: 1,
	}
}

func assertContains(t *testing.T, out string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !strings.Contains(out, l+"\n") {
			t.Errorf("missing line %q in:\n%s", l, out)
		}
	}
}

func assertNotContains(t *testing.T, out string, substrs ...string) {
	t.Helper()
	for _, s := range substrs {
		if strings.Contains(out, s) {
			t.Errorf("unexpected %q in:\n%s", s, out)
		}
	}
}

func TestNumericVariables(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, state("rack", map[string]string{
		"battery.charge":  "100",
		"battery.voltage": " 26.0 ",
		"ups.model":       "CP1500PFCRM2U", // not numeric
	}, 5*time.Second))

	assertContains(t, out,
		"# HELP nut_battery_charge NUT variable battery.charge.",
		"# TYPE nut_battery_charge gauge",
		`nut_battery_charge{ups="rack"} 100`,
		`nut_battery_voltage{ups="rack"} 26`,
	)
	assertNotContains(t, out, "nut_ups_model")
}

func TestStatusFlags(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, state("rack", map[string]string{"ups.status": "OL CHRG ALARM"}, 0))

	assertContains(t, out,
		`nut_ups_status{ups="rack",flag="OL"} 1`,
		`nut_ups_status{ups="rack",flag="CHRG"} 1`,
		`nut_ups_status{ups="rack",flag="ALARM"} 1`, // unknown flag, still exported
		`nut_ups_status{ups="rack",flag="OB"} 0`,
		`nut_ups_status{ups="rack",flag="LB"} 0`,
	)
	// One HELP/TYPE per family, however many samples.
	if n := strings.Count(out, "# TYPE nut_ups_status gauge"); n != 1 {
		t.Errorf("TYPE nut_ups_status appears %d times", n)
	}
}

func TestInfoAndHealth(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, state("office", map[string]string{
		"ups.mfr":    "Ubiquiti",
		"ups.model":  "TOWER_1000VA_120V",
		"ups.serial": "1C0B8B3A6BFF",
	}, 0))

	assertContains(t, out,
		`nut_ups_info{ups="office",ups_name="office-ups",mfr="Ubiquiti",model="TOWER_1000VA_120V",serial="1C0B8B3A6BFF"} 1`,
		`nut_up{ups="office"} 1`,
		`nut_polls_total{ups="office"} 5`,
		`nut_poll_failures_total{ups="office"} 1`,
		"# TYPE nut_polls_total counter",
		`nut_last_success_timestamp_seconds{ups="office"} 1.7906832e+09`,
	)
}

func TestDeviceIdentityPreferredOverUPS(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, state("rack", map[string]string{
		"device.mfr": "CPS", "ups.mfr": "ignored",
	}, 0))
	assertContains(t, out, `nut_ups_info{ups="rack",ups_name="rack-ups",mfr="CPS",model="",serial=""} 1`)
}

func TestFailedPollWithinStaleWindow(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	st := state("rack", map[string]string{"battery.charge": "100"}, 20*time.Second)
	st.LastError = "i/o timeout"
	out := render(t, r, st)

	// Down, but the last good values are still recent enough to show.
	assertContains(t, out,
		`nut_up{ups="rack"} 0`,
		`nut_battery_charge{ups="rack"} 100`,
	)
}

func TestStaleValuesDropped(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	st := state("rack", map[string]string{"battery.charge": "100", "ups.status": "OL"}, 2*time.Minute)
	st.LastError = "connection refused"
	out := render(t, r, st)

	assertContains(t, out,
		`nut_up{ups="rack"} 0`,
		`nut_polls_total{ups="rack"} 5`,
	)
	assertNotContains(t, out, "nut_battery_charge{", "nut_ups_status{", "nut_ups_info{")
}

func TestNeverPolledSuccessfully(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, &store.UpsState{Label: "rack", Polls: 3, Failures: 3, LastError: "refused"})

	assertContains(t, out, `nut_up{ups="rack"} 0`, `nut_poll_failures_total{ups="rack"} 3`)
	assertNotContains(t, out, "nut_last_success_timestamp_seconds{")
}

func TestExcludeAndInclude(t *testing.T) {
	vars := map[string]string{
		"battery.charge":            "100",
		"driver.parameter.pollfreq": "12",
		"ups.productid":             "0601",
		"ups.status":                "OL",
	}

	excl := render(t, &Renderer{StaleAfter: time.Minute, Exclude: []string{"driver.", "ups.productid"}}, state("rack", vars, 0))
	assertContains(t, excl, `nut_battery_charge{ups="rack"} 100`, `nut_ups_status{ups="rack",flag="OL"} 1`)
	assertNotContains(t, excl, "nut_driver_", "nut_ups_productid")

	// Include wins over Exclude, and limits everything, ups.status too.
	incl := render(t, &Renderer{StaleAfter: time.Minute, Include: []string{"driver.parameter.pollfreq"}, Exclude: []string{"driver."}}, state("rack", vars, 0))
	assertContains(t, incl, `nut_driver_parameter_pollfreq{ups="rack"} 12`)
	assertNotContains(t, incl, "nut_battery_charge{", "nut_ups_status{")
}

func TestMultipleUPSesSortedAndGrouped(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r,
		state("rack", map[string]string{"battery.charge": "90"}, 0),
		state("office", map[string]string{"battery.charge": "100"}, 0),
	)

	want := "# HELP nut_battery_charge NUT variable battery.charge.\n" +
		"# TYPE nut_battery_charge gauge\n" +
		`nut_battery_charge{ups="office"} 100` + "\n" +
		`nut_battery_charge{ups="rack"} 90` + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected one grouped, sorted family:\n%s\ngot:\n%s", want, out)
	}
}

func TestMetricName(t *testing.T) {
	cases := map[string]string{
		"battery.charge":        "nut_battery_charge",
		"ambient.1.temperature": "nut_ambient_1_temperature",
		"outlet.1-2.status":     "nut_outlet_1_2_status",
	}
	for in, want := range cases {
		if got := MetricName(in); got != want {
			t.Errorf("MetricName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLabelEscaping(t *testing.T) {
	r := &Renderer{StaleAfter: time.Minute}
	out := render(t, r, state("rack", map[string]string{"ups.model": "A \"quoted\\\" model\nline"}, 0))
	assertContains(t, out, `nut_ups_info{ups="rack",ups_name="rack-ups",mfr="",model="A \"quoted\\\" model\nline",serial=""} 1`)
}
