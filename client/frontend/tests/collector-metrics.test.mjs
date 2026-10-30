import test from "node:test";
import assert from "node:assert/strict";
import { bundleFor, displayMetrics, invalidV2Selections, isRequired, metricGroup, objectLabel, prepareMetricDraft, repairV2Selections, rowCells, rowValue, validateMetricDraft } from "../dist/services/collector-metrics.js";

const metric = (name, extra = {}) => ({ metric: name, available: true, required: false, fixed: false, ...extra });

function capabilities() {
  return {
    inventory: {
      cpu: { model: "Apple M4", physical_cores: 10, logical_cores: 10 },
      memory_total: "17179869184",
      drives: [{ id: 7, model: "Disk A" }],
    },
    metrics: [
      metric("cpu.usage", { required: true }),
      metric("cpu.model", { fixed: true }),
      metric("cpu.physical_cores", { fixed: true }),
      metric("cpu.logical_cores", { fixed: true }),
      metric("memory.usage", { required: true }),
      metric("memory.total", { fixed: true }),
      metric("memory.used"),
      metric("system.uptime", { required: true }),
      metric("drive.model", { objects: [7], fixed: true }),
      metric("drive.read_rate", { objects: [7] }),
      metric("gpu.usage", { objects: [8], available: false }),
    ],
    objects: [{ id: 7, kind: "drive", name: "disk0" }],
  };
}

test("metric groups, bundles and required flags follow the collector contract", () => {
  const caps = capabilities();
  assert.equal(metricGroup("drive.smart_health"), "磁盘");
  assert.equal(metricGroup("system.uptime"), "系统指标");
  assert.equal(bundleFor("cpu.load5").title, "CPU 负载");
  assert.equal(isRequired({ metric: "cpu.usage" }), true);
  assert.equal(isRequired({ metric: "memory.app" }), false);
});

test("display rows bundle members and skip unavailable metrics", () => {
  const rows = displayMetrics(capabilities());
  assert.ok(rows.some(row => row.metric === "cpu.model" && row.members.length === 3));
  assert.ok(rows.some(row => row.metric === "memory.usage" && row.required));
  assert.ok(!rows.some(row => row.metric === "gpu.usage"));
});

test("CPU info row renders model and core counts", () => {
  const caps = capabilities();
  const row = displayMetrics(caps).find(item => item.metric === "cpu.model");
  assert.equal(rowValue(row, caps), "型号 Apple M4\n核心 10C/10T");
});

test("each measurement becomes its own card cell", () => {
  const caps = capabilities();
  caps.metrics.push(metric("cpu.load1"), metric("cpu.load5"), metric("cpu.load15"));
  const load = displayMetrics(caps).find(item => item.metric === "cpu.load1");
  const cells = rowCells(load, caps, { v2_sample: { cpu: { l1: 1250, l5: 2000, l15: 500 } } });
  assert.deepEqual(cells.map(cell => `${cell.label} ${cell.value}`), ["1 分钟 1.250", "5 分钟 2.000", "15 分钟 0.500"]);
  const info = displayMetrics(caps).find(item => item.metric === "cpu.model");
  assert.deepEqual(rowCells(info, caps).map(cell => cell.label), ["型号", "核心"]);
});

test("disk info row renders drive name with used and total capacity", () => {
  const caps = capabilities();
  const gib = 1024 ** 3;
  caps.inventory.drives = [{ id: 7, model: "Disk A", total: String(500 * gib) }];
  caps.metrics.push(metric("drive.total", { objects: [7], fixed: true }), metric("drive.used", { objects: [7] }));
  const row = displayMetrics(caps).find(item => item.metric === "drive.model");
  const snapshot = { v2_sample: { disk: [{ i: 7, u: String(100 * gib) }] } };
  assert.equal(rowValue(row, caps, snapshot), "disk0 100.00 GiB / 500.00 GiB");
  assert.equal(rowValue(row, caps, snapshot, 7), "已用 100.00 GiB\n总量 500.00 GiB");
  assert.deepEqual(rowCells(row, caps, snapshot, 7).map(cell => `${cell.label} ${cell.value}`), ["已用 100.00 GiB", "总量 500.00 GiB"]);
  assert.equal(rowValue(row, caps), "disk0 —");
});

test("SMART health renders as a single health state", () => {
  const caps = capabilities();
  caps.metrics.push(metric("drive.smart_health", { objects: [7] }));
  const row = displayMetrics(caps).find(item => item.metric === "drive.smart_health");
  const snapshot = { v2_sample: { disk: [{ i: 7, sm: { t: 1, h: true } }] } };
  assert.equal(rowValue(row, caps, snapshot, 7), "健康");
});

test("bundled values keep plain spacing for cell-level wrapping", () => {
  const caps = capabilities();
  caps.inventory.os = { name: "Windows 11 Pro", version: "26H2" };
  caps.inventory.product = "ASUSTeK COMPUTER INC. ASUS TUF Gaming A15 FA507XV_FA507XV";
  caps.metrics.push(metric("system.os", { fixed: true }), metric("system.product", { fixed: true }));
  const row = displayMetrics(caps).find(item => item.metric === "system.os");
  assert.ok(row);
  // Cells own their width now, so labels and values keep plain spaces and the
  // renderer wraps inside each card.
  assert.equal(rowValue(row, caps),
    "系统 Windows 11 Pro 26H2\n设备 ASUSTeK COMPUTER INC. ASUS TUF Gaming A15 FA507XV_FA507XV");
  const uptime = { metric: "system.uptime", members: [{ metric: "system.uptime" }] };
  assert.equal(rowValue(uptime, caps, { v2_sample: { u: 90061 } }), "1 天 1 小时 1 分");
});

test("selections of unavailable metrics are reported as invalid", () => {
  const config = { interval_seconds: 10, selections: [], upload: { enabled: true, interval_seconds: 10, optional: [{ metric: "gpu.usage" }] } };
  assert.deepEqual(invalidV2Selections(config, capabilities()).map(item => item.metric), ["gpu.usage"]);
});

test("stale selections are pruned silently instead of blocking the save", () => {
  const caps = capabilities();
  const config = { interval_seconds: 10, upload: { enabled: true, interval_seconds: 10, optional: [{ metric: "gpu.usage" }, { metric: "drive.model", objects: [7] }, { metric: "drive.read_rate", objects: [7] }] } };
  repairV2Selections(config, caps);
  assert.deepEqual(config.upload.optional.map(item => item.metric), ["drive.model", "drive.read_rate"]);
  assert.deepEqual(config.upload.optional[0].objects, [7]);
  assert.equal(validateMetricDraft(config), "");
  const prepared = prepareMetricDraft(structuredClone(config), { v2: caps });
  assert.deepEqual(prepared.upload.optional.map(item => item.metric), ["drive.model", "drive.read_rate"]);
  assert.equal(validateMetricDraft(prepared), "");
});

test("battery devices render one row each with level and charging cells", () => {
  const caps = capabilities();
  caps.metrics.push(metric("battery.level", { objects: [3] }), metric("battery.charging", { objects: [3] }));
  caps.objects.push({ id: 3, kind: "battery", name: "系统电池" });
  const row = displayMetrics(caps).find(item => item.metric === "battery.level");
  assert.deepEqual(row.objects, [3]);
  const cells = rowCells(row, caps, { v2_sample: { bat: [{ i: 3, l: 10000, c: 0 }] } }, 3);
  assert.deepEqual(cells.map(cell => `${cell.label} ${cell.value}`), ["电量 100.00%", "充电状态 未充电"]);
});

test("raw key locals fall back to readable object names", () => {
  const caps = capabilities();
  caps.inventory.cores = [{ id: 11, index: 1 }, { id: 12, index: 2 }];
  caps.objects.push({ id: 3, kind: "battery", name: "0" }, { id: 11, kind: "core", name: "cpu1" }, { id: 9, kind: "gpu", name: "GPU" });
  assert.equal(objectLabel(caps, 3), "系统电池");
  assert.equal(objectLabel(caps, 11), "逻辑核 1");
  assert.equal(objectLabel(caps, 9), "GPU");
  assert.equal(objectLabel(caps, 7), "disk0");
  assert.equal(objectLabel(caps, 42), "对象 42");
});
