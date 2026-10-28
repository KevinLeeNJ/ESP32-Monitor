import type { CollectorBridge } from "./collector.js";
import { call } from "./wails.js";

// Implements the frontend CollectorBridge contract over the authenticated
// Agent IPC; the web frontend never sees tokens or filesystem paths.
export const wailsCollectorBridge: CollectorBridge = {
  DiscoverCollectorMetrics: () => call("CollectorService.DiscoverCollectorMetrics"),
  GetCollectorCapabilities: () => call("CollectorService.GetCollectorCapabilities"),
  GetCollectorSnapshot: () => call("CollectorService.GetCollectorSnapshot"),
  ApplyCollectorConfig: (config) => call("CollectorService.ApplyCollectorConfig", config),
};
