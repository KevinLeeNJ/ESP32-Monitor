# Monitor Server V1

This directory contains the Go service that runs on the NAS side of ESP32-Monitor. It receives Telegraf host metrics, stores them in SQLite, and provides the dashboard with a summary of online devices. The implementation follows [`../docs/DEVICES_DATA_SOURCE_V1_PLAN.md`](../docs/DEVICES_DATA_SOURCE_V1_PLAN.md).

[简体中文](README.zh_CN.md)

The service uses Go's standard HTTP and JSON packages, Charm Huh v2 for the terminal configuration UI, `modernc.org/sqlite`, `go.yaml.in/yaml/v3`, `golang.org/x/time/rate`, `golang.org/x/term`, and `gofrs/flock`. Source code, dependency locks, sample configuration, and development notes are kept in this directory.

## Development and tests

Go 1.26 or newer is required. Dependency versions are pinned in `go.mod` and `go.sum`.

```sh
cd server
go mod download
make test-ut
make test-race
make test-api
make vet
make build
```

Unit tests do not listen on a network, access a NAS, or start Telegraf. HTTP handlers are tested with `httptest.NewRecorder` and a repository substitute. SQLite repository tests use a separate file in `t.TempDir()` and a fixed clock. Coverage files are ignored locally.

Run the configuration UI with `bin/monitor-server configure --config /path/to/monitor.yaml`. Use the arrow keys to select, Enter to confirm, Tab / Shift+Tab to move between input fields, and Esc to return to the current menu. Esc or Ctrl+C in the main menu requests exit; unsaved changes require confirmation before being discarded. Confirmation buttons for deletion, token rotation, and saving default to “Return”. The main menu and device list hide tokens; choose View to reveal one. Newly generated tokens are printed after a successful save. Restart the Server process manually after saving configuration changes.

Validation errors appear in place and can be corrected without leaving the menu. The UI resizes with the terminal and `/` filters choices. With `TERM=dumb`, Huh falls back to numbered options. Unit tests use a replaceable prompt interface to exercise configuration transactions, token operations, sorting, cancellation, capacity limits, and concurrency. PTY tests cover key navigation, input correction, window resizing, save, deletion, and terminal restoration on exit.

On macOS or Linux, install the Python dependency used only for terminal tests, then run:

```sh
python3 -m venv /tmp/monitor-tui-venv
/tmp/monitor-tui-venv/bin/python -m pip install -r tests/e2e/requirements-tui.txt
PATH="/tmp/monitor-tui-venv/bin:$PATH" make test-tui
```

These tests use the [Huh configuration API](https://github.com/charmbracelet/huh) and [pyte terminal emulator](https://pyte.readthedocs.io/en/latest/tutorial.html). pyte is test-only and is not included in the Server binary.

`make test-api` enables the `integration` build tag and calls the production HTTP Server and SQLite through a real loopback temporary port. Configuration, random tokens, and the database live in temporary directories. Tests use the normal Server parameters and an independent SQL connection to inspect persisted state. Coverage includes transaction rollback, duplicates and out-of-order samples, offline/TTL behavior, concurrent rate limiting, service recreation, and credential changes. For the full integration suite, run `go test -race -tags=integration -count=1 ./...`; for static analysis, run `go vet -tags=integration ./...`.

```text
cmd/monitor-server/  command entry point, process lifecycle, hourly cleanup
internal/config/     YAML schema, strict validation, random tokens, atomic 0600 writes, conflict checks
internal/cli/        first-run setup, device management, display order, token view and rotation
internal/metrics/    Telegraf batch JSON validation, host removal, tag normalization
internal/storage/    schema version, WAL/foreign keys/busy timeout, transactions, latest/history, cleanup
internal/panel/      online filtering, fixed field mapping, freshness and display formatting
internal/api/        HTTP server settings, auth separation, rate limiting, body limits, summary, probes
tests/integration/   loopback HTTP + SQLite API tests and redacted protocol fixtures
tests/e2e/           Telegraf and Server process end-to-end tests and collection fixtures
```

## Configure and run

The following commands configure and run the long-lived local service. API integration tests instead start a temporary HTTP Server and do not connect to real agents.

```sh
./bin/monitor-server configure --config ./config/monitor.yaml
./bin/monitor-server check-config --config ./config/monitor.yaml
./bin/monitor-server serve --config ./config/monitor.yaml
```

`configure` requires an interactive stdin/stdout terminal. On first run, set the listen address, SQLite path, public base URL, and register each client. After saving, the UI displays the new panel and upload tokens. Later runs provide menus for base settings, client creation/deletion/editing, display order, token viewing, and token rotation. Device IDs remain stable; editing a name, mount point, or order does not rotate its token. Canceling or interrupting input does not save. If another editor changes the configuration, the UI refuses to overwrite it. View reads the staged configuration; token changes take effect after save and Server restart.

Tokens are generated from a cryptographic random source as 32 bytes encoded in 64 lowercase hexadecimal characters. The read-only token must differ from every upload token. Configuration files must be regular files with mode `0600`. Writes use a same-directory temporary file, sync, atomic replacement, and an edit lock. The retained `.lock` file is only a lock carrier and contains no credentials; do not remove it while another configuration wizard is running. Manually edited configuration files must also remain mode `0600`. Do not commit actual configuration files or include them in release packages. A redacted sample is available at `config/monitor.example.yaml`; its placeholder tokens intentionally fail validation.

SQLite paths are relative to the **Server process working directory**; use an absolute path for NAS deployments. At startup, the service creates a missing data directory and database file. Database files use mode `0600`, and newly created parent directories use `0700`. Store the database on local NAS storage rather than NFS/SMB. After changing configuration, run `check-config` before a controlled restart. Rotated tokens invalidate the previous token in the new process; a deleted client's token returns 401.

The setup wizard shows tokens for copying but does not generate Telegraf configuration snippets. Administrators configure upload URLs, headers, and mount points in the Telegraf template. Copy the panel base URL and read-only token into the ESP32 Devices configuration.

## HTTP contract

| Endpoint | Authorization | Success |
|---|---|---|
| `POST /v1/ingest/telegraf` | Corresponding agent's Bearer upload token | 204 after transaction commit, no body |
| `GET /v1/devices/summary` | Panel Bearer read-only token | 200 with full online-device snapshot, up to 4096 bytes |
| `GET /healthz` | None | 200 while the process is alive |
| `GET /readyz` | None | 200 if SQLite is readable and writable; otherwise 503 |

Uploads accept only `application/json`, identity content encoding, and the native `{"metrics":[...]}` batch object with Unix-second timestamps. Limits are 256 KiB and 100 metrics per batch; each metric may contain at most 16 tags and 64 fields, with names and tag values capped at 128 UTF-8 bytes. Only numeric fields from `cpu`, `mem`, `disk`, and `system` are retained. Other measurements and nonnumeric fields are skipped; a batch with no usable numeric fields returns 422. Any structural error, timestamp outside the allowed window, or non-finite number rejects the entire batch. The timestamp window is from 24 hours in the past to 30 seconds in the future. Each device may have at most 256 measurement-plus-tag combinations, including retained latest values. `host` is excluded from identity and combination keys; other tags are stored in sorted-key order.

Each agent has an independent token bucket that refills at one request per second (60 per minute on average) with a burst capacity of 10. Authenticated invalid uploads also consume quota. An upload token used to read the panel, or a read-only token used to upload, returns 403. Missing or unknown tokens return 401. Other status codes: 413 for oversized requests, 415 for unsupported media types, 400 for invalid JSON, 422 for invalid content, 429 for rate limiting, and 503 for storage failures. Error bodies contain only a short error code. Unknown routes return 404; unsupported methods return 405.

SQLite operations are serialized through a single connection with WAL, foreign keys, and a 5-second busy timeout. The history idempotency key is device / measurement / normalized tags / field / sample time. Duplicate keys keep the first value; out-of-order samples may add history but cannot replace latest. Device `last_seen_at` and samples commit in the same transaction. Agents not present in the current configuration, including deleted clients, cannot upload. Historical device records are retained according to the cleanup policy. History is pruned hourly in sample-time batches; latest values are retained. The schema uses `PRAGMA user_version=1`; unknown versions prevent startup. Use SQLite's online backup tools or a consistent backup with the service stopped; copying only the live main database file is insufficient.

The panel supports up to four devices and returns online devices in `panel.devices` order. The default online threshold is 45 seconds, and metric TTL is 45 seconds. CPU uses `cpu.usage_active` with `cpu=cpu-total`; MEM uses `mem.used_percent`; DISK uses `disk.used_percent` with the configured path. Tag selection must be unique; an expired candidate cannot be ignored to bypass ambiguity. Missing, stale, or ambiguous data returns `--` / null. Percentage text is the original value rounded to an integer; progress is clamped to 0–100. Text longer than 8 bytes returns `OVR` / null. Uptime must be a nonnegative integer number of seconds and is formatted as `12d 4h`, `3h 2m`, `2m 4s`, or `12s`, up to 8 characters. Names must contain 1–8 printable ASCII bytes. Summary responses are uncompressed and include `Cache-Control: no-store`; offline devices are omitted. Read failures return 503 rather than a fabricated empty list.

On SIGINT or SIGTERM, the process stops accepting requests and allows up to 10 seconds for shutdown. Built-in HTTP read-header/read/write/idle timeouts are 5/10/10/60 seconds. Per-handler context timeout is 8 seconds, and the header limit is 16 KiB. By default, the service listens only on `127.0.0.1:8080`; HTTPS terminates at the existing NAS reverse proxy, which should also limit upload body size. Structured logs include operation result, device ID, status, accepted field count, rejected batch count, duration, SQLite/WAL file sizes, and cleanup row counts. Tokens and raw requests are not logged.

## NAS binary and deployment

```sh
make build-linux-amd64
shasum -a 256 bin/monitor-server-linux-amd64
```

The binary uses `CGO_ENABLED=0` and pure-Go SQLite, so NAS runtime installation of Go, SQLite shared libraries, or containers is not required. The NAS deployment uses a non-root account and runs the service in the foreground; service supervision depends on the NAS service manager. The current host installation is `/home/admin/.local/opt/esp32-monitor/`, with `config/`, `bin/`, and a `data/` directory created at runtime. The service listens on `0.0.0.0:18080`; Nginx Proxy Manager forwards traffic through the NAS LAN address.

The macOS Telegraf loopback workflow can be run with `make test-telegraf` after installing Telegraf. It covers upload, 503/429 buffer retries, SQLite/summary values, Server process restart, offline removal after 45 seconds, and agent recovery. Redacted evidence is written to a temporary directory by default; see the loopback record. Collection fixtures are also part of `make test-api` compatibility checks.

The NAS loopback and remote Mac client HTTPS workflow checks 76 numeric fields against NAS SQLite, compares public and NAS-local summaries, and covers token isolation, duplicate submissions, offline removal, and recovery. It uses a temporary Telegraf build with Go 1.27.3 and does not replace the system installation.

## Official references

- [Telegraf JSON serializer: batch object and timestamp units](https://github.com/influxdata/telegraf/blob/master/plugins/serializers/json/README.md)
- [Telegraf HTTP output](https://github.com/influxdata/telegraf/blob/master/plugins/outputs/http/README.md)
- [CPU](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/cpu/README.md), [MEM](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/mem/README.md), [DISK](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/disk/README.md), and [system](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/system/README.md)
- [modernc SQLite driver and DSN PRAGMA](https://pkg.go.dev/modernc.org/sqlite)
- [YAML `Decoder.KnownFields`](https://pkg.go.dev/go.yaml.in/yaml/v3#Decoder.KnownFields)
- [Go HTTP Server](https://pkg.go.dev/net/http#Server), [token bucket](https://pkg.go.dev/golang.org/x/time/rate), [terminal detection](https://pkg.go.dev/golang.org/x/term), and [file locks](https://pkg.go.dev/github.com/gofrs/flock)
