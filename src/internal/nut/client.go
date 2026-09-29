package nut

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jakerobb/nut-influx-relay/internal/util"
)

// VarMap is a map of NUT variable names to their string values.
type VarMap map[string]string

// Bounds on a single poll, so an unresponsive NUT server fails the poll
// instead of blocking that UPS's collector goroutine forever. dialTimeout
// covers the TCP connect (and the TLS handshake in "tls" mode);
// sessionTimeout is one deadline for everything after that: STARTTLS, auth,
// LIST VAR and LOGOUT. Variables rather than constants so tests can shorten
// them.
var (
	dialTimeout    = 5 * time.Second
	sessionTimeout = 10 * time.Second
)

func connect(host string, port int, tlsMode string, tlsSkipVerify bool) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: dialTimeout}

	if tlsMode == "tls" {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			InsecureSkipVerify: tlsSkipVerify, //nolint:gosec // intentional per config
		})
		if err != nil {
			return nil, fmt.Errorf("connecting to %s: %w", addr, err)
		}
		if err := conn.SetDeadline(time.Now().Add(sessionTimeout)); err != nil {
			util.CloseCleanly(conn)
			return nil, fmt.Errorf("setting deadline: %w", err)
		}
		return conn, nil
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	// Set on the raw connection, so it also bounds the STARTTLS exchange and
	// carries over to the TLS connection wrapped around it below.
	if err := conn.SetDeadline(time.Now().Add(sessionTimeout)); err != nil {
		util.CloseCleanly(conn)
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	if tlsMode == "starttls" {
		tlsConn, err := startTLS(conn, tlsSkipVerify)
		if err != nil {
			util.CloseCleanly(conn)
			return nil, err
		}
		return tlsConn, nil
	}

	return conn, nil
}

func startTLS(conn net.Conn, tlsSkipVerify bool) (net.Conn, error) {
	if _, err := fmt.Fprintf(conn, "STARTTLS\n"); err != nil {
		return nil, fmt.Errorf("sending STARTTLS: %w", err)
	}
	r := bufio.NewReader(conn)
	resp, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading STARTTLS response: %w", err)
	}
	resp = strings.TrimRight(resp, "\r\n")
	if !strings.HasPrefix(resp, "OK") {
		return nil, fmt.Errorf("STARTTLS rejected: %s", resp)
	}
	tlsConn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: tlsSkipVerify, //nolint:gosec // intentional per config
	})
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake: %w", err)
	}
	return tlsConn, nil
}

func authenticate(send func(cmd string) error, readLine func() (string, error), username string, password string) error {
	if username != "" {
		if err := send("USERNAME " + username); err != nil {
			return fmt.Errorf("sending USERNAME: %w", err)
		}
		resp, err := readLine()
		if err != nil {
			return fmt.Errorf("reading USERNAME response: %w", err)
		}
		if resp != "OK" {
			return fmt.Errorf("USERNAME rejected: %s", resp)
		}
	}

	if password != "" {
		if err := send("PASSWORD " + password); err != nil {
			return fmt.Errorf("sending PASSWORD: %w", err)
		}
		resp, err := readLine()
		if err != nil {
			return fmt.Errorf("reading PASSWORD response: %w", err)
		}
		if resp != "OK" {
			return fmt.Errorf("PASSWORD rejected: %s", resp)
		}
	}

	return nil
}

// FetchVars connects to a NUT server, authenticates if credentials are provided,
// fetches all variables for the named UPS, and returns them as a VarMap.
// A new TCP connection is opened and closed for each call.
func FetchVars(host string, port int, upsName string, tlsMode string, tlsSkipVerify bool, username, password string) (VarMap, error) {
	conn, err := connect(host, port, tlsMode, tlsSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer util.CloseCleanly(conn)

	r := bufio.NewReader(conn)

	send := func(cmd string) error {
		_, err := fmt.Fprintf(conn, "%s\n", cmd)
		return err
	}

	readLine := func() (string, error) {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	err = authenticate(send, readLine, username, password)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Request variable list.
	if err := send("LIST VAR " + upsName); err != nil {
		return nil, fmt.Errorf("sending LIST VAR: %w", err)
	}

	vars := make(VarMap)
	beginMarker := fmt.Sprintf("BEGIN LIST VAR %s", upsName)
	endMarker := fmt.Sprintf("END LIST VAR %s", upsName)

	for {
		line, err := readLine()
		if err != nil {
			return nil, fmt.Errorf("reading LIST VAR response: %w", err)
		}
		if line == beginMarker {
			continue
		}
		if line == endMarker {
			break
		}
		// A bare "ERR" (no reason) is what the UniFi UPS Tower sends for an
		// unknown UPS name, before closing the connection.
		if line == "ERR" || strings.HasPrefix(line, "ERR ") {
			return nil, fmt.Errorf("NUT error: %s", line)
		}
		// VAR <ups-name> <key> "<value>"
		key, value, ok := parseVarLine(line, upsName)
		if ok {
			vars[key] = value
		}
	}

	// Disconnect politely.
	err = send("LOGOUT")
	if err != nil {
		slog.Error("failed to send LOGOUT to NUT server", "err", err)
	}

	return vars, nil
}

// parseVarLine parses a line of the form: VAR <upsName> <key> "<value>"
// Returns the key, value, and true on success.
func parseVarLine(line, upsName string) (string, string, bool) {
	// Expected: VAR <upsName> <key> "<value>"
	prefix := fmt.Sprintf("VAR %s ", upsName)
	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}
	rest := line[len(prefix):]

	// rest is: <key> "<value>"
	spaceIdx := strings.Index(rest, " ")
	if spaceIdx < 0 {
		return "", "", false
	}
	key := rest[:spaceIdx]
	valueRaw := rest[spaceIdx+1:]

	// Strip surrounding quotes.
	if len(valueRaw) >= 2 && valueRaw[0] == '"' && valueRaw[len(valueRaw)-1] == '"' {
		valueRaw = valueRaw[1 : len(valueRaw)-1]
	}

	return key, valueRaw, true
}
