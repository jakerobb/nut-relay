// Package metrics renders UPS state in the Prometheus text exposition
// format. Hand-written rather than via client_golang, to keep dependencies
// minimal: everything here is a gauge or counter with a few labels.
package metrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jakerobb/nut-relay/internal/store"
)

const namespace = "nut"

// KnownStatusFlags are always exported for ups.status, as 0 when absent, so
// an alert on a flag has a series to evaluate before the flag first
// appears. Any other flag a UPS reports is exported as 1 too.
var KnownStatusFlags = []string{
	"OL", "OB", "LB", "HB", "RB", "CHRG", "DISCHRG", "BYPASS",
	"CAL", "OFF", "OVER", "TRIM", "BOOST", "FSD",
}

// Renderer turns store state into metrics.
type Renderer struct {
	// StaleAfter is how old a UPS's last successful poll can be before its
	// NUT variables are left out.
	StaleAfter time.Duration
	// Include, if non-empty, is the exact set of NUT variables to export.
	Include []string
	// Exclude applies when Include is empty. An entry ending in "." is a
	// prefix.
	Exclude []string
}

type sample struct {
	labels string
	value  float64
}

type family struct {
	help, typ string
	samples   []sample
}

// Write renders every UPS's metrics to w.
func (r *Renderer) Write(w io.Writer, states []*store.UpsState, now time.Time) error {
	families := make(map[string]*family)
	add := func(name, typ, help string, value float64, labels ...string) {
		f, ok := families[name]
		if !ok {
			f = &family{help: help, typ: typ}
			families[name] = f
		}
		f.samples = append(f.samples, sample{labels: formatLabels(labels), value: value})
	}

	for _, st := range states {
		ups := st.Label
		up := st.LastSuccess != nil && st.LastError == ""
		fresh := st.LastSuccess != nil && now.Sub(*st.LastSuccess) <= r.StaleAfter

		add(namespace+"_up", "gauge", "1 if the latest poll of the UPS succeeded, else 0.",
			boolFloat(up), "ups", ups)
		add(namespace+"_polls_total", "counter", "Polls of the UPS's NUT server.",
			float64(st.Polls), "ups", ups)
		add(namespace+"_poll_failures_total", "counter", "Failed polls of the UPS's NUT server.",
			float64(st.Failures), "ups", ups)
		if st.LastSuccess != nil {
			add(namespace+"_last_success_timestamp_seconds", "gauge", "Unix time of the latest successful poll.",
				float64(st.LastSuccess.UnixNano())/1e9, "ups", ups)
		}

		// Values from the last good poll ride out a failed poll or two, but
		// past StaleAfter they're left out rather than repeated, so a dead
		// UPS or NUT server shows as a gap, not a flat line.
		if !fresh {
			continue
		}

		add(namespace+"_ups_info", "gauge", "UPS identity from NUT; always 1.", 1,
			"ups", ups,
			"ups_name", st.UpsName,
			"mfr", first(st.Vars, "device.mfr", "ups.mfr"),
			"model", first(st.Vars, "device.model", "ups.model"),
			"serial", first(st.Vars, "device.serial", "ups.serial"),
		)

		for name, raw := range st.Vars {
			if !r.exported(name) {
				continue
			}
			if name == "ups.status" {
				for flag, set := range statusFlags(raw) {
					add(namespace+"_ups_status", "gauge", "1 for each flag in NUT's ups.status, 0 for known flags that aren't set.",
						boolFloat(set), "ups", ups, "flag", flag)
				}
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil {
				continue // strings like ups.model, or ups.status handled above
			}
			add(MetricName(name), "gauge", fmt.Sprintf("NUT variable %s.", name), v, "ups", ups)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(families)) {
		f := families[name]
		slices.SortFunc(f.samples, func(a, b sample) int { return strings.Compare(a.labels, b.labels) })
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, f.help, name, f.typ); err != nil {
			return err
		}
		for _, s := range f.samples {
			if _, err := fmt.Fprintf(w, "%s{%s} %s\n", name, s.labels, strconv.FormatFloat(s.value, 'g', -1, 64)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) exported(name string) bool {
	if len(r.Include) > 0 {
		return slices.Contains(r.Include, name)
	}
	for _, e := range r.Exclude {
		if name == e || (strings.HasSuffix(e, ".") && strings.HasPrefix(name, e)) {
			return false
		}
	}
	return true
}

// MetricName converts a NUT variable name to a metric name:
// battery.charge becomes nut_battery_charge.
func MetricName(nutVar string) string {
	var b strings.Builder
	b.WriteString(namespace + "_")
	for _, c := range nutVar {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// statusFlags maps each known flag, plus any other flag present, to whether
// it's set.
func statusFlags(raw string) map[string]bool {
	flags := make(map[string]bool, len(KnownStatusFlags))
	for _, f := range KnownStatusFlags {
		flags[f] = false
	}
	for _, f := range strings.Fields(raw) {
		flags[f] = true
	}
	return flags
}

func first(vars map[string]string, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(vars[n]); v != "" {
			return v
		}
	}
	return ""
}

func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// formatLabels renders name/value pairs as `a="x",b="y"`.
func formatLabels(pairs []string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, pairs[i], escapeLabelValue(pairs[i+1])))
	}
	return strings.Join(parts, ",")
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabelValue(s string) string {
	return labelEscaper.Replace(s)
}
