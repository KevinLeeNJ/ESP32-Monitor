/** Mirror of the Go uploadv2 wire contract (client/internal/collector/uploadv2).
 * uint64 fields marked `,string` on the Go side arrive as decimal strings. */

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
