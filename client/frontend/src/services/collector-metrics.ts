import type { CollectorCapabilities, CollectorConfig, CollectorSnapshot, CollectorUploadConfig, CollectorUploadSelection, CollectorV2Capabilities, CollectorV2Metric } from "../types/collector.js";

export const requiredMetrics = new Set(["cpu.usage", "memory.usage", "memory.total", "memory.used", "system.uptime"]);
export const metricGroups = ["CPU", "内存（Swap）", "GPU", "磁盘", "网络", "电池", "系统指标"];
export const metricNames: Record<string, string> = {
  "cpu.usage": "总体 CPU 占用率", "core.usage": "逐核 CPU 占用率", "cpu.model": "CPU 型号",
  "cpu.physical_cores": "物理核心数", "cpu.logical_cores": "逻辑核心数", "cpu.performance_cores": "性能核心数", "cpu.efficiency_cores": "能效核心数",
  "cpu.load1": "平均负载（1 分钟）", "cpu.load5": "平均负载（5 分钟）", "cpu.load15": "平均负载（15 分钟）", "cpu.temperature": "CPU 平均温度",
  "memory.usage": "内存占用率", "memory.total": "内存总量", "memory.used": "已用内存", "memory.app": "App 内存", "memory.wired": "Wired 内存", "memory.compressed": "压缩内存",
  "memory.swap_total": "Swap 总量", "memory.swap_used": "Swap 已用", "memory.pressure": "内存压力",
  "gpu.usage": "GPU 占用率", "gpu.temperature": "GPU 平均温度",
  "drive.model": "磁盘型号", "drive.total": "磁盘总容量", "drive.used": "磁盘已用容量", "drive.read_rate": "磁盘读取速度", "drive.write_rate": "磁盘写入速度", "drive.smart_health": "SMART 健康状态",
  "volume.filesystem": "文件系统", "volume.total": "文件系统总容量", "volume.used": "文件系统已用容量",
  "interface.medium": "网络连接类型", "interface.up": "网卡连接状态", "interface.upload_rate": "网络上传速度", "interface.download_rate": "网络下载速度", "interface.ipv4": "本地 IPv4", "network.public_ipv4": "公网 IPv4",
  "battery.level": "电池电量与充电状态", "battery.charging": "充电状态", "system.uptime": "运行时间", "system.os": "操作系统", "system.product": "硬件产品名称",
};
export function metricGroup(key: string): string {
  const prefix = key.split(".")[0];
  if (prefix === "cpu" || prefix === "core") return "CPU";
  if (prefix === "memory") return "内存（Swap）";
  if (prefix === "gpu") return "GPU";
  if (prefix === "drive" || prefix === "volume") return "磁盘";
  if (prefix === "interface" || prefix === "network") return "网络";
  if (prefix === "battery") return "电池";
  return "系统指标";
}
export const isRequired = (metric: CollectorV2Metric) => metric.required || requiredMetrics.has(metric.metric);
export const metricBundles = [
  { key: "cpu.model", title: "CPU 信息", members: ["cpu.model", "cpu.physical_cores", "cpu.logical_cores", "cpu.performance_cores", "cpu.efficiency_cores"] },
  { key: "cpu.load1", title: "CPU 负载", members: ["cpu.load1", "cpu.load5", "cpu.load15"] },
  { key: "memory.app", title: "内存构成", members: ["memory.app", "memory.wired", "memory.compressed"] },
  { key: "memory.usage", title: "内存", members: ["memory.usage", "memory.used", "memory.total"] },
  { key: "memory.swap_total", title: "Swap", members: ["memory.swap_total", "memory.swap_used"] },
  { key: "drive.read_rate", title: "磁盘读写", members: ["drive.read_rate", "drive.write_rate"] },
  { key: "drive.model", title: "磁盘信息", members: ["drive.model", "drive.total", "drive.used"] },
  { key: "interface.medium", title: "网络连接", members: ["interface.medium", "interface.up"] },
  { key: "interface.upload_rate", title: "网络速度", members: ["interface.upload_rate", "interface.download_rate"] },
  { key: "interface.ipv4", title: "IP 地址", members: ["interface.ipv4", "network.public_ipv4"] },
  { key: "battery.level", title: "电池状态", members: ["battery.level", "battery.charging"], requireAll: true },
  { key: "system.os", title: "设备信息", members: ["system.os", "system.product"] },
];
export type MetricBundle = typeof metricBundles[number];
export type MetricRow = CollectorV2Metric & { members: CollectorV2Metric[]; bundle?: MetricBundle };
export const bundleFor = (key: string) => metricBundles.find(bundle => bundle.members.includes(key));
export const deviceDependencies: Record<string, string> = {
  "interface.upload_rate": "interface.medium", "interface.ipv4": "interface.medium",
  "gpu.temperature": "gpu.usage", "drive.read_rate": "drive.model",
  "drive.smart_health": "drive.model",
};
export function selectedObjects(upload: CollectorUploadConfig, key: string): number[] {
  const members = bundleFor(key)?.members ?? [key];
  return [...new Set(upload.optional.filter(item => members.includes(item.metric)).flatMap(item => item.objects ?? []))];
}
export function pruneDeviceDependents(upload: CollectorUploadConfig, parent: string, id: number) {
  const keys = Object.entries(deviceDependencies).filter(([, value]) => value === parent).flatMap(([key]) => bundleFor(key)?.members ?? [key]);
  for (const selection of upload.optional) if (keys.includes(selection.metric) && selection.objects) selection.objects = selection.objects.filter(object => object !== id);
  upload.optional = upload.optional.filter(selection => !keys.includes(selection.metric) || !selection.objects || selection.objects.length > 0);
  if (parent === "interface.medium" && !selectedObjects(upload, "interface.ipv4").length) upload.optional = upload.optional.filter(selection => selection.metric !== "network.public_ipv4");
}
export function availableMetrics(caps: CollectorV2Capabilities): CollectorV2Metric[] {
  return caps.metrics.filter(metric => !metric.metric.startsWith("volume.") && metric.available && (!metric.objects || metric.objects.length > 0) &&
    (metric.metric !== "core.usage" || (array(caps.inventory.cores).length > 0 && array(caps.inventory.cores).length <= 256 && array(caps.inventory.cores).every(core => metric.objects?.includes(Number(core.id))))));
}
export function displayMetrics(caps: CollectorV2Capabilities): MetricRow[] {
  const available = availableMetrics(caps), rows: MetricRow[] = [];
  for (const bundle of metricBundles) {
    const members = bundle.members.flatMap(key => available.filter(metric => metric.metric === key));
    if (!members.length || (bundle.requireAll && members.length !== bundle.members.length)) continue;
    const objectMembers = members.filter(metric => metric.objects);
    const objects = objectMembers.length ? (bundle.key === "drive.model" ? [...new Set(objectMembers.flatMap(metric => metric.objects!))] : objectMembers[0].objects!.filter(id => objectMembers.every(metric => metric.objects!.includes(id)))) : undefined;
    if (objectMembers.length && !objects?.length) continue;
    rows.push({ ...members[0], metric: bundle.key, objects, required: bundle.key === "memory.usage", fixed: members.every(metric => metric.fixed), members, bundle });
  }
  for (const metric of available) if (!bundleFor(metric.metric)) rows.push({ ...metric, members: [metric] });
  return rows;
}
// Row order mirrors the preview page so the GUI reads identically.
export const displayOrder = ["cpu.usage", "cpu.model", "core.usage", "cpu.load1", "cpu.temperature",
  "memory.usage", "memory.app", "memory.swap_total", "memory.pressure",
  "gpu.usage", "gpu.temperature", "drive.model", "drive.read_rate", "drive.smart_health",
  "interface.medium", "interface.upload_rate", "interface.ipv4", "battery.level", "system.uptime", "system.os"];
const orderIndex = (key: string) => { const index = displayOrder.indexOf(key); return index === -1 ? displayOrder.length : index; };
export function orderedRows(caps: CollectorV2Capabilities): MetricRow[] {
  return displayMetrics(caps).sort((a, b) => orderIndex(a.metric) - orderIndex(b.metric));
}
// Existing drafts are kept when a member becomes unavailable; the user can repair them.
export function bundleUploads(upload: CollectorUploadConfig, caps: CollectorV2Capabilities) {
  upload.optional = upload.optional.filter(selection => selection.metric !== "memory.total" && selection.metric !== "memory.used" && !selection.metric.startsWith("volume."));
  const rows = displayMetrics(caps);
  const core = rows.find(row => row.metric === "core.usage");
  const selectedCore = upload.optional.find(selection => selection.metric === "core.usage");
  if (core?.objects && selectedCore) selectedCore.objects = [...core.objects];
  for (const row of rows) {
    if (!row.bundle || isRequired(row)) continue;
    const selected = upload.optional.filter(selection => row.bundle!.members.includes(selection.metric));
    if (!selected.length) continue;
    const objects = [...new Set(selected.flatMap(selection => selection.objects ?? []))].sort((a, b) => a - b);
    for (const member of row.members) {
      let selection = upload.optional.find(item => item.metric === member.metric);
      if (!selection) { selection = { metric: member.metric }; upload.optional.push(selection); }
      if (member.objects) selection.objects = row.metric === "drive.model" ? objects.filter(id => member.objects!.includes(id)) : [...objects];
      if (member.objects && !selection.objects?.length) upload.optional = upload.optional.filter(item => item !== selection);
    }
  }
}
export function invalidV2Selections(config: CollectorConfig, caps: CollectorV2Capabilities): CollectorUploadSelection[] {
  const available = new Map(availableMetrics(caps).map(metric => [metric.metric, metric]));
  const rows = displayMetrics(caps);
  return (config.upload?.optional ?? []).filter(selection => {
    const metric = available.get(selection.metric);
    const bundle = bundleFor(selection.metric);
    const row = bundle && rows.find(item => item.bundle === bundle);
    const parent = deviceDependencies[bundle?.key ?? selection.metric];
    return !metric || isRequired(metric) || (bundle && !row) || (parent && selection.objects?.some(id => !selectedObjects(config.upload!, parent).includes(id))) ||
      (selection.metric === "network.public_ipv4" && !selectedObjects(config.upload!, "interface.ipv4").length) || (metric.objects && (!selection.objects?.length || selection.objects.some(id => !(row?.objects ?? metric.objects!).includes(id))));
  });
}
export function repairV2Selections(config: CollectorConfig, caps: CollectorV2Capabilities) {
  if (!config.upload) return;
  const available = new Map(availableMetrics(caps).map(metric => [metric.metric, metric]));
  const rows = displayMetrics(caps);
  config.upload.optional = config.upload.optional.flatMap(selection => {
    const metric = available.get(selection.metric); if (!metric || isRequired(metric)) return [];
    const bundle = bundleFor(selection.metric), row = bundle && rows.find(item => item.bundle === bundle);
    if (bundle && !row) return [];
    const next = structuredClone(selection);
    if (metric.objects) { next.objects = (next.objects ?? []).filter(id => (row?.objects ?? metric.objects!).includes(id)); if (!next.objects.length) return []; }
    const parent = deviceDependencies[bundle?.key ?? selection.metric];
    if (parent && next.objects) { next.objects = next.objects.filter(id => selectedObjects(config.upload!, parent).includes(id)); if (!next.objects.length) return []; }
    if (selection.metric === "network.public_ipv4" && !selectedObjects(config.upload!, "interface.ipv4").some(id => selectedObjects(config.upload!, "interface.medium").includes(id))) return [];
    return [next];
  });
  bundleUploads(config.upload, caps);
}
export function validateMetricDraft(config: CollectorConfig): string {
  if (!Number.isInteger(config.interval_seconds) || config.interval_seconds < 5 || config.interval_seconds > 60) return "采集间隔必须为 5–60 秒的整数";
  return "";
}
export function ensureUploadConfig(config: CollectorConfig): CollectorUploadConfig {
  config.upload ??= { enabled: false, interval_seconds: config.interval_seconds, optional: [] };
  return config.upload;
}
export function prepareMetricDraft(config: CollectorConfig, caps?: CollectorCapabilities): CollectorConfig {
  const next = structuredClone(config);
  if (caps?.v2) {
    const upload = ensureUploadConfig(next);
    upload.interval_seconds = next.interval_seconds;
    // V2 upload choices live in upload.optional; keep local previews separate.
    upload.optional = upload.optional.filter(selection => !requiredMetrics.has(selection.metric));
    // The GUI only offers currently available metrics, so stale selections
    // (hardware changed or a sensor backend went away) are pruned here
    // instead of blocking the save with a selection the user cannot see.
    repairV2Selections(next, caps.v2);
  }
  return next;
}
export function record(value: unknown): Record<string, unknown> { return value && typeof value === "object" ? value as Record<string, unknown> : {}; }
function array(value: unknown): Record<string, unknown>[] { return Array.isArray(value) ? value.map(record) : []; }
function formatBytes(value: unknown): string {
  try {
    let n = BigInt(String(value)); const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"]; let unit = 0, divisor = 1n;
    while (n >= divisor * 1024n && unit < units.length - 1) { divisor *= 1024n; unit++; }
    return `${n / divisor}${unit ? `.${String(n % divisor * 100n / divisor).padStart(2, "0")}` : ""} ${units[unit]}`;
  } catch { return "—"; }
}
function formatMemoryPart(value: unknown, total: unknown): string {
  try {
    const used = BigInt(String(value)), capacity = BigInt(String(total));
    if (used < 0n || capacity <= 0n) return "—";
    const hundredths = (used * 10000n + capacity / 2n) / capacity;
    return `${hundredths / 100n}.${String(hundredths % 100n).padStart(2, "0")}%`;
  } catch { return "—"; }
}
function formatValue(key: string, value: unknown): string {
  if (value === undefined || value === null) return "—";
  if (["cpu.usage", "core.usage", "memory.usage", "gpu.usage", "battery.level"].includes(key)) return `${(Number(value) / 100).toFixed(2)}%`;
  if (key.endsWith("temperature")) return `${(Number(value) / 10).toFixed(1)} °C`;
  if (/cpu\.load/.test(key)) return (Number(value) / 1000).toFixed(3);
  if (key.endsWith("_rate")) return `${formatBytes(value)}/s`;
  if (["memory.total", "memory.used", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "drive.total", "drive.used"].includes(key)) return formatBytes(value);
  if (key === "battery.charging") return value ? "充电中" : "未充电";
  if (key === "drive.smart_health") return value ? "健康" : "故障";
  if (key === "interface.up") return value ? "已连接" : "未连接";
  if (key === "interface.ipv4") return Array.isArray(value) ? value.length ? value.join(" / ") : "无地址" : "—";
  if (key === "memory.pressure") return ["正常", "警告", "严重"][Number(value)] ?? "—";
  if (key === "system.uptime") { const n = Number(value); return `${Math.floor(n / 86400)} 天 ${Math.floor(n % 86400 / 3600)} 小时 ${Math.floor(n % 3600 / 60)} 分`; }
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
// 代理上报的对象名可能只是键里的局部名（电池为 "0"、核心为 "cpu0"），
// 这类名称按对象类型补一个可读名称。
export function objectLabel(caps: CollectorV2Capabilities, id: number): string {
  const object = caps.objects?.find(item => item.id === id);
  const name = object?.name?.trim() ?? "";
  const core = object?.kind === "core" ? array(caps.inventory.cores).find(item => Number(item.id) === id) : undefined;
  if (core) {
    if (name && !/^cpu\d+$/i.test(name)) return name;
    const index = Number(core.index);
    return Number.isNaN(index) ? `逻辑核 ${id}` : `逻辑核 ${index}`;
  }
  if (name && !/^\d+$/.test(name)) return name;
  if (object?.kind === "battery") return "系统电池";
  if (object?.kind === "gpu") return "GPU";
  if (object?.kind === "volume") return "卷";
  return name || `对象 ${id}`;
}

export function metricValue(metric: CollectorV2Metric, caps: CollectorV2Capabilities, snapshot?: CollectorSnapshot, objectID?: number): string {
  const key = metric.metric, inventory = caps.inventory, sample = record(snapshot?.v2_sample);
  const [family, field] = key.split(".");
  if (metric.fixed && !metric.objects) {
    if (family === "cpu") return formatValue(key, record(inventory.cpu)[field]);
    if (key === "memory.total") return formatValue(key, inventory.memory_total);
    if (key === "system.product") return formatValue(key, inventory.product);
    if (key === "system.os") { const os = record(inventory.os); return [os.name, os.version].filter(Boolean).join(" ") || "—"; }
  }
  const direct: Record<string, unknown> = { "cpu.usage": sample.c, "memory.usage": sample.m, "system.uptime": sample.u, "network.public_ipv4": record(sample.ip).v };
  if (key in direct) return formatValue(key, direct[key]);
  const fields: Record<string, string> = { "cpu.load1": "l1", "cpu.load5": "l5", "cpu.load15": "l15", "cpu.temperature": "t", "memory.used": "u", "memory.app": "a", "memory.wired": "w", "memory.compressed": "c", "memory.swap_total": "st", "memory.swap_used": "su", "memory.pressure": "p", "core.usage": "u", "gpu.usage": "u", "gpu.temperature": "t", "drive.model": "model", "drive.total": "total", "drive.used": "u", "drive.read_rate": "r", "drive.write_rate": "w", "interface.medium": "medium", "interface.upload_rate": "tx", "interface.download_rate": "rx", "interface.ipv4": "ip", "interface.up": "up", "battery.level": "l", "battery.charging": "c" };
  if (!metric.objects) {
    const value = record(sample[family === "memory" ? "mem" : family])[fields[key]];
    return ["memory.app", "memory.wired", "memory.compressed"].includes(key) ? formatMemoryPart(value, inventory.memory_total) : formatValue(key, value);
  }
  const lists: Record<string, string> = metric.fixed ? { drive: "drives", volume: "volumes", interface: "interfaces" } : { core: "cores", gpu: "gpu", drive: "disk", volume: "vol", interface: "net", battery: "bat" };
  return array((metric.fixed ? inventory : sample)[lists[family]])
    .filter(item => metric.objects!.includes(Number(item[metric.fixed ? "id" : "i"])) && (objectID === undefined || Number(item[metric.fixed ? "id" : "i"]) === objectID))
    .map(item => {
      let value = item[fields[key]];
      if (key === "drive.smart_health") value = record(item.sm).h;
      const id = Number(item[metric.fixed ? "id" : "i"]);
      const name = objectLabel(caps, id);
      const formatted = key === "interface.medium" ? ({ wifi: "Wi-Fi", ethernet: "有线网络", other: "其他" }[String(value)] ?? "—") : formatValue(key, value);
      return objectID === undefined ? `${name}: ${formatted}` : formatted;
    }).join("；") || "—";
}

const memberLabels: Record<string, string> = {
 "cpu.load1":"1 分钟", "cpu.load5":"5 分钟", "cpu.load15":"15 分钟", "memory.app":"App", "memory.wired":"Wired", "memory.compressed":"压缩",
 "memory.swap_total":"总量", "memory.swap_used":"已用", "interface.medium":"类型", "interface.up":"状态", "interface.upload_rate":"上传", "interface.download_rate":"下载",
 "memory.usage":"占用率", "memory.used":"已用", "memory.total":"总量", "drive.model":"型号", "drive.total":"容量", "drive.used":"已用",
 "volume.filesystem":"类型", "volume.total":"总量", "volume.used":"已用", "drive.read_rate":"读取", "drive.write_rate":"写入",
 "interface.ipv4":"本地", "network.public_ipv4":"公网", "battery.level":"电量", "battery.charging":"充电状态",
 "system.os":"系统", "system.product":"设备",
};
// One cell per measurement. Each cell owns its own width, so values wrap
// inside the cell and the layout needs no NBSP bonding or manual line breaks.
export interface MetricCell { key: string; label: string; value: string }

// CPU 信息拆成「型号」与核心/线程数两格。
function cpuInfoCells(row: MetricRow, caps: CollectorV2Capabilities): MetricCell[] {
  const raw = (key: string) => { const metric = row.members.find(member => member.metric === key); return metric ? metricValue(metric, caps) : "—"; };
  const model = raw("cpu.model"), cores = raw("cpu.physical_cores"), threads = raw("cpu.logical_cores");
  const counts = cores !== "—" ? (threads === "—" ? `${cores}C` : `${cores}C/${threads}T`) : threads !== "—" ? `${threads}T` : "";
  const cells: MetricCell[] = [];
  if (model !== "—") cells.push({ key: "cpu.model", label: "型号", value: model });
  if (counts) cells.push({ key: "cpu.cores", label: "核心", value: counts });
  return cells;
}

// 磁盘信息每格一个磁盘：对象名在行首，格内只放「已用 / 总容量」。
function driveCells(row: MetricRow, caps: CollectorV2Capabilities, snapshot?: CollectorSnapshot, objectID?: number): MetricCell[] {
  const totals = new Map(array(caps.inventory.drives).map(drive => [Number(drive.id), drive.total]));
  const used = new Map(array(record(snapshot?.v2_sample).disk).map(disk => [Number(disk.i), disk.u]));
  const ids = objectID === undefined ? row.objects ?? [] : [objectID];
  return ids.flatMap(id => {
    const total = totals.get(id), current = used.get(id);
    let value = "—";
    try {
      const usedBytes = BigInt(String(current)), totalBytes = BigInt(String(total));
      if (usedBytes >= 0n && totalBytes > 0n && usedBytes <= totalBytes) value = `${formatBytes(usedBytes)} / ${formatBytes(totalBytes)}`;
    } catch { /* 缺失或非法容量保持占位符 */ }
    if (objectID !== undefined) {
      return [
        { key: `${id}.used`, label: "已用", value: current === undefined ? "—" : formatBytes(current) },
        { key: `${id}.total`, label: "总量", value: total === undefined ? "—" : formatBytes(total) },
      ];
    }
    const name = objectLabel(caps, id);
    return [{ key: String(id), label: name, value }];
  });
}

export function rowCells(row: MetricRow, caps: CollectorV2Capabilities, snapshot?: CollectorSnapshot, objectID?: number): MetricCell[] {
  if (row.bundle?.key === "cpu.model") return cpuInfoCells(row, caps);
  if (row.bundle?.key === "drive.model") return driveCells(row, caps, snapshot, objectID);
  const cells = !row.bundle
    ? [{ key: row.metric, label: "", value: metricValue(row.members[0], caps, snapshot, objectID) }]
    : row.members
      .filter(metric => metric.metric !== "network.public_ipv4" || objectID === undefined ||
        array(record(snapshot?.v2_sample).net).some(item => Number(item.i) === objectID && Array.isArray(item.ip) && item.ip.some(ip => typeof ip === "string" && ip.trim() !== "")))
      .map(metric => {
        const value = metricValue(metric, caps, snapshot, objectID);
        return { key: metric.metric, label: memberLabels[metric.metric] ?? metric.metric, value: metric.metric === "interface.up" && value === "—" ? "未知" : value };
      })
      .filter(cell => cell.value !== "—");
  return cells.length ? cells : [{ key: row.metric, label: "", value: "—" }];
}

// Flat text form for places that still render one string (overview, arcade).
// The metrics page renders rowCells directly as pixel cards.
export function rowValue(row: MetricRow, caps: CollectorV2Capabilities, snapshot?: CollectorSnapshot, objectID?: number): string {
  return rowCells(row, caps, snapshot, objectID).map(cell => cell.label ? `${cell.label} ${cell.value}` : cell.value).join("\n");
}
