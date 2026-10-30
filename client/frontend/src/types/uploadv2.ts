/** Mirror of the Go uploadv2 wire contract (client/internal/collector/uploadv2).
 * uint64 fields marked `,string` on the Go side arrive as decimal strings. */

export type UploadSelection = { metric: string; objects?: number[] };
export interface UploadConfig { enabled: boolean; interval_seconds: number; optional: UploadSelection[] }

export interface Profile { version: number; interval_seconds: number; enabled: UploadSelection[]; inventory: Inventory }
export interface ProfileOperation { op: "add" | "replace" | "remove"; path: string; value?: unknown }

export interface ObjectDescriptor { id: number; kind: "core" | "gpu" | "drive" | "interface" | "battery" | "volume" | "fan"; name: string; model?: string }
export interface Inventory {
  objects?: ObjectDescriptor[];
  os?: OSInfo; product?: string; cpu?: CPUInfo; memory_total?: string;
  cores?: CoreInfo[]; gpus?: ObjectInfo[]; drives?: DriveInfo[]; volumes?: VolumeInfo[];
  interfaces?: InterfaceInfo[]; batteries?: ObjectInfo[]; fans?: FanInfo[];
}
export interface OSInfo { family: string; name: string; version: string; build?: string }
export interface CPUInfo { model?: string; physical_cores?: number; logical_cores?: number; performance_cores?: number; efficiency_cores?: number }
export interface ObjectInfo { id: number }
export interface CoreInfo { id: number; index: number; kind?: string }
export interface DriveInfo { id: number; model?: string; total?: string }
export interface VolumeInfo { id: number; drives?: number[]; filesystem?: string; total?: string }
export interface InterfaceInfo { id: number; medium?: string }
export interface FanInfo { id: number; scope: string; gpu?: number }

export interface SampleBatch { r: number; id: string; s: Sample[] }
export interface Sample {
  t: number;
  /** Required, non-null: percentage × 100, 0–10000. */
  c: number | null; m: number | null; u: number | null;
  cpu?: CPUSample; mem?: MemorySample; cores?: CoreSample[]; gpu?: GPUSample[];
  disk?: DriveSample[]; vol?: VolumeSample[]; net?: InterfaceSample[];
  ip?: IPv4Observation; bat?: BatterySample[]; fan?: FanSample[];
}
export interface CPUSample { l1?: number; l5?: number; l15?: number; t?: number }
export interface MemorySample { u?: string; a?: string; w?: string; c?: string; st?: string; su?: string; p?: number }
export interface CoreSample { i: number; u?: number }
export interface GPUSample { i: number; u?: number; t?: number }
export interface DriveSample { i: number; u?: string; r?: string; w?: string; sm?: SMARTSample }
export interface SMARTSample { t: number; h?: boolean }
export interface VolumeSample { i: number; u?: string }
export interface InterfaceSample { i: number; up?: boolean; tx?: string; rx?: string; ip?: string[] }
export interface IPv4Observation { t: number; v: string }
export interface BatterySample { i: number; l: number | null; c: boolean | null }
export interface FanSample { i: number; r?: number }
