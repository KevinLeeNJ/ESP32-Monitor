import type { CollectorCapabilities, CollectorConfig, CollectorSnapshot } from "../types/collector.js";
import { call } from "./wails.js";

// Collector bindings over the authenticated Agent IPC; the web frontend never
// sees tokens or filesystem paths.
export const collectorService = {
  rescan: () => call<CollectorCapabilities>("CollectorService.DiscoverCollectorMetrics"),
  capabilities: () => call<CollectorCapabilities>("CollectorService.GetCollectorCapabilities"),
  snapshot: () => call<CollectorSnapshot>("CollectorService.GetCollectorSnapshot"),
  apply: (config: CollectorConfig) => call<CollectorSnapshot>("CollectorService.ApplyCollectorConfig", config),
};
