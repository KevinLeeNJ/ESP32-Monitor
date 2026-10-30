export function fmtTime(value?: string | null): string {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

export function fmtClockTime(value?: string | null): string {
  return value ? new Date(value).toLocaleTimeString("zh-CN") : "—";
}

export function fmtClockFromSeconds(seconds?: number | null): string {
  return seconds ? new Date(seconds * 1000).toLocaleTimeString("zh-CN") : "—";
}

export function fmtBytes(bytes?: number | null): string {
  if (!bytes) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
