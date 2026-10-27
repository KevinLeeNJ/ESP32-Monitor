import { collectorService, type CollectorBridge } from "./collector.js";
import type { CollectorCapabilities, CollectorConfig, CollectorSnapshot } from "../types/collector.js";
import { prepareMetricDraft, validateMetricDraft } from "./collector-metrics.js";

/** Keeps editing state separate from live snapshots. A failed apply preserves
 * the draft for repair and the previous committed config shown by the Agent. */
export class CollectorController {
  capabilities?: CollectorCapabilities;
  snapshot?: CollectorSnapshot;
  draft?: CollectorConfig;
  busy = false;
  error = "";
  pollError = "";
  private service;
  private listeners = new Set<() => void>();
  private timer?: ReturnType<typeof setTimeout>;
  private stopped = true;

  constructor(bridge: CollectorBridge, private pollMS = 1000) {
    this.service = collectorService(bridge);
  }
  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    listener();
    return () => this.listeners.delete(listener);
  }
  notifyDraftChanged() { this.emit(); }
  private emit() { this.listeners.forEach(listener => listener()); }
  async start() {
    this.stopped = false;
    await this.rescan();
    if (this.stopped) return;
    await this.refresh();
    this.schedule();
  }
  stop() { this.stopped = true; clearTimeout(this.timer); }
  private schedule() {
    if (this.stopped) return;
    this.timer = setTimeout(async () => { await this.refresh(); this.schedule(); }, this.pollMS);
  }
  async refresh() {
    if (this.busy) return;
    try {
      const snapshot = await this.service.snapshot();
      if (this.stopped || this.busy || snapshot.revision < (this.snapshot?.revision ?? 0)) return;
      this.snapshot = snapshot;
      this.draft ??= structuredClone(snapshot.config);
      this.pollError = "";
    } catch (error) { this.pollError = String(error); }
    this.emit();
  }
  async rescan() {
    if (this.busy) return;
    this.busy = true; this.error = ""; this.emit();
    try { this.capabilities = await this.service.rescan(); }
    catch (error) { this.error = String(error); }
    finally { this.busy = false; this.emit(); }
  }
  async apply() {
    if (this.busy || !this.draft) return;
    const error = validateMetricDraft(this.draft, this.capabilities);
    if (error) { this.error = error; this.emit(); return; }
    this.busy = true; this.error = ""; this.emit();
    try {
      this.snapshot = await this.service.apply(prepareMetricDraft(this.draft, this.capabilities));
      this.draft = structuredClone(this.snapshot.config);
    } catch (error) { this.error = String(error); }
    finally { this.busy = false; this.emit(); }
  }
}
