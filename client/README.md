# Telegraf Client Setup

This guide covers running Telegraf on Windows, macOS, Linux, or a NAS to collect host metrics and upload them to ESP32-Monitor Server over HTTP POST. InfluxDB is not required. Use the Linux template on a NAS.

[简体中文](README.zh_CN.md)

Deployment steps: choose a template, prepare the server-side device and upload token, install Telegraf, verify collection and upload, then optionally configure a background service. For a first deployment, verify uploads in the foreground before enabling startup at boot.

## Templates

| Platform | Template | Default disk path |
|---|---|---|
| Windows | [Win/telegraf.conf](Win/telegraf.conf) | `C:` |
| macOS | [Mac/telegraf.conf](Mac/telegraf.conf) | `/` |
| Linux (including NAS) | [Linux/telegraf.conf](Linux/telegraf.conf) | `/` |

The templates follow Telegraf **1.40.2** configuration syntax and use Telegraf's cross-platform input and HTTP output plugins on Windows, macOS, and Linux.

## Prepare server details

Ask a Server administrator to register the device through the interactive configuration and issue its dedicated `write_token`. The administrator must also add the device to `panel.devices`. Clients cannot register themselves. Use a different upload token for each host; do not use the panel's `read_token`.

Copy the matching template to its deployment location and update these settings:

| Setting | Value |
|---|---|
| `outputs.http.url` | Server base URL plus `/v1/ingest/telegraf`, for example `https://monitor.kevinleenj.top:1443/v1/ingest/telegraf` |
| `outputs.http.headers.Authorization` | Keep the `Bearer ` prefix and replace `<UPLOAD_TOKEN>` with the 64-character hexadecimal upload token issued by Server |
| `inputs.disk.mount_points` | List the single mount point to display. It must exactly match this device's `disk_mount_path` on Server. |

For example, to monitor `/vol1` on a NAS, set `mount_points = ["/vol1"]` in the Linux template and `disk_mount_path: "/vol1"` for that device on Server. The Windows default is `C:` without a trailing slash. Use the disk `path` tag from `--test` output as the source of truth; the Server YAML value can be `disk_mount_path: 'C:'`.

The upload token determines device identity. Changing the hostname does not create a new device. Configure the card name, ordering, and disk selection on Server. Keep system time synchronized on both the client and server.

## Install Telegraf

Choose the package for your operating system and CPU architecture from the [official installation guide](https://docs.influxdata.com/telegraf/v1/install/) or [download page](https://www.influxdata.com/downloads/), and verify the official SHA-256 checksum. This repository provides configuration templates, not Telegraf binaries.

### Windows

Download and extract the official Windows ZIP. Put `telegraf.exe` in `C:\Program Files\Telegraf`, then copy `Win/telegraf.conf` from this directory to the same folder and configure it. The commands below use PowerShell. Run PowerShell as Administrator when installing the service.

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' version
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf' --test
```

The configuration contains a plaintext token. Use file security properties to restrict read access to administrators and the account that runs Telegraf. Use the same configuration for foreground testing and the service.

### macOS

With Homebrew installed:

```sh
brew install telegraf
telegraf version
mkdir -p "$HOME/.config/esp32-monitor"
chmod 700 "$HOME/.config/esp32-monitor"
cp client/Mac/telegraf.conf "$HOME/.config/esp32-monitor/telegraf.conf"
chmod 600 "$HOME/.config/esp32-monitor/telegraf.conf"
```

Run the copy command from the repository root, then edit the copied configuration. Alternatively, download the official macOS binary into a user-owned tools directory and use its absolute path in the commands below.

### Linux (including NAS)

Install a distribution package by following the official guide, or download the official Linux archive and locate its `telegraf` executable. The standalone-binary method below requires no root access and does not install a system service:

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.config/esp32-monitor"
chmod 700 "$HOME/.config/esp32-monitor"
# Replace the source path below with the extracted executable's actual path.
install -m 755 /path/to/extracted/telegraf "$HOME/.local/bin/telegraf"
cp client/Linux/telegraf.conf "$HOME/.config/esp32-monitor/telegraf.conf"
chmod 600 "$HOME/.config/esp32-monitor/telegraf.conf"
"$HOME/.local/bin/telegraf" version
```

Run the template copy command from the repository root, or use the template's absolute path. Then edit the deployed configuration. The service account must be able to read capacity information for the selected mount point.

## Verify collection and uploads

### Check collection only

macOS:

```sh
telegraf --config "$HOME/.config/esp32-monitor/telegraf.conf" --test
```

Linux:

```sh
"$HOME/.local/bin/telegraf" --config "$HOME/.config/esp32-monitor/telegraf.conf" --test
```

On Windows, use the PowerShell `--test` command above. Check for the following measurements, fields, and tags. The `--test` output is usually line protocol; this does not change the JSON format used by the HTTP output.

| Purpose | Required data |
|---|---|
| `CPU` | `cpu.usage_active`, with tag `cpu=cpu-total` |
| `MEM` | `mem.used_percent` |
| `DISK` | `disk.used_percent`, with a `path` tag equal to the mount path configured on Server |
| Uptime | `system.uptime`, an integer number of seconds |

`--test` does not run output plugins, so it cannot prove that an upload succeeded. CPU uses differential sampling; the first sample may be incomplete, so confirm it after Telegraf has run continuously.

### Run continuously and verify an upload

macOS:

```sh
telegraf --config "$HOME/.config/esp32-monitor/telegraf.conf"
```

Linux:

```sh
"$HOME/.local/bin/telegraf" --config "$HOME/.config/esp32-monitor/telegraf.conf"
```

Windows:

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf'
```

After at least 30 seconds, ask an administrator to check the Server `ingest` log. The matching `device_id` should return `status=204` with `accepted_fields>0`. Then use the panel's read-only token to call `GET /v1/devices/summary` and confirm that the device and its CPU, MEM, DISK, and uptime values appear. A successful `/readyz` response alone does not prove that metrics were uploaded. Press Ctrl+C to stop a foreground process.

By default, Telegraf collects and uploads every 10 seconds. HTTP requests use batched JSON with Unix-second timestamps and no compression. The templates retain numeric fields from the four input plugin groups; Server selects the three percentage fields listed above for display. After uploads stop for longer than the server's online timeout (45 seconds by default), the device is omitted from the next summary response.

## Optional: run in the background

Complete foreground upload verification first. Before configuring a background service on a host that already runs Telegraf, inspect its existing configuration and services to avoid running two upload processes for the same device.

### Windows service

Run PowerShell as Administrator and use the absolute configuration path when registering the service:

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf' service install
& 'C:\Program Files\Telegraf\telegraf.exe' service start
& 'C:\Program Files\Telegraf\telegraf.exe' service status
```

After changing the configuration, stop and start the service again. To remove it:

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' service stop
& 'C:\Program Files\Telegraf\telegraf.exe' service uninstall
```

Check Windows Event Viewer under **Windows Logs → Application** for startup errors. Runtime logs go to stderr by default. To save logs to a file, set `[agent]`'s `logfile` and grant the service account write access to that directory.

### macOS / Linux / NAS

Create a task in the host's existing service manager. Use the **absolute path to the Telegraf binary + `--config` + the absolute path to the configuration file**. For example, expand `$HOME` in a Linux launch command to the actual home directory:

```text
/home/your-user/.local/bin/telegraf --config /home/your-user/.config/esp32-monitor/telegraf.conf
```

Set the service account, restart policy, and optional start-at-boot behavior. On macOS, use launchd or a Homebrew service. Homebrew services read `etc/telegraf.conf` under the Homebrew prefix by default; if using one, first copy the configured file to `$(brew --prefix)/etc/telegraf.conf`, check its permissions, and then run `brew services start telegraf`. Linux distribution services typically read `/etc/telegraf/telegraf.conf` and may also load `telegraf.d`; inspect their startup arguments to avoid loading a default InfluxDB output or unrelated input configuration.

Use the Linux template and the NAS's own service manager on a NAS. Telegraf only needs to make outbound connections to Server; no reverse proxy or inbound port is required on the client. If the NAS and Server share a host, the NAS client can use `http://127.0.0.1:18080/v1/ingest/telegraf` (adjust the port to match the deployment). Other hosts should use the public HTTPS URL.

Logs go to stderr by default and can be collected by the service manager. With systemd, use `journalctl -u SERVICE_NAME -f`; otherwise, use the service manager's log viewer. Restart the service after configuration changes, then verify ingestion and the summary on Server.

## Troubleshooting

| Symptom | Check |
|---|---|
| HTTP 401 | Confirm the token is complete and belongs to this Server. Remove angle brackets and extra whitespace. |
| HTTP 403 | Check that the panel's read-only token was not used for uploads. |
| HTTP 404 / 502 | Check the port and `/v1/ingest/telegraf` path, and confirm the reverse proxy can reach Server. |
| TLS verification failure | Check the domain, certificate chain, local clock, and root certificates. Keep `insecure_skip_verify = false`. |
| HTTP 422 | Check system time and confirm the native JSON structure, time units, and plugin settings were not changed. |
| CPU / MEM / DISK displays `--` | Check that required fields exist and are fresh, CPU differential sampling has completed, and the disk `path` matches exactly. |
| Upload succeeds but no card appears | Confirm the token's device is listed under `panel.devices` and has not exceeded the online timeout. |
| Network interruption | The buffer is in memory and is lost when the process restarts. HTTP 429 and 5xx responses are retried; other statuses listed in the template are not retried, so correct the configuration. |

Keep live configurations outside the repository; do not replace the placeholder templates with real credentials. Check logs for tokens, host details, or disk identifiers before sharing them.

## Official references

- [Installation and platform guide](https://docs.influxdata.com/telegraf/v1/install/)
- [Windows service (v1.40.2)](https://github.com/influxdata/telegraf/blob/v1.40.2/docs/WINDOWS_SERVICE.md)
- [HTTP output](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/outputs/http/README.md) and [JSON serializer](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/serializers/json/README.md)
- [CPU](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/cpu/README.md), [memory](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/mem/README.md), [disk](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/disk/README.md), and [system uptime](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/system/README.md)
