# ESP32-Monitor

ESP32-S3 400×300反射式LCD监视器，包含桌面Client、NAS Server及复用固件UI的桌面模拟器。

主文档仅维护以下五份；事实以当前Client实现为准，计划与原生/实机验收分开标注：

| 文档 | 内容 |
| --- | --- |
| [Server与Client通信协议（v2）](docs/SERVER_CLIENT_PROTOCOL_V2.md) | 注册、档案、指标上报和生产Server待实现部分 |
| [ESP32与上位机通信协议](docs/ESP32_COMPANION_PROTOCOL_V1.md) | 业务V1、Protobuf线编码v2、LAN与串口 |
| [Client整体能力表](docs/CLIENT_CAPABILITIES.md) | 承接原METRICS.md全部七类要求，含GUI/采集/提供者/平台限制和开发入口 |
| [V1计划](docs/V1_RELEASE_PLAN.md) | 剩余工作、阶段、部署与发布门槛 |
| [ESP32固件计划能力表](docs/FIRMWARE_CAPABILITIES_PLAN.md) | 现有基础、三模式目标、实机验收和构建/测试入口 |

**当前Client指标上传只支持v2，正式Server仍为v1；升级前通过本地sim-server联调，不能宣称完整链路已可用。**

旧文档与历史证据按原路径保存在 [.bak/docs-cleanup-2026-10-28](.bak/docs-cleanup-2026-10-28)，[manifest.json](.bak/docs-cleanup-2026-10-28/manifest.json)记录原路径与SHA-256，未删除原内容。备份不再作为当前规范。许可证、素材来源、依赖组件说明、补丁说明和AGENTS.md保留原位；两个README只作导航。

[English entry](README.md)
