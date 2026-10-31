# ESP32-Monitor

ESP32-S3 400×300 RLCD monitor, desktop Client, NAS Server and shared-UI simulator.

The five maintained documents below describe the current Client implementation and explicitly distinguish implemented code from planned integration and release validation.

| Document | Scope |
| --- | --- |
| [Server ↔ Client protocol v2](docs/SERVER_CLIENT_PROTOCOL_V2.md) | Registration, profiles, metric uploads and production Server gaps |
| [ESP32 ↔ host protocol](docs/ESP32_COMPANION_PROTOCOL_V1.md) | Companion business V1, Protobuf wire v2, LAN and serial |
| [Client capabilities](docs/CLIENT_CAPABILITIES.md) | GUI, collection, providers, platform limits and development commands |
| [V1 plan](docs/V1_RELEASE_PLAN.md) | Remaining work, milestones, deployment and release acceptance |
| [Firmware capability plan](docs/FIRMWARE_CAPABILITIES_PLAN.md) | Current firmware, three modes, hardware validation and build/test commands |

**Current Client uploads require v2; the production Server still exposes v1. Use the local sim-server for v2 integration until the Server is upgraded.**

Historical documents and evidence are preserved byte-for-byte under [.bak/docs-cleanup-2026-10-28](.bak/docs-cleanup-2026-10-28); [manifest.json](.bak/docs-cleanup-2026-10-28/manifest.json) records original paths and SHA-256 hashes. They are not current specifications. Licenses, asset provenance, dependency documentation, patch notes and AGENTS.md remain beside their sources.

[中文入口](README.zh_CN.md)

## Linux Client prerequisites

The Linux AppImage requires host-provided GTK4 and WebKitGTK 6.0. See the
[Linux setup instructions](client/packaging/linux/README.txt) for dependency
installation on Fedora, Debian and Ubuntu, and AppImage usage.

## Headless Agent (Linux)

Servers and NAS devices can run only the Agent: in-process collection and v2
upload, with no GUI, WebView or desktop dependencies. Run it as an ordinary
user; it must not run as root and needs no sudo, block-device write access or
SMART refresh privileges.

Build the Agent from source:

```sh
cd client
make build        # bin/monitor-agent

# Or cross-compile a static Linux binary elsewhere (adjust GOARCH for the target):
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o monitor-agent ./cmd/monitor-agent
```

Register the device once:

1. In the Server WebUI, add a device and copy its six-digit code (valid for 10 minutes, single use).
2. Exchange the code for the device token:

```sh
curl -sS -X POST https://server.example/v2/collector/register \
  -H 'Content-Type: application/json' -d '{"code":"123456"}'
```

3. Save the returned `token` to a private file; the Agent re-reads and validates it for every upload:

```sh
mkdir -p ~/.config/esp32-monitor
umask 077
printf '%s\n' '<token>' > ~/.config/esp32-monitor/collector-token
```

Create a configuration file (`upload.optional` starts empty; the required CPU,
memory and uptime values are always uploaded):

```json
{"interval_seconds": 10, "selections": [], "upload": {"enabled": true, "interval_seconds": 10, "optional": []}}
```

For optional metrics, list object IDs in `upload.optional`, for example
`{"metric":"drive.read_rate","objects":[5]}`. Print available metrics and
object IDs with:

```sh
monitor-agent --collector --collector-state-dir ~/.local/state/esp32-monitor/collector --collector-scan
```

IDs are specific to the machine and must not be copied from another host.

Run the Agent:

```sh
monitor-agent --collector \
  --collector-state-dir ~/.local/state/esp32-monitor/collector \
  --collector-upload-url https://server.example/v2/collector/samples \
  --collector-upload-token-file ~/.config/esp32-monitor/collector-token \
  --collector-config ~/.config/esp32-monitor/collector.json
```

The Agent creates and requires a private `0700` state directory; the token file
must be `0600`. `--collector-config` is applied only on first start, while no
persisted configuration exists. Optional packages add metrics but are not
required: `udisks2` (cached SMART health), `nvtop` with `--snapshot` (GPU) and
`upower` (battery). Disk collection reads `/sys`, `/proc`, `statfs` and cached
UDisks2 properties only, so it does not wake sleeping disks. See
[Client capabilities](docs/CLIENT_CAPABILITIES.md) for the complete metric,
RAID and Linux limits.

## License

ESP32-Monitor is released under the MIT License; see [LICENSE](LICENSE). Third-party components and bundled asset licenses are listed in [client/packaging/THIRD_PARTY_NOTICES.txt](client/packaging/THIRD_PARTY_NOTICES.txt).
