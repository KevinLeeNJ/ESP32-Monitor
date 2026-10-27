import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { mountCollectorPanel } from "../dist/components/collector-panel.js";

const tick = () => new Promise(resolve => setImmediate(resolve));
const metric = (id, upload = true) => ({ id, measurement: id.split("/")[1], field: id.split("/")[2], unit: "bytes", upload_supported: upload, objects: [{ interface: "en0" }, { interface: "lo0" }] });
const capabilities = { schema_version: 1, core_collector: "gopsutil", native_collector: "darwin-stub", os: "darwin", arch: "arm64", observed_at: "", categories: [{ id: "memory", name: "内存", sources: [{ id: "mem", collector: "core", state: "available", metrics: [metric("mem/mem/used")] }, { id: "net", collector: "core", state: "available", metrics: [metric("net/net/bytes_recv", false)] }, { id: "platform_sensors", collector: "native", state: "unsupported", reason: "native_collector_stub", metrics: [] }] }] };
function setup() {
  const dom = new JSDOM('<main></main>');
  const root = dom.window.document.querySelector("main");
  let snapshot = { schema_version: 1, revision: 0, state: "disabled", pid: 0, config: { interval_seconds: 10, selections: [] }, preview: { metrics: [] }, last_received_at: null, last_upload_at: null, queue_depth: 0, dropped_metrics: 0 };
  const applied = [];
  let fail = false;
  const panel = mountCollectorPanel(root, {
    async DiscoverCollectorMetrics() { return structuredClone(capabilities); },
    async GetCollectorCapabilities() { return structuredClone(capabilities); },
    async GetCollectorSnapshot() { return structuredClone(snapshot); },
    async ApplyCollectorConfig(config) {
      if (fail) throw new Error("config_persist_failed");
      applied.push(config);
      snapshot = { ...snapshot, revision: snapshot.revision + 1, state: config.selections.length ? "healthy" : "disabled", config };
      return structuredClone(snapshot);
    }
  });
  const input = label => root.querySelector(`[aria-label="${label}"]`);
  const change = node => node.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  return { root, panel, applied, input, change, fail: () => { fail = true; } };
}
test("frontend edits fields, objects, local-only and interval; applies through Agent bridge", async () => {
  const s = setup();
  try {
    await tick();
    const check = label => { const node = s.input(label); node.checked = true; s.change(node); };
    check("采集 mem/mem/used"); check("上传 mem/mem/used");
    check("采集 net/net/bytes_recv");
    assert.equal(s.input("上传 net/net/bytes_recv").disabled, true);
    const objects = s.input("对象 mem/mem/used"); objects.options[0].selected = true; s.change(objects);
    const interval = s.root.querySelector('input[type="number"]'); interval.value = "5"; s.change(interval);
    [...s.root.querySelectorAll("button")].find(node => node.textContent === "立即应用").click();
    await tick();
    assert.deepEqual(s.applied[0], { interval_seconds: 5, selections: [{ id: "mem/mem/used", upload: true, objects: [{ interface: "en0" }] }, { id: "net/net/bytes_recv", upload: false }] });
    assert.doesNotMatch(s.root.textContent, /native_collector_stub/);
    assert.equal(s.panel.controller.snapshot.revision, 1);
  } finally { s.panel.dispose(); }
});
test("failed apply and polling preserve unsaved edits; polling preserves active form nodes", async () => {
  const s = setup();
  try {
    await tick();
    const local = s.input("采集 mem/mem/used"); local.checked = true; s.change(local);
    const interval = s.root.querySelector('input[type="number"]'); interval.value = "6"; s.change(interval);
    await s.panel.controller.refresh();
    const focused = s.root.querySelector('input[type="number"]');
    await s.panel.controller.refresh();
    assert.equal(focused, s.root.querySelector('input[type="number"]'));
    s.fail(); await s.panel.controller.apply();
    assert.equal(s.panel.controller.snapshot.revision, 0);
    assert.equal(s.panel.controller.draft.interval_seconds, 6);
    assert.equal(s.panel.controller.draft.selections.length, 1);
    assert.match(s.root.textContent, /config_persist_failed/);
  } finally { s.panel.dispose(); }
});
