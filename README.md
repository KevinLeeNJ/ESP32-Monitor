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

See the [Linux setup instructions](README.zh_CN.md#linux-client-使用准备) for Debian 13 package installation and AppImage usage.

## License

ESP32-Monitor is released under the MIT License; see [LICENSE](LICENSE). Third-party components and bundled asset licenses are listed in [client/packaging/THIRD_PARTY_NOTICES.txt](client/packaging/THIRD_PARTY_NOTICES.txt).
