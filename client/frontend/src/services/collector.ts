import type { CollectorCapabilities, CollectorConfig, CollectorSnapshot } from "../types/collector";

/** Implement in the trusted native bridge. The web frontend receives no tokens,
 * filesystem paths or Telegraf TOML. Go Application exposes the same methods. */
export interface CollectorBridge {
  DiscoverCollectorMetrics(): Promise<CollectorCapabilities>;
  GetCollectorCapabilities(): Promise<CollectorCapabilities>;
  GetCollectorSnapshot(): Promise<CollectorSnapshot>;
  ApplyCollectorConfig(config: CollectorConfig): Promise<CollectorSnapshot>;
}

export function collectorService(bridge: CollectorBridge) {
  return {
    rescan: () => bridge.DiscoverCollectorMetrics(),
    capabilities: () => bridge.GetCollectorCapabilities(),
    snapshot: () => bridge.GetCollectorSnapshot(),
    apply: (config: CollectorConfig) => bridge.ApplyCollectorConfig(config),
  };
}
