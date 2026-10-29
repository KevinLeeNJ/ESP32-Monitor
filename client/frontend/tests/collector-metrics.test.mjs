import test from "node:test";
import assert from "node:assert/strict";
import { bundleFor, displayMetrics, invalidV2Selections, isRequired, metricGroup, prepareMetricDraft, repairV2Selections, rowValue, validateMetricDraft } from "../dist/services/collector-metrics.js";

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
  assert.equal(rowValue(row, caps), "Apple M4\n10C/10T");
});

test("disk info row renders drive name with used and total capacity", () => {
  const caps = capabilities();
  const gib = 1024 ** 3;
  caps.inventory.drives = [{ id: 7, model: "Disk A", total: String(500 * gib) }];
  caps.metrics.push(metric("drive.total", { objects: [7], fixed: true }), metric("drive.used", { objects: [7] }));
  const row = displayMetrics(caps).find(item => item.metric === "drive.model");
  const snapshot = { v2_sample: { disk: [{ i: 7, u: String(100 * gib) }] } };
  const nb = "\u00A0";
  assert.equal(rowValue(row, caps, snapshot), `disk0: 100.00${nb}GiB${nb}/${nb}500.00${nb}GiB`);
  assert.equal(rowValue(row, caps, snapshot, 7), `100.00${nb}GiB${nb}/${nb}500.00${nb}GiB`);
  assert.equal(rowValue(row, caps), "disk0: —");
});

test("SMART health renders as a single health state", () => {
  const caps = capabilities();
  caps.metrics.push(metric("drive.smart_health", { objects: [7] }));
  const row = displayMetrics(caps).find(item => item.metric === "drive.smart_health");
  const snapshot = { v2_sample: { disk: [{ i: 7, sm: { t: 1, h: true } }] } };
  assert.equal(rowValue(row, caps, snapshot, 7), "健康");
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
  assert.equal(validateMetricDraft(config, { v2: caps }), "");
  const prepared = prepareMetricDraft(structuredClone(config), { v2: caps });
  assert.deepEqual(prepared.upload.optional.map(item => item.metric), ["drive.model", "drive.read_rate"]);
  assert.equal(validateMetricDraft(prepared, { v2: caps }), "");
});
