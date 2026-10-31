import { collectorService } from "./collector.js";
import type { CollectorCapabilities, CollectorConfig, CollectorSnapshot } from "../types/collector.js";
import { ensureUploadConfig, prepareMetricDraft, repairV2Selections, validateMetricDraft } from "./collector-metrics.js";

/** Keeps editing state separate from live snapshots. A failed apply preserves
 * the draft for repair and the previous committed config shown by the Agent. */
export class CollectorController {
  capabilities?: CollectorCapabilities;
  snapshot?: CollectorSnapshot;
  draft?: CollectorConfig;
  busy = false;
  error = "";
  pollError = "";
  private service = collectorService;
  private listeners = new Set<() => void>();
  private timer?: ReturnType<typeof setTimeout>;
  private stopped = true;
  private capsRetryAt = 0;

  constructor(private pollMS = 1000) {}
  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    listener();
    return () => this.listeners.delete(listener);
  }
  notifyDraftChanged() { this.emit(); }
  private emit() { this.listeners.forEach(listener => listener()); }
  async start() {
    this.stopped = false;
    // The agent creates its IPC socket slightly after the window appears; a
    // failed first rescan would otherwise leave the error pill stuck.
    for (let attempt = 0; attempt < 10 && !this.stopped; attempt++) {
      await this.rescan();
      if (!this.error) break;
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    if (this.stopped) return;
    await this.refresh();
    this.schedule();
  }
  stop() { this.stopped = true; clearTimeout(this.timer); }
  private schedule() {
    if (this.stopped) return;
    this.timer = setTimeout(async () => {
      if (!document.hidden) await this.refresh();
      this.schedule();
    }, this.pollMS);
  }
  async refresh() {
    if (this.busy) return;
    try {
      const snapshot = await this.service.snapshot();
      if (this.stopped || this.busy) return;
      // A restarted Agent starts a fresh revision counter; only compare
      // revisions within the same process.
      if (this.snapshot && snapshot.pid === this.snapshot.pid && snapshot.revision < this.snapshot.revision) return;
      this.snapshot = snapshot;
      if (!this.draft) {
        this.draft = structuredClone(snapshot.config);
        this.repairDraft();
      }
      this.pollError = "";
      if (!this.capabilities && Date.now() >= this.capsRetryAt) {
        this.capsRetryAt = Date.now() + 5000;
        void this.rescan();
      }
    } catch (error) { this.pollError = String(error); }
    this.emit();
  }
  async rescan() {
    if (this.busy) return;
    this.busy = true; this.error = ""; this.emit();
    try {
      this.capabilities = await this.service.rescan();
      this.repairDraft();
    }
    catch (error) { this.error = String(error); }
    finally { this.busy = false; this.emit(); }
  }
  // Hardware changes silently invalidate saved selections; prune them so the
  // draft never carries choices the GUI cannot display or uncheck.
  private repairDraft() {
    if (this.draft && this.capabilities?.v2) repairV2Selections(this.draft, this.capabilities.v2);
  }
  // Commits only the upload switch on top of the last applied config; unsaved
  // edits on the metrics page stay in the draft.
  async setUploadEnabled(enabled: boolean): Promise<boolean> {
    if (this.busy) return false;
    if (!this.snapshot) { this.error = "Agent 尚未就绪，请稍后重试"; this.emit(); return false; }
    const config = prepareMetricDraft(this.snapshot.config, this.capabilities);
    ensureUploadConfig(config).enabled = enabled;
    this.busy = true; this.error = ""; this.emit();
    try {
      this.snapshot = await this.service.apply(config);
      if (this.draft) ensureUploadConfig(this.draft).enabled = enabled;
      return true;
    } catch (error) {
      this.error = String(error);
      return false;
    } finally {
      this.busy = false; this.emit();
    }
  }
  async apply(): Promise<boolean> {
    if (this.busy || !this.draft) return false;
    const error = validateMetricDraft(this.draft);
    if (error) { this.error = error; this.emit(); return false; }
    this.busy = true; this.error = ""; this.emit();
    try {
      this.snapshot = await this.service.apply(prepareMetricDraft(this.draft, this.capabilities));
      this.draft = structuredClone(this.snapshot.config);
      return true;
    } catch (error) {
      this.error = String(error);
      return false;
    } finally {
      this.busy = false; this.emit();
    }
  }
}
