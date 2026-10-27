import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const script = await readFile(new URL("../preview/main.js", import.meta.url), "utf8");
function setup() {
  const dom = new JSDOM('<div id="app"></div>', { runScripts: "outside-only", pretendToBeVisual: true });
  dom.window.eval(script);
  const doc = dom.window.document;
  doc.querySelector('[data-page="metrics"]').click();
  const change = node => node.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  const tab = name => doc.querySelector(`[data-tab="${name}"]`).click();
  return { dom, doc, change, tab };
}
test("preview follows current Agent metric scope and mandatory rows have no switches", () => {
  const s = setup(); try {
    for (const [tab, id] of [["CPU", "cpu.usage"], ["内存（Swap）", "memory.usage"], ["系统指标", "system.uptime"]]) {
      s.tab(tab); const row = s.doc.querySelector(`[data-metric-row="${id}"]`);
      assert.ok(row); assert.match(row.textContent, /必选/); assert.equal(row.querySelector("input"), null);
    }
    for (const tab of ["CPU", "内存（Swap）", "GPU", "磁盘", "网络", "电池", "系统指标"]) {
      s.tab(tab);
      assert.doesNotMatch(s.doc.querySelector(".metric-content").textContent, /进程数|用户态|系统态|显存|丢包|不可用|Server 暂不支持/);
    }
    assert.equal(s.doc.querySelector('[data-tab="硬件与传感器"]'), null);
    assert.equal(s.doc.querySelector('[data-metric-row="fan.rpm"]'), null);
  } finally { s.dom.window.close(); }
});
test("preview retains selections and interval across tabs and capability changes", () => {
  const s = setup(); try {
    s.tab("网络"); let optional = s.doc.querySelector('[data-metric="interface.upload_rate"]'); optional.checked = true; s.change(optional);
    const interval = s.doc.querySelector('[data-metric-interval]'); interval.value = "30"; s.change(interval);
    s.tab("GPU"); const gpu = s.doc.querySelector('[data-preview="gpu"]'); gpu.value = "unavailable"; s.change(gpu);
    assert.equal(s.doc.querySelector('[data-tab="GPU"]'), null);
    s.tab("网络"); assert.equal(s.doc.querySelector('[data-metric="interface.upload_rate"]').checked, true);
    assert.equal(s.doc.querySelector('[data-metric-interval]').value, "30");
  } finally { s.dom.window.close(); }
});

test("preview battery level and charging are shown as one upload option", () => {
  const s = setup(); try {
    s.tab("电池");
    assert.equal(s.doc.querySelectorAll(".metric-content input[type=checkbox]").length, 1);
    assert.match(s.doc.querySelector(".metric-content").textContent, /电池状态/);
    assert.match(s.doc.querySelector(".metric-content").textContent, /85% · 未充电/);
    assert.equal(s.doc.querySelector('[data-metric-row="battery.charging"]'), null);
  } finally { s.dom.window.close(); }
});

test("preview gives each bundled set a single concise title and switch", () => {
  const s = setup(); try {
    const groups = [["CPU", "cpu.model", "CPU 信息", ["cpu.physical_cores", "cpu.logical_cores"]], ["CPU", "cpu.load1", "CPU 负载", ["cpu.load5", "cpu.load15"]],
      ["内存（Swap）", "memory.app", "内存构成", ["memory.wired", "memory.compressed"]], ["内存（Swap）", "memory.swap_total", "Swap", ["memory.swap_used"]],
      ["磁盘", "drive.read_rate", "磁盘读写", ["drive.write_rate"]],
      ["网络", "interface.medium", "网络连接", ["interface.up"]], ["网络", "interface.upload_rate", "网络速度", ["interface.download_rate"]],
      ["网络", "interface.ipv4", "IP 地址", ["network.public_ipv4"]], ["电池", "battery.level", "电池状态", ["battery.charging"]], ["系统指标", "system.os", "设备信息", ["system.product"]]];
    for (const [tab, key, label, hidden] of groups) {
      s.tab(tab); const row = s.doc.querySelector(`[data-metric-row="${key}"]`); assert.ok(row); assert.equal(row.querySelector("strong").textContent, label);
      assert.equal(row.querySelector("small"), null);
      const objects = row.querySelectorAll("[data-object-row]").length;
      assert.equal(row.querySelectorAll('input[type="checkbox"]').length, objects || 1);
      for (const member of hidden) assert.equal(s.doc.querySelector(`[data-metric-row="${member}"]`), null);
    }
  } finally { s.dom.window.close(); }
});

test("multiple example devices have independent values and switches", () => {
  const s = setup(); try {
    for (const [tab, key] of [["磁盘", "drive.model"], ["磁盘", "drive.read_rate"], ["GPU", "gpu.usage"], ["网络", "interface.upload_rate"]]) {
      s.tab(tab); const row = s.doc.querySelector(`[data-metric-row="${key}"]`), objects = [...row.querySelectorAll(".metric-object")];
      assert.equal(objects.length, 2); assert.ok(objects.every(object => object.querySelector(".value") && object.querySelector('input[type="checkbox"]')));
      const first = objects[0].querySelector("input"), second = objects[1].querySelector("input"); first.checked = true; s.change(first); assert.equal(second.checked, false);
      s.tab("CPU"); s.tab(tab); const restored = s.doc.querySelector(`[data-metric-row="${key}"]`).querySelectorAll('input[type="checkbox"]');
      assert.equal(restored[0].checked, true); assert.equal(restored[1].checked, false);
    }
  } finally { s.dom.window.close(); }
});

test("per-core CPU has one global switch, multiple values and no subtitles", () => {
  const s = setup(); try {
    const row = s.doc.querySelector('[data-metric-row="core.usage"]');
    assert.equal(row.querySelectorAll("[data-object-row]").length, 2);
    assert.equal(row.querySelectorAll('input[type="checkbox"]').length, 1);
    assert.equal(row.querySelectorAll('.metric-object input').length, 1);
    assert.equal(row.querySelector('[data-object-row] input').getAttribute('aria-label'), '上传 逐核 CPU 占用率');
    assert.equal(row.textContent.includes('全部核心'), false);
    assert.equal(row.querySelector("small"), null);
    assert.equal(s.doc.querySelectorAll(".metric-content .metric-row small").length, 0);
    const toggle = row.querySelector('input[type="checkbox"]'); toggle.checked = true; s.change(toggle);
    s.tab("网络"); s.tab("CPU"); assert.equal(s.doc.querySelector('[data-metric-row="core.usage"] input').checked, true);
  } finally { s.dom.window.close(); }
});

test('device switches cascade independently across network, GPU and disks', () => {
  const s = setup(); try {
    for (const [tab, parent, child, first, second] of [
      ['网络', 'interface.medium', 'interface.ipv4', 'en0', 'en1'],
      ['GPU', 'gpu.usage', 'gpu.temperature', '集成 GPU', '外置 GPU'],
      ['磁盘', 'drive.model', 'drive.read_rate', 'disk0', 'disk1'],
    ]) {
      s.tab(tab);
      const input = (key, object) => s.doc.querySelector(`[data-metric="${key}"][data-object="${object}"]`);
      assert.equal(input(child, first).disabled, true);
      for (const device of [first, second]) { let box = input(parent, device); box.checked = true; s.change(box); box = input(child, device); assert.equal(box.disabled, false); box.checked = true; s.change(box); }
      const box = input(parent, first); box.checked = false; s.change(box);
      assert.equal(input(child, first).disabled, true); assert.equal(input(child, first).checked, false);
      assert.equal(input(child, second).disabled, false); assert.equal(input(child, second).checked, true);
    }
  } finally { s.dom.window.close(); }
});

test('memory and disk information are bundled', () => {
  const s = setup(); try {
    s.tab('内存（Swap）'); const memory = s.doc.querySelector('[data-metric-row="memory.usage"]');
    assert.match(memory.textContent, /58\.4%/); assert.match(memory.textContent, /9\.34 \/ 16 GiB/); assert.equal(memory.querySelector('input'), null);
    assert.equal(s.doc.querySelector('[data-metric-row="memory.total"]'), null); assert.equal(s.doc.querySelector('[data-metric-row="memory.used"]'), null);
    s.tab('磁盘'); const disk = s.doc.querySelector('[data-metric-row="drive.model"]');
    assert.match(disk.textContent, /512 GiB/); assert.equal(disk.querySelectorAll('input').length, 2);
    assert.equal(s.doc.querySelector('[data-metric-row="drive.total"]'), null);
  } finally { s.dom.window.close(); }
});
