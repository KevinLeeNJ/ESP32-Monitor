import { CollectorController } from "../services/collector-controller.js";
import type { CollectorBridge } from "../services/collector.js";

/** Mount in the trusted desktop shell with its native CollectorBridge.
 * No upload token, filesystem API or arbitrary HTTP access enters this component. */
export function mountCollectorPanel(root: HTMLElement, bridge: CollectorBridge) {
  const controller = new CollectorController(bridge);
  const doc = root.ownerDocument;
  const element = <K extends keyof HTMLElementTagNameMap>(tag: K, text?: string) => {
    const node = doc.createElement(tag);
    if (text !== undefined) node.textContent = text;
    return node;
  };
  let renderedModel = "";
  const unsubscribe = controller.subscribe(() => {
    const model = JSON.stringify([controller.capabilities, controller.draft, controller.busy]);
    const statusText = controller.error || controller.pollError || controller.snapshot?.error_code || controller.snapshot?.state || "加载中";
    const previewText = JSON.stringify(controller.snapshot?.preview ?? { metrics: [] }, null, 2);
    if (model === renderedModel) {
      const status = root.querySelector("output"); if (status) status.textContent = statusText;
      const preview = root.querySelector("pre"); if (preview) preview.textContent = previewText;
      const uploadError = root.querySelector("[data-upload-error]");
      if (uploadError) uploadError.textContent = controller.snapshot?.upload_error ?? "";
      return;
    }
    renderedModel = model;
    root.replaceChildren();
    const header = element("p", "Core Collector · gopsutil / Native Collector · stub");
    const status = element("output", statusText);
    status.setAttribute("aria-live", "polite");
    root.append(header, status);
    const rescan = element("button", "重新扫描");
    rescan.disabled = controller.busy;
    rescan.onclick = () => void controller.rescan();
    root.append(rescan);
    const draft = controller.draft;
    if (!draft) return;
    const intervalLabel = element("label", "采集间隔（秒） ");
    const interval = element("input");
    interval.type = "number"; interval.min = "5"; interval.max = "60"; interval.value = String(draft.interval_seconds);
    interval.disabled = controller.busy;
    interval.onchange = () => {
      draft.interval_seconds = Number(interval.value);
      if (draft.upload) draft.upload.interval_seconds = draft.interval_seconds;
    };
    intervalLabel.append(interval); root.append(intervalLabel);
    for (const category of controller.capabilities?.categories ?? []) {
      const group = element("fieldset");
      group.append(element("legend", category.name));
      for (const source of category.sources) {
        group.append(element("p", `${source.id} (${source.collector}) · ${source.state}${source.reason ? " · " + source.reason : ""}`));
        for (const metric of source.metrics) {
          const row = element("div");
          const selected = draft.selections.find(selection => selection.id === metric.id);
          const localLabel = element("label", `${metric.measurement}.${metric.field} (${metric.unit}) `);
          const local = element("input"); local.type = "checkbox"; local.checked = Boolean(selected); local.disabled = controller.busy;
          local.setAttribute("aria-label", `采集 ${metric.id}`);
          local.onchange = () => {
            if (local.checked) draft.selections.push({ id: metric.id, upload: false });
            else draft.selections = draft.selections.filter(selection => selection.id !== metric.id);
            controller.notifyDraftChanged();
          };
          localLabel.prepend(local); row.append(localLabel);
          const uploadLabel = element("label", " 上传");
          const upload = element("input"); upload.type = "checkbox"; upload.checked = selected?.upload ?? false;
          upload.disabled = controller.busy || !selected || !metric.upload_supported;
          upload.title = metric.upload_reason ?? "";
          upload.setAttribute("aria-label", `上传 ${metric.id}`);
          upload.onchange = () => { if (selected) selected.upload = upload.checked; };
          uploadLabel.prepend(upload); row.append(uploadLabel);
          if (selected && metric.objects.length) {
            const label = element("label", " 对象（不选表示全部） ");
            const objects = element("select"); objects.multiple = true;
            objects.disabled = controller.busy;
            objects.setAttribute("aria-label", `对象 ${metric.id}`);
            const key = (tags: Record<string, string>) => JSON.stringify(Object.entries(tags).sort());
            for (const tags of metric.objects) {
              const option = element("option", JSON.stringify(tags));
              option.selected = selected.objects?.some(actual => key(actual) === key(tags)) ?? false;
              objects.append(option);
            }
            objects.onchange = () => { selected.objects = Array.from(objects.selectedOptions).map(option => structuredClone(metric.objects[option.index])); };
            label.append(objects); row.append(label);
          }
          group.append(row);
        }
      }
      root.append(group);
    }
    const available = new Set(controller.capabilities?.categories.flatMap(category => category.sources.flatMap(source => source.metrics.map(metric => metric.id))) ?? []);
    for (const selection of draft.selections.filter(selection => !available.has(selection.id))) {
      const remove = element("button", `移除已不可用指标 ${selection.id}`);
      remove.disabled = controller.busy;
      remove.onclick = () => {
        draft.selections = draft.selections.filter(actual => actual.id !== selection.id);
        controller.notifyDraftChanged();
      };
      root.append(remove);
    }
    const apply = element("button", "立即应用");
    apply.disabled = controller.busy;
    apply.onclick = () => {
      if (!Number.isInteger(draft.interval_seconds) || draft.interval_seconds < 5 || draft.interval_seconds > 60) {
        controller.error = "采集间隔必须为 5–60 秒的整数"; controller.notifyDraftChanged(); return;
      }
      void controller.apply();
    };
    root.append(apply);
    root.append(element("pre", previewText));
    const uploadError = element("p", controller.snapshot?.upload_error ?? "");
    uploadError.setAttribute("data-upload-error", ""); root.append(uploadError);
  });
  void controller.start();
  return { controller, dispose() { controller.stop(); unsubscribe(); root.replaceChildren(); } };
}
