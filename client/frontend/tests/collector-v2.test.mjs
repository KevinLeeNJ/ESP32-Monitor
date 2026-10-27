import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { mountCollectorPanel } from "../dist/components/collector-panel.js";

const tick = () => new Promise(resolve => setImmediate(resolve));
const metric = (metric, extra = {}) => ({ metric, available: true, required: false, fixed: false, ...extra });
function capabilities() {
  return { schema_version: 1, core_collector: "gopsutil", native_collector: "darwin", os: "darwin", arch: "arm64", observed_at: "", categories: [],
    v2: { inventory: { cpu: { model: "Fixture CPU" }, memory_total: "17179869184", drives: [{ id: 7, model: "Disk A", smart: [{ id: 4, key: "smart_media_errors" }, { id: 5, key: "smart_power_on_hours" }] }] },
      objects: [{ id: 4, kind: "interface", name: "en0" }, { id: 7, kind: "drive", name: "disk0" }],
      metrics: [metric("cpu.usage", { required: true }), metric("memory.usage", { required: true }), metric("system.uptime", { required: true }),
        metric("cpu.model", { fixed: true }), metric("memory.total", { fixed: true }), metric("memory.used"), metric("memory.pressure"),
        metric("interface.medium", { objects: [4], fixed: true }), metric("drive.model", { objects: [7], fixed: true }), metric("interface.download_rate", { objects: [4] }), metric("interface.ipv4", { objects: [4] }),
        metric("drive.smart_attribute", { objects: [7], detail: [4, 5] }),
        metric("gpu.usage", { objects: [8], available: false }), metric("fan.rpm", { available: false }),
        metric("cpu.efficiency_cores", { fixed: true, available: false })] } };
}
function setup(config) {
  const dom = new JSDOM("<main></main>"), root = dom.window.document.querySelector("main");
  let caps = capabilities();
  let snapshot = { schema_version: 1, revision: 0, state: "disabled", pid: 0,
    config: config ?? { interval_seconds: 10, selections: [], upload: { enabled: false, optional: [] } },
    preview: { metrics: [] }, v2_sample: { t: 1793059200, c: 0, m: 6200, u: 0, mem: { u: "0" }, net: [{ i: 4, rx: "0", ip: [] }] },
    last_received_at: null, last_upload_at: null, queue_depth: 0, dropped_metrics: 0 };
  const applied = []; let fail = false;
  const panel = mountCollectorPanel(root, {
    async DiscoverCollectorMetrics() { return structuredClone(caps); }, async GetCollectorCapabilities() { return structuredClone(caps); },
    async GetCollectorSnapshot() { return structuredClone(snapshot); },
    async ApplyCollectorConfig(config) {
      if (fail) throw new Error("config_persist_failed");
      applied.push(config); snapshot = { ...snapshot, revision: snapshot.revision + 1, config }; return structuredClone(snapshot);
    },
  });
  const input = label => root.querySelector(`[aria-label="${label}"]`) ?? (label.startsWith("上传 ") ? root.querySelector(`[data-upload-key="${label.slice(3)}"]`) : null);
  const change = node => node.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  const toggle = label => { const node = input(label); assert.ok(node, label); node.checked = !node.checked; change(node); };
  return { root, panel, applied, input, change, toggle, caps: () => caps, setCaps: value => { caps = value; }, update: sample => { snapshot.v2_sample = sample; }, fail: () => { fail = true; } };
}

test("v2 hides unavailable metrics and groups, and renders required rows without controls", async () => {
  const s = setup(); try {
    await tick();
    for (const key of ["cpu.usage", "memory.usage", "system.uptime"]) {
      const row = s.root.querySelector(`[data-metric="${key}"]`); assert.ok(row);
      assert.match(row.textContent, /必选/); assert.equal(row.querySelectorAll("input, select, button").length, 0);
    }
    for (const key of ["gpu.usage", "fan.rpm", "cpu.efficiency_cores"]) assert.equal(s.root.querySelector(`[data-metric="${key}"]`), null);
    assert.equal(s.root.querySelector('[data-metric-group="GPU"]'), null);
    assert.match(s.root.querySelector('[data-v2-value="cpu.usage"]').textContent, /0\.00%/);
    assert.match(s.root.querySelector('[data-v2-value="cpu.model"]').textContent, /Fixture CPU/);
    assert.match(s.root.querySelector('[data-v2-value="memory.usage"]').textContent, /16\.00 GiB/);
    assert.match(s.root.querySelector('[data-v2-value="interface.ipv4"]').textContent, /无地址/);
  } finally { s.panel.dispose(); }
});
test("v2 sends optional semantic IDs and explicit objects, keeping required metrics implicit", async () => {
  const s = setup(); try {
    await tick(); s.toggle("启用远端指标上报"); s.toggle("上传 cpu.model"); s.toggle("上传 interface.medium 4"); s.toggle("上传 interface.upload_rate 4");
    const interval = s.root.querySelector('input[type="number"]'); interval.value = "5"; s.change(interval);
    await s.panel.controller.apply();
    assert.deepEqual(s.applied[0], { interval_seconds: 5, selections: [], upload: { enabled: true, interval_seconds: 5,
      optional: [{ metric: "cpu.model" }, { metric: "interface.medium", objects: [4] }, { metric: "interface.download_rate", objects: [4] }] } });
  } finally { s.panel.dispose(); }
});
test("SMART uses observed detail IDs and newly discovered objects are never implicitly selected", async () => {
  const s = setup(); try {
    await tick(); s.toggle("启用远端指标上报"); s.toggle("上传 drive.model 7"); s.toggle("上传 drive.smart_attribute 7");
    const details = s.input("SMART 细项"); details.options[0].selected = false; details.options[1].selected = true; s.change(details);
    const caps = s.caps(); caps.v2.objects.push({ id: 9, kind: "drive", name: "disk1" });
    caps.v2.inventory.drives.push({ id: 9, smart: [{ id: 5, key: "smart_power_on_hours" }] });
    caps.v2.metrics.find(metric => metric.metric === "drive.smart_attribute").objects.push(9);
    await s.panel.controller.rescan();
    const selected = s.panel.controller.draft.upload.optional.find(item => item.metric === "drive.smart_attribute"); assert.deepEqual(selected, { metric: "drive.smart_attribute", objects: [7], detail: [5] });
    await s.panel.controller.apply(); assert.deepEqual(s.applied[0].upload.optional, [{ metric: "drive.model", objects: [7] }, selected]);
  } finally { s.panel.dispose(); }
});
test("rescan preserves drafts and offers explicit repair for vanished metrics instead of listing them", async () => {
  const s = setup(); try {
    await tick(); s.toggle("启用远端指标上报"); s.toggle("上传 memory.pressure");
    s.caps().v2.metrics.find(metric => metric.metric === "memory.pressure").available = false;
    await s.panel.controller.rescan();
    assert.equal(s.root.querySelector('[data-metric="memory.pressure"]'), null);
    assert.deepEqual(s.panel.controller.draft.upload.optional, [{ metric: "memory.pressure" }]);
    await s.panel.controller.apply(); assert.equal(s.applied.length, 0); assert.match(s.root.textContent, /先清理/);
    [...s.root.querySelectorAll("button")].find(button => button.textContent === "清理不可用选择").click();
    await s.panel.controller.apply(); assert.deepEqual(s.applied[0].upload.optional, []);
  } finally { s.panel.dispose(); }
});
test("polls update values without replacing forms and failed application retains optional edits", async () => {
  const s = setup(); try {
    await tick(); s.toggle("上传 memory.pressure"); await s.panel.controller.refresh();
    const form = s.root.querySelector('input[type="number"]'); form.value = "6"; s.change(form);
    s.update({ t: 1793059210, c: 1250, m: 0, u: 86400, mem: { u: "0" } }); await s.panel.controller.refresh();
    assert.equal(s.root.querySelector('input[type="number"]'), form);
    assert.match(s.root.querySelector('[data-v2-value="cpu.usage"]').textContent, /12\.50%/);
    assert.match(s.root.querySelector('[data-v2-value="memory.usage"]').textContent, /0 B/);
    s.fail(); await s.panel.controller.apply();
    assert.equal(s.panel.controller.draft.interval_seconds, 6);
    assert.deepEqual(s.panel.controller.draft.upload.optional, [{ metric: "memory.pressure" }]);
    assert.match(s.root.textContent, /config_persist_failed/);
  } finally { s.panel.dispose(); }
});

test("each battery switch submits level and charging for the same object", async () => {
  const s = setup(); try {
    await tick();
    const caps = s.caps(); caps.v2.objects.push({ id: 11, kind: "battery", name: "内置电池" }, { id: 12, kind: "battery", name: "扩展电池" });
    caps.v2.metrics.push(metric("battery.level", { objects: [11, 12] }), metric("battery.charging", { objects: [11, 12] }));
    await s.panel.controller.rescan();
    assert.equal(s.root.querySelector('[data-metric="battery.charging"]'), null);
    assert.match(s.root.querySelector('[data-metric="battery.level"]').textContent, /电池状态/);
    s.toggle("启用远端指标上报"); s.toggle("上传 battery.level 12");
    await s.panel.controller.apply();
    assert.deepEqual(s.applied[0].upload.optional, [{ metric: "battery.level", objects: [12] }, { metric: "battery.charging", objects: [12] }]);
    s.toggle("上传 battery.level 12"); await s.panel.controller.apply(); assert.deepEqual(s.applied[1].upload.optional, []);
  } finally { s.panel.dispose(); }
});
test("a battery without both capabilities is hidden and cleaning removes both upload choices", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps(); caps.v2.objects.push({ id: 11, kind: "battery", name: "内置电池" });
    caps.v2.metrics.push(metric("battery.level", { objects: [11] }), metric("battery.charging", { objects: [11] }));
    await s.panel.controller.rescan(); s.toggle("上传 battery.level 11");
    caps.v2.metrics.find(metric => metric.metric === "battery.charging").available = false;
    await s.panel.controller.rescan();
    assert.equal(s.root.querySelector('[data-metric-group="电池"]'), null);
    [...s.root.querySelectorAll("button")].find(button => button.textContent === "清理不可用选择").click();
    assert.deepEqual(s.panel.controller.draft.upload.optional, []);
  } finally { s.panel.dispose(); }
});

test("bundled metrics show one short title and submit all available members together", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps(), items = caps.v2.metrics;
    caps.v2.objects.push({ id: 13, kind: "volume", name: "/" });
    caps.v2.inventory.volumes = [{ id: 13, filesystem: "APFS", total: "1000" }];
    caps.v2.inventory.os = { family: "darwin", name: "macOS", version: "27.2" }; caps.v2.inventory.product = "Fixture Host";
    for (const [id, extra] of [
      ["cpu.physical_cores", { fixed: true }], ["cpu.logical_cores", { fixed: true }], ["cpu.performance_cores", { fixed: true }],
      ["cpu.load1", {}], ["cpu.load5", {}], ["cpu.load15", {}],
      ["memory.app", {}], ["memory.wired", {}], ["memory.compressed", {}], ["memory.swap_total", {}], ["memory.swap_used", {}],
      ["volume.filesystem", { objects: [13], fixed: true }], ["volume.total", { objects: [13], fixed: true }], ["volume.used", { objects: [13] }],
      ["drive.read_rate", { objects: [7] }], ["drive.write_rate", { objects: [7] }],
      ["interface.up", { objects: [4] }], ["interface.upload_rate", { objects: [4] }],
      ["network.public_ipv4", { reason: "not_probed_until_selected" }], ["system.os", { fixed: true }], ["system.product", { fixed: true }],
    ]) items.push(metric(id, extra));
    await s.panel.controller.rescan(); s.toggle("启用远端指标上报"); s.toggle("上传 drive.model 7");
    const groups = [
      ["cpu.model", "CPU 信息", ["cpu.model", "cpu.physical_cores", "cpu.logical_cores", "cpu.performance_cores"]],
      ["cpu.load1", "CPU 负载", ["cpu.load1", "cpu.load5", "cpu.load15"]],
      ["memory.app", "内存构成", ["memory.app", "memory.wired", "memory.compressed"]],
      ["memory.swap_total", "Swap", ["memory.swap_total", "memory.swap_used"]],
      ["drive.read_rate", "磁盘读写", ["drive.read_rate", "drive.write_rate"]],
      ["interface.medium", "网络连接", ["interface.medium", "interface.up"]],
      ["interface.upload_rate", "网络速度", ["interface.upload_rate", "interface.download_rate"]],
      ["interface.ipv4", "IP 地址", ["interface.ipv4", "network.public_ipv4"]],
      ["system.os", "设备信息", ["system.os", "system.product"]],
    ];
    for (const [key, label, members] of groups) {
      const row = s.root.querySelector(`[data-metric="${key}"]`); assert.ok(row, key);
      assert.equal(row.querySelector("strong").textContent, label);
      assert.equal(row.querySelector("small"), null);
      assert.equal(row.querySelectorAll('input[type="checkbox"]').length, 1);
      for (const member of members.slice(1)) assert.equal(s.root.querySelector(`[data-metric="${member}"]`), null, member);
      const objectID = key.startsWith("interface.") ? 4 : key === "volume.filesystem" ? 13 : key === "drive.read_rate" ? 7 : undefined;
      s.toggle(`上传 ${key}${objectID === undefined ? "" : ` ${objectID}`}`);
    }
    assert.match(s.root.textContent, /外部服务查询/);
    await s.panel.controller.apply();
    const entries = new Map(s.applied[0].upload.optional.map(entry => [entry.metric, entry]));
    for (const [, , members] of groups) for (const member of members) assert.ok(entries.has(member), member);
    for (const member of ["interface.medium", "interface.up", "interface.upload_rate", "interface.download_rate", "interface.ipv4"]) assert.deepEqual(entries.get(member).objects, [4]);
    for (const member of ["drive.read_rate", "drive.write_rate"]) assert.deepEqual(entries.get(member).objects, [7]);
    assert.equal(entries.get("network.public_ipv4").objects, undefined);
    assert.equal(entries.has("cpu.efficiency_cores"), false);
    s.toggle("上传 interface.upload_rate 4"); await s.panel.controller.apply();
    assert.equal(s.applied[1].upload.optional.some(entry => ["interface.upload_rate", "interface.download_rate"].includes(entry.metric)), false);
  } finally { s.panel.dispose(); }
});
test("a grouped row contains only fields that the Agent currently provides", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps();
    caps.v2.metrics.find(metric => metric.metric === "cpu.model").available = false;
    caps.v2.metrics.push(metric("cpu.logical_cores", { fixed: true })); caps.v2.inventory.cpu.logical_cores = 8;
    caps.v2.metrics.push(metric("cpu.load5"), metric("cpu.load15", { available: false }));
    await s.panel.controller.rescan();
    const cpu = s.root.querySelector('[data-metric="cpu.model"]'); assert.match(cpu.textContent, /CPU 信息/); assert.match(cpu.textContent, /8/); assert.doesNotMatch(cpu.textContent, /Fixture CPU/);
    s.toggle("启用远端指标上报"); s.toggle("上传 cpu.model"); s.toggle("上传 cpu.load1"); await s.panel.controller.apply();
    assert.deepEqual(s.applied[0].upload.optional, [{ metric: "cpu.logical_cores" }, { metric: "cpu.load5" }]);
  } finally { s.panel.dispose(); }
});

test("memory composition displays ratios derived from the actual cached total", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps();
    caps.v2.metrics.push(metric("memory.app"), metric("memory.wired"), metric("memory.compressed"));
    await s.panel.controller.rescan();
    s.update({ t: 1793059210, c: 1200, m: 5000, u: 10, mem: { a: "4294967296", w: "2147483648", c: "1073741824" } });
    await s.panel.controller.refresh();
    const value = s.root.querySelector('[data-v2-value="memory.app"]').textContent;
    for (const text of ["4.00 GiB（25.00%）", "2.00 GiB（12.50%）", "1.00 GiB（6.25%）"]) assert.ok(value.includes(text), value);
  } finally { s.panel.dispose(); }
});

test("GPU devices keep independent values and switches", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps();
    caps.v2.objects.push({ id: 20, kind: "gpu", name: "GPU A" }, { id: 21, kind: "gpu", name: "GPU B" });
    const gpu = caps.v2.metrics.find(metric => metric.metric === "gpu.usage"); gpu.available = true; gpu.objects = [20, 21];
    s.update({ t: 1793059210, c: 1250, m: 4000, u: 5, gpu: [{ i: 20, u: 1000 }, { i: 21, u: 3000 }] });
    await s.panel.controller.rescan(); await s.panel.controller.refresh();
    const row = s.root.querySelector('[data-metric="gpu.usage"]');
    assert.match(row.querySelector('[data-object="20"] .metric-value').textContent, /10\.00%/);
    assert.match(row.querySelector('[data-object="21"] .metric-value').textContent, /30\.00%/);
    s.toggle("启用远端指标上报"); s.toggle("上传 gpu.usage 20");
    assert.equal(s.input("上传 gpu.usage 21").checked, false);
    await s.panel.controller.apply(); assert.deepEqual(s.applied[0].upload.optional, [{ metric: "gpu.usage", objects: [20] }]);
  } finally { s.panel.dispose(); }
});

test("IP bundle retains the shared public address only while an interface is selected", async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps(); caps.v2.objects.push({ id: 5, kind: "interface", name: "en1" });
    caps.v2.metrics.find(metric => metric.metric === "interface.ipv4").objects.push(5);
    caps.v2.metrics.find(metric => metric.metric === "interface.medium").objects.push(5);
    caps.v2.metrics.push(metric("network.public_ipv4", { reason: "not_probed_until_selected" }));
    await s.panel.controller.rescan(); s.toggle("启用远端指标上报");
    s.toggle("上传 interface.medium 4"); s.toggle("上传 interface.medium 5"); s.toggle("上传 interface.ipv4 4"); s.toggle("上传 interface.ipv4 5"); s.toggle("上传 interface.ipv4 4");
    await s.panel.controller.apply();
    assert.deepEqual(s.applied[0].upload.optional, [{ metric: "interface.medium", objects: [4, 5] }, { metric: "interface.ipv4", objects: [5] }, { metric: "network.public_ipv4" }]);
    s.toggle("上传 interface.ipv4 5"); await s.panel.controller.apply();
    assert.deepEqual(s.applied[1].upload.optional, [{ metric: "interface.medium", objects: [4, 5] }]);
  } finally { s.panel.dispose(); }
});

test("per-core CPU is controlled by one switch for all cores and never a subset", async () => {
  const config = { interval_seconds: 10, selections: [], upload: { enabled: true, optional: [{ metric: "core.usage", objects: [1] }] } };
  const s = setup(config); try {
    await tick(); const caps = s.caps();
    caps.v2.inventory.cores = [{ id: 1, index: 0 }, { id: 2, index: 1 }];
    caps.v2.objects.push({ id: 1, kind: "core", name: "cpu0" }, { id: 2, kind: "core", name: "cpu1" });
    caps.v2.metrics.push(metric("core.usage", { objects: [1, 2] }));
    s.update({ t: 1793059210, c: 1200, m: 5000, u: 10, cores: [{ i: 1, u: 1250 }, { i: 2, u: 2500 }] });
    await s.panel.controller.rescan(); await s.panel.controller.refresh();
    const row = s.root.querySelector('[data-metric="core.usage"]');
    assert.equal(row.querySelectorAll('input[type="checkbox"]').length, 1);
    assert.equal(row.querySelectorAll('.metric-object input').length, 1);
    assert.equal(row.textContent.includes('全部核心'), false);
    assert.match(row.querySelector('[data-v2-value="core.usage"][data-object="1"]').textContent, /12\.50%/);
    assert.match(row.querySelector('[data-v2-value="core.usage"][data-object="2"]').textContent, /25\.00%/);
    await s.panel.controller.apply(); assert.deepEqual(s.applied[0].upload.optional, [{ metric: "core.usage", objects: [1, 2] }]);
    s.toggle("上传 core.usage"); await s.panel.controller.apply(); assert.deepEqual(s.applied[1].upload.optional, []);
    s.toggle("上传 core.usage"); await s.panel.controller.apply(); assert.deepEqual(s.applied[2].upload.optional, [{ metric: "core.usage", objects: [1, 2] }]);
    caps.v2.metrics.find(metric => metric.metric === "core.usage").objects = [1];
    await s.panel.controller.rescan(); assert.equal(s.root.querySelector('[data-metric="core.usage"]'), null);
  } finally { s.panel.dispose(); }
});

test('turning off one network device clears its dependent metrics only', async () => {
  const s = setup(); try {
    await tick(); const caps = s.caps();
    caps.v2.objects.push({ id: 5, kind: 'interface', name: 'en1' });
    for (const key of ['interface.medium', 'interface.ipv4', 'interface.download_rate']) caps.v2.metrics.find(item => item.metric === key).objects.push(5);
    caps.v2.metrics.push(metric('network.public_ipv4'));
    await s.panel.controller.rescan(); s.toggle('启用远端指标上报');
    assert.equal(s.input('上传 interface.ipv4 4').disabled, true);
    for (const id of [4, 5]) { s.toggle(`上传 interface.medium ${id}`); s.toggle(`上传 interface.ipv4 ${id}`); s.toggle(`上传 interface.upload_rate ${id}`); }
    s.toggle('上传 interface.medium 4');
    assert.equal(s.input('上传 interface.ipv4 4').disabled, true);
    assert.equal(s.input('上传 interface.ipv4 4').checked, false);
    assert.equal(s.input('上传 interface.ipv4 5').checked, true);
    await s.panel.controller.apply();
    const entries = new Map(s.applied[0].upload.optional.map(item => [item.metric, item]));
    for (const key of ['interface.medium', 'interface.ipv4', 'interface.download_rate']) assert.deepEqual(entries.get(key).objects, [5]);
    assert.ok(entries.has('network.public_ipv4'));
    s.toggle('上传 interface.medium 5'); await s.panel.controller.apply();
    assert.deepEqual(s.applied[1].upload.optional, []);
  } finally { s.panel.dispose(); }
});

test('memory is one required row and disk information bundles available fields per device', async () => {
  const s = setup({ interval_seconds: 10, selections: [], upload: { enabled: true, optional: [{ metric: 'memory.used' }, { metric: 'memory.total' }] } });
  try {
    await tick(); const caps = s.caps();
    caps.v2.objects.push({ id: 9, kind: 'drive', name: 'disk1' });
    caps.v2.inventory.drives.push({ id: 9, total: '1000' });
    caps.v2.metrics.push(metric('drive.total', { fixed: true, objects: [7, 9] }), metric('drive.used', { objects: [7] }));
    s.update({ t:1793059210, c:0, m:6200, u:0, mem:{ u:'0' }, disk:[{i:7,u:'1234'}] }); await s.panel.controller.refresh();
    caps.v2.inventory.drives[0].total = '2000';
    await s.panel.controller.rescan();
    const memory = s.root.querySelector('[data-metric="memory.usage"]');
    assert.match(memory.textContent, /16\.00 GiB/); assert.match(memory.textContent, /0 B/);
    assert.equal(memory.querySelector('input'), null); assert.equal(s.root.querySelector('[data-metric="memory.total"]'), null);
    const disk = s.root.querySelector('[data-metric="drive.model"]');
    assert.equal(disk.querySelectorAll('.metric-object').length, 2); assert.match(disk.querySelector('[data-object="7"] .metric-value').textContent, /1\.20 KiB/);
    s.toggle('上传 drive.model 7'); s.toggle('上传 drive.model 9');
    await s.panel.controller.apply();
    const entries = new Map(s.applied[0].upload.optional.map(item => [item.metric, item]));
    assert.equal(entries.has('memory.total'), false); assert.equal(entries.has('memory.used'), false);
    assert.deepEqual(entries.get('drive.model').objects, [7]); assert.deepEqual(entries.get('drive.total').objects, [7, 9]); assert.deepEqual(entries.get('drive.used').objects, [7]);
    s.toggle('上传 drive.model 7'); await s.panel.controller.apply();
    assert.deepEqual(s.applied[1].upload.optional, [{ metric: 'drive.total', objects: [9] }]);
  } finally { s.panel.dispose(); }
});
