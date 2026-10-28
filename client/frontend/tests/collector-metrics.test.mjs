import test from "node:test";
import assert from "node:assert/strict";
import { bundleFor, displayMetrics, invalidV2Selections, isRequired, metricGroup, rowValue } from "../dist/services/collector-metrics.js";

const metric = (name, extra = {}) => ({ metric: name, available: true, required: false, fixed: false, ...extra });

function capabilities() {
  return {
    inventory: {
      cpu: { model: "Apple M4", physical_cores: 10, logical_cores: 10 },
      memory_total: "17179869184",
      drives: [{ id: 7, model: "Disk A" }],
      fans: [{ id: 1, scope: "cpu" }],
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
      metric("fan.rpm", { objects: [1] }),
    ],
    objects: [{ id: 7, kind: "drive", name: "disk0" }, { id: 1, kind: "fan", name: "Fan" }],
  };
}

test("metric groups, bundles and required flags follow the collector contract", () => {
  const caps = capabilities();
  assert.equal(metricGroup("drive.smart_attribute"), "磁盘");
  assert.equal(metricGroup("system.uptime"), "系统指标");
  assert.equal(metricGroup("fan.rpm", caps), "CPU");
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

test("selections of unavailable metrics are reported as invalid", () => {
  const config = { interval_seconds: 10, selections: [], upload: { enabled: true, interval_seconds: 10, optional: [{ metric: "gpu.usage" }] } };
  assert.deepEqual(invalidV2Selections(config, capabilities()).map(item => item.metric), ["gpu.usage"]);
});
