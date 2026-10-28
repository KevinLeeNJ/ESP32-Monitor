import { Call } from "@wailsio/runtime";

// Fully qualified binding prefix; Wails v3 derives it from the Go package path
// plus the bound struct name (bindings.go getMethods).
const FQN = "esp32-monitor/client/internal/guibridge";

export function call<T>(name: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`${FQN}.${name}`, ...args) as Promise<T>;
}

/** The Agent error_code literal travels as the rejection message. */
export function errCode(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}
