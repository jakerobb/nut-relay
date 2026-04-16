package nut

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	"github.com/jakerobb/nut-influx-relay/internal/util"
)

// VarMap is a map of NUT variable names to their string values.
type VarMap map[string]string

// FetchVars connects to a NUT server, authenticates if credentials are provided,
// fetches all variables for the named UPS, and returns them as a VarMap.
// A new TCP connection is opened and closed for each call.
func FetchVars(host string, port int, upsName string, useTLS bool, tlsSkipVerify bool, username, password string) (VarMap, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	var conn net.Conn
	var err error

	if useTLS {
		conn, err = tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: tlsSkipVerify, //nolint:gosec // intentional per config
		})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
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

	// Authenticate if credentials provided.
	if username != "" {
		if err := send("USERNAME " + username); err != nil {
			return nil, fmt.Errorf("sending USERNAME: %w", err)
		}
		resp, err := readLine()
		if err != nil {
			return nil, fmt.Errorf("reading USERNAME response: %w", err)
		}
		if resp != "OK" {
			return nil, fmt.Errorf("USERNAME rejected: %s", resp)
		}
	}

	if password != "" {
		if err := send("PASSWORD " + password); err != nil {
			return nil, fmt.Errorf("sending PASSWORD: %w", err)
		}
		resp, err := readLine()
		if err != nil {
			return nil, fmt.Errorf("reading PASSWORD response: %w", err)
		}
		if resp != "OK" {
			return nil, fmt.Errorf("PASSWORD rejected: %s", resp)
		}
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
		if strings.HasPrefix(line, "ERR ") {
			return nil, fmt.Errorf("NUT error: %s", line)
		}
		// VAR <ups-name> <key> "<value>"
		key, value, ok := parseVarLine(line, upsName)
		if ok {
			vars[key] = value
		}
	}

	// Politely disconnect.
	_ = send("LOGOUT")

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
