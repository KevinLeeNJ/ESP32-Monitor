import type { CollectorController } from "../services/collector-controller.js";
import type { CollectorV2Capabilities } from "../types/collector.js";
import { bundleUploads, deviceDependencies, displayMetrics, invalidV2Selections, isRequired, metricGroup, metricGroups, metricNames, pruneDeviceDependents, repairV2Selections, rowValue, selectedObjects, smartDetails } from "../services/collector-metrics.js";

export function renderV2Metrics(root: HTMLElement, controller: CollectorController, caps: CollectorV2Capabilities) {
  const doc = root.ownerDocument, draft = controller.draft!;
  draft.upload ??= { enabled: false, interval_seconds: draft.interval_seconds, optional: [] };
  const upload = draft.upload;
  bundleUploads(upload, caps);
  const element = <K extends keyof HTMLElementTagNameMap>(tag: K, text?: string) => { const node = doc.createElement(tag); if (text !== undefined) node.textContent = text; return node; };
  const enableLabel = element("label", "启用远端指标上报 ");
  const enabled = element("input"); enabled.type = "checkbox"; enabled.checked = upload.enabled; enabled.disabled = controller.busy;
  enabled.setAttribute("aria-label", "启用远端指标上报");
  enabled.onchange = () => { upload.enabled = enabled.checked; controller.notifyDraftChanged(); };
  enableLabel.prepend(enabled); root.append(enableLabel);
  root.append(element("p", "总体 CPU、内存占用率和运行时间为必选项。其余指标可选择上传；型号、OS、容量等固定信息只在启动时采集。"));
  const metrics = displayMetrics(caps);
  if (metrics.some(metric => metric.metric === "interface.ipv4" && metric.members.some(member => member.metric === "network.public_ipv4"))) root.append(element("p", "选择 IP 地址会通过外部服务查询公网 IP。"));
  for (const groupName of metricGroups) {
    const groupMetrics = metrics.filter(metric => metricGroup(metric.metric, caps) === groupName).sort((a, b) => Number(isRequired(b)) - Number(isRequired(a))); if (!groupMetrics.length) continue;
    const group = element("fieldset"); group.dataset.metricGroup = groupName; group.append(element("legend", groupName));
    for (const metric of groupMetrics) {
      const key = metric.metric, required = isRequired(metric);
      const row = element("div"); row.dataset.metric = key;
      row.append(element("strong", metric.bundle?.title ?? metricNames[key] ?? key));
      const selected = upload.optional.find(selection => metric.members.some(member => member.metric === selection.metric && (!metric.objects || member.objects)));
      if (required) {
        const value = element("span", rowValue(metric, caps, controller.snapshot)); value.dataset.v2Value = key; value.className = "metric-value"; row.append(value);
        const badge = element("span", "必选 · 自动采集并上传"); badge.dataset.required = key; badge.className = "metric-required"; row.append(badge);
      } else if (metric.objects) {
        let coreLabel: HTMLLabelElement | undefined;
        if (key === "core.usage") {
          const label = element("label"); const toggle = element("input"); toggle.type = "checkbox"; toggle.checked = Boolean(selected); toggle.disabled = controller.busy;
          toggle.setAttribute("aria-label", "上传 逐核 CPU 占用率"); toggle.dataset.uploadKey = key;
          toggle.onchange = () => {
            upload.optional = upload.optional.filter(selection => selection.metric !== key);
            if (toggle.checked) upload.optional.push({ metric: key, objects: [...metric.objects!] });
            controller.notifyDraftChanged();
          };
          label.prepend(toggle); coreLabel = label;
        }
        const objectList = element("div"); objectList.className = "metric-objects";
        for (const id of metric.objects) {
          const item = element("div"); item.className = "metric-object"; item.dataset.object = String(id);
          const name = caps.objects?.find(object => object.id === id)?.name ?? `对象 ${id}`;
          item.append(element("span", name));
          const value = element("span", rowValue(metric, caps, controller.snapshot, id)); value.dataset.v2Value = key; value.dataset.object = String(id); value.className = "metric-value"; item.append(value);
          if (key === "core.usage") { if (coreLabel) { item.append(coreLabel); coreLabel = undefined; } objectList.append(item); continue; }
          const label = element("label", " 上传"); const toggle = element("input"); toggle.type = "checkbox"; toggle.checked = selectedObjects(upload, key).includes(id);
          const parent = deviceDependencies[key]; toggle.disabled = controller.busy || Boolean(parent && !selectedObjects(upload, parent).includes(id));
          if (parent && toggle.disabled) toggle.title = "请先启用该设备的主指标";
          toggle.setAttribute("aria-label", `上传 ${metric.bundle?.title ?? metricNames[key] ?? key} ${name}`);
          toggle.dataset.uploadKey = `${key} ${id}`;
          toggle.onchange = () => {
            const ids = new Set(selectedObjects(upload, key)); if (toggle.checked) ids.add(id); else ids.delete(id);
            const keys = metric.bundle?.members ?? [key];
            if (!ids.size) upload.optional = upload.optional.filter(selection => !keys.includes(selection.metric));
            else for (const member of metric.members) {
              let selection = upload.optional.find(entry => entry.metric === member.metric);
              if (!selection) { selection = { metric: member.metric }; upload.optional.push(selection); }
              if (member.objects) selection.objects = [...ids].filter(object => key !== "drive.model" || member.objects!.includes(object)).sort((a, b) => a - b);
              if (member.objects && !selection.objects?.length) { upload.optional = upload.optional.filter(entry => entry !== selection); continue; }
              if (key === "drive.smart_attribute") {
                const allowed = smartDetails(caps, selection.objects ?? []);
                const previous = selection.detail;
                selection.detail = previous ? previous.filter(detail => allowed.some(entry => entry.id === detail)) : allowed.map(detail => detail.id);
              }
            }
            if (!toggle.checked) pruneDeviceDependents(upload, key, id);
            controller.notifyDraftChanged();
          };
          label.prepend(toggle); item.append(label); objectList.append(item);
        }
        row.append(objectList);
        if (selected && key === "drive.smart_attribute") {
          const detailLabel = element("label", " 所选磁盘共同支持的 SMART 细项 "), details = element("select"); details.multiple = true; details.disabled = controller.busy;
          details.setAttribute("aria-label", "SMART 细项");
          for (const detail of smartDetails(caps, selected.objects ?? [])) {
            const option = element("option", detail.name); option.value = String(detail.id); option.selected = selected.detail?.includes(detail.id) ?? false; details.append(option);
          }
          details.onchange = () => { selected.detail = Array.from(details.selectedOptions, option => Number(option.value)); controller.notifyDraftChanged(); };
          detailLabel.append(details); row.append(detailLabel);
        }
      } else {
        const value = element("span", rowValue(metric, caps, controller.snapshot)); value.dataset.v2Value = key; value.className = "metric-value"; row.append(value);
        const label = element("label", " 上传"); const toggle = element("input"); toggle.type = "checkbox"; toggle.checked = Boolean(selected); toggle.disabled = controller.busy;
        toggle.setAttribute("aria-label", `上传 ${metric.bundle?.title ?? metricNames[key] ?? key}`);
        toggle.dataset.uploadKey = key;
        toggle.onchange = () => {
          const keys = metric.bundle?.members ?? [key];
          upload.optional = upload.optional.filter(selection => !keys.includes(selection.metric));
          if (toggle.checked) for (const member of metric.members) upload.optional.push({ metric: member.metric });
          controller.notifyDraftChanged();
        };
        label.prepend(toggle); row.append(label);
      }
      group.append(row);
    }
    root.append(group);
  }
  if (!metrics.length) root.append(element("p", "暂未获取到可用指标，请重新检测。"));
  const invalid = invalidV2Selections(draft, caps);
  if (invalid.length) {
    const warning = element("p", `有 ${invalid.length} 项已保存选择当前不可用，未在指标列表中显示。`);
    const repair = element("button", "清理不可用选择"); repair.disabled = controller.busy;
    repair.onclick = () => { repairV2Selections(draft, caps); controller.notifyDraftChanged(); };
    root.append(warning, repair);
  }
}
export function updateV2MetricValues(root: HTMLElement, controller: CollectorController) {
  const caps = controller.capabilities?.v2; if (!caps) return;
  for (const node of Array.from(root.querySelectorAll<HTMLElement>("[data-v2-value]"))) {
    const metric = displayMetrics(caps).find(metric => metric.metric === node.dataset.v2Value);
    if (metric) node.textContent = rowValue(metric, caps, controller.snapshot, node.dataset.object === undefined ? undefined : Number(node.dataset.object));
  }
}
