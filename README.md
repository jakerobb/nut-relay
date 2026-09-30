# nut-relay

A Go service that polls one or more NUT (Network UPS Tools) servers, keeps the latest
variables for each UPS in memory, and relays them to one of two outputs:

- **InfluxDB:** writes each poll to InfluxDB v2 with the line protocol (the default)
- **Prometheus:** serves them as metrics on `/metrics`

It also serves a small JSON API with every UPS's latest variables.

Formerly `nut-influx-relay`, which only wrote to InfluxDB. Existing configs keep working
unchanged; see [Upgrading from nut-influx-relay](#upgrading-from-nut-influx-relay).

---

## Purpose

- Replace `inputs.upsd` in a Telegraf/InfluxDB/Grafana stack, or export to Prometheus
- Support NUT servers that need TLS or STARTTLS (Telegraf's `inputs.upsd` can't), and
  ones that need a login
- Work with NUT servers that implement only part of the protocol. The UniFi UPS Tower
  answers `LIST VAR` but returns `ERR INVALID-ARGUMENT` for `LIST CLIENT`,
  `GET NUMLOGINS` and `LIST RW`, which breaks clients built on the `go.nut` library
  (such as `DRuggeri/nut_exporter`). This one only ever sends `LIST VAR`.
- Read from several NUT servers from one process
- Run as a long-lived container

---

## Repository Layout

```
nut-relay/
├── Dockerfile
├── .dockerignore
├── .gitignore
├── .github/
│   └── workflows/
│       └── docker-publish.yml
└── src/
    ├── go.mod
    ├── go.sum
    ├── cmd/
    │   └── main.go            # entry point; picks the output
    └── internal/
        ├── config/
        │   └── config.go      # config file parsing + validation
        ├── nut/
        │   └── client.go      # NUT wire protocol client (plain, TLS, STARTTLS)
        ├── store/
        │   └── store.go       # in-memory state per UPS
        ├── collector/
        │   └── collector.go   # per-UPS poll goroutines
        ├── influx/
        │   ├── mapping.go     # NUT variables → InfluxDB fields
        │   └── writer.go      # InfluxDB v2 line protocol writer
        ├── metrics/
        │   └── metrics.go     # Prometheus text format
        └── api/
            └── api.go         # HTTP handlers
```

---

## Configuration

Read from `CONFIG_PATH` if set, else `/etc/nut-relay/config.yaml`, else
`/etc/nut-influx-relay/config.yaml` (the old location). Set `LOG_LEVEL=debug` for a log
line per poll.

`url`, `token`, `username`, and `password` may be literal strings or env var references
using `${VAR_NAME}` syntax.

`output` picks **one** output per process: `influxdb` (the default) or `prometheus`. To
feed both, run two instances. Settings for the other output are rejected rather than
ignored, so a config that mixes them fails at startup.

### Settings for both outputs

```yaml
output: influxdb        # influxdb (default) or prometheus
poll_interval: 10s      # required
http_port: 8080         # default 8080

upses:
  - label: rack          # required, unique. The InfluxDB ups_label tag, or the
                         # Prometheus ups label
    host: nut-upsd       # required
    port: 3493           # default 3493
    ups_name: cyberpower # required: the UPS's name on that NUT server
    tls_mode: plain      # plain (default), tls, or starttls
    username: ${NUT_USER}
    password: ${NUT_PASSWORD}

  - label: office
    host: 192.168.0.9
    ups_name: office-ups
    tls_mode: starttls     # the UniFi UPS Tower offers STARTTLS; plain reads work too
    tls_skip_verify: true  # its certificate is self-signed
    # no username/password -- the Tower allows unauthenticated reads
```

### InfluxDB output

```yaml
influxdb:
  url: http://influxdb:8086   # required
  token: ${INFLUXDB_TOKEN}
  org: home
  bucket: telegraf
  measurement: upsd           # default upsd, matching Telegraf's inputs.upsd

# Optional. Replaces the default mappings below.
# field_mappings:
#   - {nut_var: battery.charge, influx_field: battery_charge_percent, type: float}

# Optional. Added to the default (or explicit) mappings.
extra_field_mappings:
  - nut_var: ups.realpower
    influx_field: real_power_watts
    type: float            # float, int or string (default string)
```

### Prometheus output

```yaml
output: prometheus

# Optional. If set, exactly these NUT variables are exported (ups.status included).
# If not, every numeric variable is, except driver.*, ups.productid and ups.vendorid.
# variables: [battery.charge, battery.runtime, ups.load, ups.realpower, ups.status]
```

---

## InfluxDB output

On each successful poll, the configured field mappings turn NUT variables into fields,
and one line is POSTed to InfluxDB with the v2 line protocol over HTTP (no SDK).

Endpoint: `POST /api/v2/write?org=<org>&bucket=<bucket>&precision=ns`

Headers:
- `Authorization: Token <token>`
- `Content-Type: text/plain; charset=utf-8`

```
upsd,ups_label=rack,serial=XXXX battery_charge_percent=100,battery_runtime_seconds=3106i,load_percent=10,input_voltage=123.3,ups_status="OL" 1744900000000000000
```

Tags: `ups_label` (the configured label) and `serial` (always `ups.serial`, whatever the
mappings). Fields: every mapped NUT variable the UPS reports that parses as its type.
Variables the UPS doesn't report are left out. A failed poll writes nothing, and a failed
write is logged and doesn't affect the poll.

### Default field mappings

| NUT variable       | InfluxDB field            | Type   |
|--------------------|---------------------------|--------|
| battery.charge     | battery_charge_percent    | float  |
| battery.runtime    | battery_runtime_seconds   | int    |
| battery.voltage    | battery_voltage           | float  |
| battery.low        | battery_low               | float  |
| input.voltage      | input_voltage             | float  |
| input.frequency    | input_frequency           | float  |
| output.voltage     | output_voltage            | float  |
| output.current     | output_current            | float  |
| output.power       | output_power              | float  |
| output.frequency   | output_frequency          | float  |
| ups.realpower      | real_power_watts          | float  |
| ups.power          | apparent_power_va         | float  |
| ups.status         | ups_status                | string |
| ups.load           | load_percent              | float  |
| ups.model          | model                     | string |
| ups.mfr            | manufacturer              | string |

---

## Prometheus output

`GET /metrics`, in the Prometheus text format. Every metric has a `ups` label, the
UPS's configured `label`.

| Metric | Type | Meaning |
|---|---|---|
| `nut_<variable>` | gauge | Each numeric NUT variable, with dots (and anything else not allowed in a metric name) as underscores: `battery.charge` → `nut_battery_charge` |
| `nut_ups_status{flag}` | gauge | `ups.status` split into flags: 1 for each flag present. The common flags (OL, OB, LB, HB, RB, CHRG, DISCHRG, BYPASS, CAL, OFF, OVER, TRIM, BOOST, FSD) are always exported, as 0 when absent, so alerts on them always have a series |
| `nut_ups_info{ups_name,mfr,model,serial}` | gauge | Always 1. Identity from `device.*`, falling back to `ups.*` |
| `nut_up` | gauge | 1 if the latest poll succeeded, else 0 |
| `nut_last_success_timestamp_seconds` | gauge | Unix time of the latest successful poll |
| `nut_polls_total` | counter | Polls attempted |
| `nut_poll_failures_total` | counter | Polls that failed |

Non-numeric variables (other than `ups.status`) aren't exported as metrics; the JSON
API has them.

**Stale data:** after a failed poll, the last good values are still exported for up to
three poll intervals, so one blip doesn't leave a gap. After that the NUT variables,
`nut_ups_status` and `nut_ups_info` are left out until a poll succeeds again, so a dead
UPS shows as missing data rather than a flat line. `nut_up` and the counters are always
exported.

Scraping never touches the NUT servers: it reads what the collectors last stored.

---

## Collector

One goroutine per UPS, started at service startup. Each goroutine:
- Polls immediately, then on every `poll_interval`
- Connects to its NUT server, fetches all variables, closes the connection
- On success: replaces that UPS's stored variables, and (InfluxDB output) writes them
- On failure: logs the error and records it, keeping the last good variables

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

Supports all four combinations of TLS/plain and authenticated/unauthenticated.

Per-connection flow:
1. Dial TCP (plain, TLS, or plain then STARTTLS, per config). The connect (plus the TLS handshake in `tls` mode) times
   out after 5s; everything after it shares one 10s deadline, so a hung server fails that poll instead of stalling
   the UPS's collector.
2. Authenticate if username+password configured
3. Send `LIST VAR <upsname>`
4. Read and parse response lines until `END LIST VAR` (an `ERR` line fails the poll)
5. Send `LOGOUT`
6. Close connection

A new connection is opened on every poll; no persistent connections.

---

## HTTP API

Binds to `0.0.0.0:<http_port>`.

### GET /metrics
Prometheus metrics, above. Only with `output: prometheus`; 404 otherwise.

### GET /health
Returns 200 OK with `{"status":"ok"}`, whether or not any UPS is reachable.

### GET /ups
JSON array of every configured UPS's state, sorted by label: all its NUT variables as
strings (`vars`, null until a poll succeeds), `last_success`, `last_attempt`,
`last_error`, and poll counts.

### GET /ups/{label}
One UPS's state as a JSON object. 404 with `{"error":"not found"}` for an unknown label.

---

## Upgrading from nut-influx-relay

- **Config:** unchanged. With no `output`, it's `influxdb`, and the lines written are the
  same. The file is still found at `/etc/nut-influx-relay/config.yaml`.
- **Image:** published as both `jakerobb/nut-relay` and `jakerobb/nut-influx-relay`.
- **JSON API:** `/ups` and `/ups/{label}` changed shape. They now return every raw NUT
  variable (`vars`) plus poll status, instead of the mapped InfluxDB fields, and list a
  UPS before its first successful poll too.
- **Config validation** is a little stricter: at least one UPS, unique labels, a positive
  `poll_interval`.

---

## Image

Published to Docker Hub as `jakerobb/nut-relay` (and `jakerobb/nut-influx-relay`) on
every push to `main`, tagged `latest` and `YYYYMMDD`, for `linux/amd64` and
`linux/arm64`. Built from `scratch`, running as UID 65532. The build runs `go vet` and
the tests, so a failure blocks the publish.

## Side goals
- No unnecessary dependencies: no NUT client library, no InfluxDB SDK, no Prometheus
  client library.
- Emphasis on testability and maintainability
