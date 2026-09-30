package nut

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jakerobb/nut-relay/internal/util"
)

func TestParseVarLine(t *testing.T) {
	tests := []struct {
		line    string
		ups     string
		wantKey string
		wantVal string
		wantOK  bool
	}{
		{
			line:    `VAR cyberpower battery.charge "100"`,
			ups:     "cyberpower",
			wantKey: "battery.charge",
			wantVal: "100",
			wantOK:  true,
		},
		{
			line:    `VAR cyberpower ups.status "OL CHRG"`,
			ups:     "cyberpower",
			wantKey: "ups.status",
			wantVal: "OL CHRG",
			wantOK:  true,
		},
		{
			line:    `VAR cyberpower ups.model "CyberPower CP1500PFCLCD"`,
			ups:     "cyberpower",
			wantKey: "ups.model",
			wantVal: "CyberPower CP1500PFCLCD",
			wantOK:  true,
		},
		{
			line:   `BEGIN LIST VAR cyberpower`,
			ups:    "cyberpower",
			wantOK: false,
		},
		{
			line:   `END LIST VAR cyberpower`,
			ups:    "cyberpower",
			wantOK: false,
		},
		{
			line:   `VAR other.ups battery.charge "50"`,
			ups:    "cyberpower",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			key, val, ok := parseVarLine(tc.line, tc.ups)
			if ok != tc.wantOK {
				t.Errorf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if ok {
				if key != tc.wantKey {
					t.Errorf("key: got %q, want %q", key, tc.wantKey)
				}
				if val != tc.wantVal {
					t.Errorf("val: got %q, want %q", val, tc.wantVal)
				}
			}
		})
	}
}

// startFakeNUTServer starts a TCP server that simulates a NUT server for testing.
// It returns the listener address and a channel that signals shutdown.
func startFakeNUTServer(t *testing.T, upsName string, vars map[string]string, requireAuth bool) (string, int, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { util.CloseCleanly(ln) })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeNUT(conn, upsName, vars, requireAuth)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := 0
	_, err = fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		slog.Error("failed to parse port", "input", portStr, "err", err)
	}
	return host, port, err
}

func handleFakeNUT(conn net.Conn, upsName string, vars map[string]string, requireAuth bool) {
	defer util.CloseCleanly(conn)
	r := bufio.NewReader(conn)

	send := func(s string) {
		_, err := fmt.Fprintf(conn, "%s\n", s)
		if err != nil {
			slog.Error("failed to send command", "command", s, "err", err)
		}
	}

	authed := !requireAuth

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case strings.HasPrefix(line, "USERNAME "):
			send("OK")
		case strings.HasPrefix(line, "PASSWORD "):
			authed = true
			send("OK")
		case line == "LIST VAR "+upsName:
			if !authed {
				send("ERR ACCESS-DENIED")
				return
			}
			send("BEGIN LIST VAR " + upsName)
			for k, v := range vars {
				send(fmt.Sprintf(`VAR %s %s "%s"`, upsName, k, v))
			}
			send("END LIST VAR " + upsName)
		case line == "LOGOUT":
			send("OK Goodbye")
			return
		default:
			send("ERR UNKNOWN-COMMAND")
		}
	}
}

func TestFetchVars_NoAuth(t *testing.T) {
	upsName := "cyberpower"
	want := map[string]string{
		"battery.charge":  "100",
		"battery.runtime": "3600",
		"ups.status":      "OL",
		"ups.load":        "15",
	}

	host, port, _ := startFakeNUTServer(t, upsName, want, false)

	got, err := FetchVars(host, port, upsName, "plain", false, "", "")
	if err != nil {
		t.Fatalf("FetchVars: %v", err)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("var %s: got %q, want %q", k, got[k], v)
		}
	}
}

func TestFetchVars_WithAuth(t *testing.T) {
	upsName := "rack"
	want := map[string]string{
		"battery.charge": "85",
		"ups.status":     "OL CHRG",
	}

	host, port, err := startFakeNUTServer(t, upsName, want, true)

	got, err := FetchVars(host, port, upsName, "plain", false, "admin", "secret")
	if err != nil {
		t.Fatalf("FetchVars: %v", err)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("var %s: got %q, want %q", k, got[k], v)
		}
	}
}

// startSilentServer accepts connections and reads from them, but never
// writes anything back: a NUT server that's hung.
func startSilentServer(t *testing.T) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { util.CloseCleanly(ln) })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer util.CloseCleanly(conn)
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func shortenTimeouts(t *testing.T) {
	t.Helper()
	oldDial, oldSession := dialTimeout, sessionTimeout
	dialTimeout, sessionTimeout = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { dialTimeout, sessionTimeout = oldDial, oldSession })
}

func TestFetchVars_TimesOutOnSilentServer(t *testing.T) {
	for _, mode := range []string{"plain", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			shortenTimeouts(t)
			host, port := startSilentServer(t)

			start := time.Now()
			_, err := FetchVars(host, port, "ups", mode, true, "", "")
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("FetchVars: expected a timeout error, got nil")
			}
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Errorf("FetchVars: expected a timeout error, got %v", err)
			}
			if elapsed > 2*time.Second {
				t.Errorf("FetchVars took %v; the session deadline didn't bound it", elapsed)
			}
		})
	}
}

func TestFetchVars_BareERR(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { util.CloseCleanly(ln) })

	// Mimics the UniFi UPS Tower asked for a UPS name it doesn't have: a
	// bare "ERR", then it hangs up.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer util.CloseCleanly(conn)
		if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "ERR\n")
	}()

	addr := ln.Addr().(*net.TCPAddr)
	_, err = FetchVars(addr.IP.String(), addr.Port, "wrong-name", "plain", false, "", "")
	if err == nil || !strings.Contains(err.Error(), "NUT error: ERR") {
		t.Errorf("FetchVars: expected a NUT error for a bare ERR, got %v", err)
	}
}
