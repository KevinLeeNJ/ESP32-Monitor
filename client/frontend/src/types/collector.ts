/** Collector schema v1. Values and objects come from Core/Native Collector samples. */
export type Tags = Record<string, string>;
export type CapabilityState = "available" | "partial" | "missing_dependency" | "permission_required" | "unsupported";
export interface CollectorSelection { id: string; objects?: Tags[]; upload: boolean }
export interface CollectorUploadSelection { metric: string; objects?: number[]; detail?: number[] }
export interface CollectorUploadConfig { enabled: boolean; interval_seconds?: number; optional: CollectorUploadSelection[] }
export interface CollectorV2Metric {
  metric: string; objects?: number[]; detail?: number[];
  fixed: boolean; available: boolean; required: boolean; reason?: string;
}
export interface CollectorV2Capabilities {
  inventory: Record<string, unknown>;
  metrics: CollectorV2Metric[];
  objects: Array<{ id: number; kind: string; name: string }>;
}
export interface CollectorConfig { interval_seconds: number; selections: CollectorSelection[]; upload?: CollectorUploadConfig }
export interface CollectorMetric {
  id: string; measurement: string; field: string; unit: string;
  upload_supported: boolean; upload_reason?: string; objects: Tags[];
}
export interface CollectorCapabilities {
  schema_version: 1; core_collector: string; native_collector: string; os: string; arch: string; observed_at: string;
  categories: Array<{ id: string; name: string; sources: Array<{
    id: string; collector: "core" | "native"; state: CapabilityState; reason?: string; metrics: CollectorMetric[];
  }> }>;
  v2?: CollectorV2Capabilities;
}
export interface CollectorBatch {
  metrics: Array<{ name: string; tags: Tags; fields: Record<string, number>; exact?: Record<string, string>; timestamp: number }>;
}
export interface CollectorSnapshot {
  schema_version: 1; revision: number; state: "disabled" | "applying" | "healthy" | "error";
  error_code?: string; pid: number; config: CollectorConfig; preview: CollectorBatch;
  last_received_at: string | null; last_upload_at: string | null; upload_error?: string;
  queue_depth: number; dropped_metrics: number;
  v2_sample?: { t: number; c: number; m: number; u: number; [key: string]: unknown };
  profile_revision?: number;
}
