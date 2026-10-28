/** Companion provider DTOs mirrored from client/internal/companion and providers. */
import type { ProviderHealth } from "./app.js";
import type { NowPlayingSnapshot } from "./now-playing.js";

export interface GitHubDay { date: string; count: number; level: number }
export interface GitHubCalendar { login: string; days: GitHubDay[]; total: number; today?: number | null }
export interface GitHubSnapshot { health: ProviderHealth; data?: GitHubCalendar | null; stale: boolean; next_refresh_at?: string | null }

export interface CodexWindow {
  bucket_id: string;
  window_minutes?: number | null;
  used_percent?: number | null;
  remaining_percent?: number | null;
  resets_at?: number | null;
}
export interface CodexQuota { health: ProviderHealth; windows: CodexWindow[] }
export interface CodexTask { id: string; turn_id?: string; state: string; observed_at: string }
export interface CodexTasks {
  adapter?: string; health: ProviderHealth; scope: string; state: string;
  active_count?: number | null; items: CodexTask[]; selected_task?: CodexTask | null;
}
export interface CodexSnapshot { quota: CodexQuota; tasks: CodexTasks }

export interface CompanionSnapshot {
  schema_version: number;
  observed_at: string;
  github: GitHubSnapshot;
  codex: CodexSnapshot;
  music: NowPlayingSnapshot;
}

export interface GitHubSession {
  id: string;
  state: string;
  user_code?: string;
  verification_uri?: string;
  expires_at: string;
  next_poll_at: string;
  login?: string;
  error_code?: string;
}
