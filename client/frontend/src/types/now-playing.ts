/** Native binding / authenticated IPC DTO. Times are RFC3339 UTC strings.
 * Numeric media times are seconds; null is unavailable, zero is valid.
 * Show a playing indicator only for healthy current.state === playing.
 * not_playing covers paused/stopped. Healthy empty also means not playing.
 * Keep the media panel visible; errors are communicated separately by health.
 */
export type PlaybackState = 'playing' | 'not_playing';
export interface NowPlayingCurrent {
  title: string | null;
  artist: string | null;
  source: string | null;
  state: PlaybackState;
  duration: number | null;
  position: number | null;
  position_observed_at: string | null;
}
export interface NowPlayingSnapshot {
  adapter: string;
  coverage: 'system_current_source' | 'playing_first_lexical_bus_name' | 'none';
  health: {
    state: 'disabled' | 'starting' | 'healthy' | 'degraded' | 'permission_denied' | 'unsupported';
    observed_at: string;
    last_success_at: string | null;
    error_code?: string;
    source_version?: string;
  };
  current: NowPlayingCurrent | null;
}

/** Implement with future Wails binding or authenticated Agent IPC. */
export interface NowPlayingReader {
  GetNowPlayingSnapshot(): Promise<NowPlayingSnapshot>;
}
