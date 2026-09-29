# nut-influx-relay

A Go service that polls one or more NUT (Network UPS Tools) servers, writes UPS metrics
to InfluxDB, keeps the latest stats for each UPS in memory, and exposes a simple HTTP API.

---

## Purpose

- Replace `inputs.upsd` in a Telegraf/InfluxDB/Grafana homelab stack
- Support NUT servers that require TLS (e.g. UniFi UPS Tower), which Telegraf's native
  `inputs.upsd` plugin cannot handle
- Support reading from multiple NUT servers
- Expose a lightweight HTTP API and web UI for UPS status, replacing `edgd1er/webnut`
- Run as a long-lived Docker service

---

## Repository Layout

```
nut-influx-relay/
├── CLAUDE.md
├── Dockerfile
├── .dockerignore
├── .gitignore
├── .github/
│   └── workflows/
│       └── docker-publish.yml
├── src/
│   ├── go.mod
│   ├── go.sum
│   └── cmd/
│       └── main.go          # entry point
│   └── internal/
│       ├── config/
│       │   └── config.go    # config file parsing + validation
│       ├── nut/
│       │   └── client.go    # NUT wire protocol client (plain + TLS)
│       ├── store/
│       │   └── store.go     # in-memory stats store
│       ├── collector/
│       │   └── collector.go # per-UPS poll goroutines
│       ├── influx/
│       │   └── writer.go    # InfluxDB v2 line protocol writer
│       └── api/
│           └── api.go       # HTTP handlers
```

---

## Configuration

Mounted at `/etc/nut-influx-relay/config.yaml`. Path overridable via `CONFIG_PATH` env var.

`url`, `token`, `username`, and `password` may be specified as literal strings or as env var references using `${VAR_NAME}` syntax.

### Example config.yaml

```yaml
poll_interval: 10s
http_port: 8080

influxdb:
  url: http://influxdb:8086
  token: ${INFLUXDB_TOKEN}
  org: home
  bucket: telegraf
  measurement: upsd      # must match what Telegraf's inputs.upsd would write

upses:
  - label: rack
    host: nut-upsd
    port: 3493
    ups_name: cyberpower
    tls_mode: plain        # plain (default), tls, or starttls
    username: ${NUT_USER}
    password: ${NUT_PASSWORD}

  - label: office
    host: 192.168.0.9
    port: 3493
    ups_name: office-ups
    tls_mode: starttls     # the UniFi UPS Tower speaks STARTTLS, not implicit TLS
    tls_skip_verify: true  # its certificate is self-signed
    # no username/password -- UniFi Tower allows unauthenticated reads
```

---

### NUT variable → struct field mapping

| NUT variable        | Struct field         |
|---------------------|----------------------|
| battery.charge      | BatteryCharge        |
| battery.voltage     | BatteryVoltage       |
| battery.runtime     | BatteryRuntime       |
| battery.low         | BatteryLow           |
| input.voltage       | InputVoltage         |
| input.frequency     | InputFrequency       |
| output.voltage      | OutputVoltage        |
| output.current      | OutputCurrent        |
| output.power        | OutputPower          |
| output.frequency    | OutputFrequency      |
| ups.status          | Status               |
| ups.load            | Load                 |
| ups.model           | Model                |
| ups.serial          | Serial               |
| ups.mfr             | Manufacturer         |

Any NUT variables not in this mapping are silently ignored.

---

## In-Memory Store

On each successful poll:
1. Build a new `UpsStats`
2. Call `store.Store(label, stats)`
3. Write to InfluxDB

On failed poll: 
* log the error
* do NOT update the store (preserve last known state),
* do NOT write to InfluxDB.

---

## NUT Protocol Client

NUT is a simple line-based TCP protocol:

```
Client: USERNAME <username>\n       (if auth required)
Client: PASSWORD <password>\n       (if auth required)
Client: LIST VAR <upsname>\n
Server: BEGIN LIST VAR <upsname>
Server: VAR <upsname> <key> "<value>"
Server: VAR <upsname> <key> "<value>"
...
Server: END LIST VAR <upsname>
Client: LOGOUT\n
```

The UniFi UPS Tower offers TLS via STARTTLS and, by default, doesn't require authentication. Other NUT servers may require authentication but 
not TLS. This app is written to support all four combinations of TLS/plain and authenticated/unauthenticated.

Per-connection flow:
1. Dial TCP (plain, TLS, or plain then STARTTLS, per config). The connect (plus the TLS handshake in `tls` mode) times
   out after 5s; everything after it shares one 10s deadline, so a hung server fails that poll instead of stalling
   the UPS's collector.
2. Authenticate if username+password configured
3. Send `LIST VAR <upsname>`
4. Read and parse response lines until `END LIST VAR`
5. Send `LOGOUT`
6. Close connection

Open a new connection on every poll — do not maintain persistent connections.

---

## Collector

One goroutine per UPS, started at service startup. Each goroutine:
- Ticks on `poll_interval`
- Connects to its NUT server, fetches stats, closes connection
- On success: updates store, writes to InfluxDB
- On failure: logs error with UPS label, continues (does not crash)

---

## InfluxDB Writer

Write using the InfluxDB v2 line protocol over HTTP (no SDK — keep dependencies minimal).

Endpoint: `POST /api/v2/write?org=<org>&bucket=<bucket>&precision=ns`

Headers:
- `Authorization: Token <token>`
- `Content-Type: text/plain; charset=utf-8`

Line protocol format — match `inputs.upsd` field names exactly so existing Grafana
dashboards work without changes:

```
upsd,ups_label=rack,serial=XXXX battery_charge_percent=100,time_left_ns=3106000000000i,load_percent=10,input_voltage=123.3,output_voltage=123.3,ups_status="OL",status_flags=1i 1744900000000000000
```

Tag keys: `ups_label`, `serial`
Field keys: all numeric UpsStats fields that are non-nil, plus `ups_status` (string field)
  and `status_flags` (integer, derived from Status string).

### status_flags mapping (matching inputs.upsd convention)

| ups.status contains | status_flags |
|---------------------|--------------|
| OL                  | 8            |
| OB                  | 16           |
| LB                  | 32           |
| CHRG                | 256          |
| DISCHRG             | 512          |

Flags are bitwise OR'd for compound states (e.g. `OL CHRG` → 8|256 = 264).

### Field name mapping for InfluxDB line protocol

| UpsStats field  | Line protocol field name |
|-----------------|--------------------------|
| BatteryCharge   | battery_charge_percent   |
| BatteryRuntime  | time_left_ns (×1e9, int) |
| Load            | load_percent             |
| InputVoltage    | input_voltage            |
| OutputVoltage   | output_voltage           |
| OutputCurrent   | output_current           |
| OutputPower     | output_power             |
| BatteryVoltage  | battery_voltage          |
| InputFrequency  | input_frequency          |
| OutputFrequency | output_frequency         |

Null values are omitted when writing to InfluxDB

---

## HTTP API

Bind to `0.0.0.0:<http_port>`.

### GET /health
Returns 200 OK with `{"status":"ok"}`. Always responds even if no UPS data yet.

### GET /ups
Returns JSON array of all `*UpsStats` currently in the store. If a UPS has not yet been
successfully polled, it is omitted (not returned as null). Returns `[]` if store is empty.

### GET /ups/{label}
Returns a single `*UpsStats` as a JSON object. Returns 404 with
`{"error":"not found"}` if label unknown or not yet polled.

## Side goals
- Avoid unnecessary dependencies like NUT client libraries and InfluxDB SDKs.
- Emphasis on testability and maintainability
