/** DTOs mirrored from the Agent IPC surface (client/internal/ipc, client/internal/guibridge). */

export interface AppStatus {
  schema_version: number;
  pid: number;
  start_time: string;
  collector_enabled: boolean;
  companion_enabled: boolean;
  github_enabled: boolean;
  music_enabled: boolean;
  codex_enabled: boolean;
  socket_path: string;
}

export interface ServerTestResult {
  ok: boolean;
  status_code: number;
  error?: string;
  duration_ms: number;
  url: string;
}

export interface ServerRegistration {
  device_id: string;
  device_name: string;
  server_url: string;
}

export interface GuiSettings {
  schema_version: number;
  collector: { enabled: boolean; upload_url?: string; upload_token_file?: string; device_id?: string; device_name?: string };
  companion: { enabled: boolean; bind_ip?: string };
  github: { login?: string; client_id?: string };
  codex: { enabled: boolean };
  music: { enabled: boolean };
  launch: { launch_at_login: boolean; close_to_background: boolean; show_in_status_bar: boolean };
}

export interface ProviderHealth {
  state: string;
  observed_at: string;
  last_success_at?: string | null;
  error_code?: string;
  source_version?: string;
}

export interface CollectorReportSection {
  state: string;
  error_code?: string;
  revision: number;
  profile_revision: number;
  interval_seconds: number;
  upload_enabled: boolean;
  selected_count: number;
  queue_depth: number;
  dropped_metrics: number;
  upload_error?: string;
  last_received_at?: string | null;
  last_upload_at?: string | null;
  running_pid: number;
}

export interface DiagnosticsReport {
  schema_version: number;
  generated_at: string;
  pid: number;
  collector?: CollectorReportSection | null;
  companion: { github: ProviderHealth; codex: ProviderHealth; music: ProviderHealth };
  monitor?: { device_count: number; paired_count: number; online_count: number } | null;
}
