// Package uploadv2 defines the Client to Server v2 wire contract.
package uploadv2

import "encoding/json"

// 此配置只保存在 Client；上传凭据由 Agent 单独持有。
type UploadConfig struct {
	Enabled         bool              `json:"enabled"`
	IntervalSeconds uint16            `json:"interval_seconds"`
	Optional        []UploadSelection `json:"optional"`
}

type UploadSelection struct {
	Metric  string   `json:"metric"`            // 第 2 节中的语义 ID
	Objects []uint16 `json:"objects,omitempty"` // 本机稳定对象 ID
	Detail  []uint16 `json:"detail,omitempty"`  // 仅 SMART 细项，必须显式选择
}

type Profile struct {
	Version         uint8             `json:"version"` // 固定为 2
	IntervalSeconds uint16            `json:"interval_seconds"`
	Enabled         []UploadSelection `json:"enabled"` // 仅可选项，含固定字段选择
	Inventory       Inventory         `json:"inventory"`
}

// 首次发送完整 Profile，后续仅发送差异；与 Profile 共用一个 revision。
type ProfilePatch []ProfileOperation

type ProfileOperation struct {
	Op    string          `json:"op"`              // add / replace / remove
	Path  string          `json:"path"`            // 受限 JSON Pointer 路径
	Value json.RawMessage `json:"value,omitempty"` // remove 时禁止 value
}

type Inventory struct {
	OS          *OSInfo         `json:"os,omitempty"`
	Product     *string         `json:"product,omitempty"`
	CPU         *CPUInfo        `json:"cpu,omitempty"`
	MemoryTotal *uint64         `json:"memory_total,omitempty,string"`
	Cores       []CoreInfo      `json:"cores,omitempty"`
	GPUs        []ObjectInfo    `json:"gpus,omitempty"`
	Drives      []DriveInfo     `json:"drives,omitempty"`
	Volumes     []VolumeInfo    `json:"volumes,omitempty"`
	Interfaces  []InterfaceInfo `json:"interfaces,omitempty"`
	Batteries   []ObjectInfo    `json:"batteries,omitempty"`
	Fans        []FanInfo       `json:"fans,omitempty"`
}

type OSInfo struct {
	Family  string  `json:"family"` // darwin / windows / linux
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Build   *string `json:"build,omitempty"`
}

type CPUInfo struct {
	Model            *string `json:"model,omitempty"`
	PhysicalCores    *uint16 `json:"physical_cores,omitempty"`
	LogicalCores     *uint16 `json:"logical_cores,omitempty"`
	PerformanceCores *uint16 `json:"performance_cores,omitempty"`
	EfficiencyCores  *uint16 `json:"efficiency_cores,omitempty"`
}

type ObjectInfo struct {
	ID uint16 `json:"id"`
}

type CoreInfo struct {
	ID    uint16  `json:"id"`
	Index uint16  `json:"index"`          // 平台逻辑 CPU 序号，不是物理核心数
	Kind  *string `json:"kind,omitempty"` // performance / efficiency / general
}

type DriveInfo struct {
	ID         uint16               `json:"id"`
	Model      *string              `json:"model,omitempty"`
	TotalBytes *uint64              `json:"total,omitempty,string"`
	SMART      []SMARTAttributeInfo `json:"smart,omitempty"`
}

type VolumeInfo struct {
	ID         uint16   `json:"id"`
	DriveIDs   []uint16 `json:"drives,omitempty"`
	Filesystem *string  `json:"filesystem,omitempty"`
	TotalBytes *uint64  `json:"total,omitempty,string"`
}

type InterfaceInfo struct {
	ID     uint16  `json:"id"`
	Medium *string `json:"medium,omitempty"` // wifi / ethernet / other
}

type FanInfo struct {
	ID    uint16  `json:"id"`
	Scope string  `json:"scope"`         // cpu / gpu / system
	GPU   *uint16 `json:"gpu,omitempty"` // 仅已确认归属的 GPU ID
}

// ID 在所属 Drive 内唯一，定义变更分配新 ID；不要跨盘比较厂商原始值。
type SMARTAttributeInfo struct {
	ID      uint16 `json:"id"`
	Key     string `json:"key"`     // 如 nvme.media_errors / ata.5.raw
	Unit    string `json:"unit"`    // count / hours / percent / celsius / raw
	Scale10 int8   `json:"scale10"` // 实际值 = 十进制整数值 × 10^scale10
	Source  string `json:"source"`  // diskutil / nvme / ata 等明确来源
}

type SampleBatch struct {
	Revision uint64   `json:"r"`  // Server 确认的 Profile revision
	ID       string   `json:"id"` // 128 位随机数，32 个小写十六进制字符
	Samples  []Sample `json:"s"`
}

type Sample struct {
	Timestamp   int64   `json:"t"` // Unix 秒；一轮采集共享一次时间戳
	CPUUsage    *uint16 `json:"c"` // 必须存在且非 null，0–10000
	MemoryUsage *uint16 `json:"m"` // 必须存在且非 null，0–10000
	Uptime      *uint32 `json:"u"` // 必须存在且非 null，秒

	CPU        *CPUSample        `json:"cpu,omitempty"`
	Memory     *MemorySample     `json:"mem,omitempty"`
	Cores      []CoreSample      `json:"cores,omitempty"`
	GPUs       []GPUSample       `json:"gpu,omitempty"`
	Drives     []DriveSample     `json:"disk,omitempty"`
	Volumes    []VolumeSample    `json:"vol,omitempty"`
	Interfaces []InterfaceSample `json:"net,omitempty"`
	PublicIPv4 *IPv4Observation  `json:"ip,omitempty"`
	Batteries  []BatterySample   `json:"bat,omitempty"`
	Fans       []FanSample       `json:"fan,omitempty"`
}

type CPUSample struct {
	Load1       *uint32 `json:"l1,omitempty"` // 负载 × 1000，不是百分比
	Load5       *uint32 `json:"l5,omitempty"`
	Load15      *uint32 `json:"l15,omitempty"`
	Temperature *int16  `json:"t,omitempty"` // 摄氏度 × 10
}

type MemorySample struct {
	Used       *uint64 `json:"u,omitempty,string"` // 字节
	App        *uint64 `json:"a,omitempty,string"`
	Wired      *uint64 `json:"w,omitempty,string"`
	Compressed *uint64 `json:"c,omitempty,string"`
	SwapTotal  *uint64 `json:"st,omitempty,string"`
	SwapUsed   *uint64 `json:"su,omitempty,string"`
	Pressure   *uint8  `json:"p,omitempty"` // 0 normal / 1 warning / 2 critical
}

type CoreSample struct {
	ID    uint16  `json:"i"`
	Usage *uint16 `json:"u,omitempty"` // 百分比 × 100
}

type GPUSample struct {
	ID          uint16  `json:"i"`
	Usage       *uint16 `json:"u,omitempty"`
	Temperature *int16  `json:"t,omitempty"`
}

type DriveSample struct {
	ID        uint16       `json:"i"`
	ReadRate  *uint64      `json:"r,omitempty,string"` // B/s
	WriteRate *uint64      `json:"w,omitempty,string"`
	SMART     *SMARTSample `json:"sm,omitempty"`
}

type SMARTSample struct {
	Timestamp int64             `json:"t"`           // 实际读取时间
	Healthy   *bool             `json:"h,omitempty"` // false 是已知故障
	Values    map[uint16]string `json:"v,omitempty"` // 细项 ID → 十进制整数文本
}

type VolumeSample struct {
	ID   uint16  `json:"i"`
	Used *uint64 `json:"u,omitempty,string"`
}

type InterfaceSample struct {
	ID           uint16    `json:"i"`
	Up           *bool     `json:"up,omitempty"`
	UploadRate   *uint64   `json:"tx,omitempty,string"`
	DownloadRate *uint64   `json:"rx,omitempty,string"`
	IPv4         *[]string `json:"ip,omitempty"` // [] 表示本次确认无地址
}

type IPv4Observation struct {
	Timestamp int64  `json:"t"` // 实际查询时间，不能将缓存当作刚查询
	Address   string `json:"v"` // 有效公网 IPv4；失败则省略整个 observation
}

type BatterySample struct {
	ID       uint16  `json:"i"`
	Level    *uint16 `json:"l,omitempty"`
	Charging *bool   `json:"c,omitempty"`
}

type FanSample struct {
	ID  uint16  `json:"i"`
	RPM *uint32 `json:"r,omitempty"`
}
