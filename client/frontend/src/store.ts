import { reactive } from "vue";
import { appService, companionService, monitorService } from "./services/bridge.js";
import type { AppStatus, GuiSettings } from "./types/app.js";
import type { CompanionSnapshot } from "./types/companion.js";
import type { DeviceStatus } from "./types/monitors.js";
import { CollectorController } from "./services/collector-controller.js";
import { wailsCollectorBridge } from "./services/collector-bridge.js";
import { errCode } from "./services/wails.js";

export type PageID = "overview" | "metrics" | "server" | "companion" | "esp32" | "startup" | "diagnostics";

export const pages: Array<[PageID, string, string]> = [
  ["overview", "总览", "pixelart-icons-font-layout"],
  ["metrics", "指标采集", "pixelart-icons-font-chart-bar-big"],
  ["server", "Server 连接", "pixelart-icons-font-server"],
  ["companion", "上位机", "pixelart-icons-font-monitor"],
  ["esp32", "ESP32", "pixelart-icons-font-cpu"],
  ["startup", "启动与后台", "pixelart-icons-font-power-off"],
  ["diagnostics", "诊断", "pixelart-icons-font-tools"],
];

interface Store {
  page: PageID;
  status: AppStatus | null;
  statusError: string;
  settings: GuiSettings | null;
  companion: CompanionSnapshot | null;
  companionError: string;
  devices: DeviceStatus[];
  restarting: boolean;
  toast: string;
}

export const store = reactive<Store>({
  page: "overview",
  status: null,
  statusError: "",
  settings: null,
  companion: null,
  companionError: "",
  devices: [],
  restarting: false,
  toast: "",
});

/** Background GUI data poll; every view reads these snapshots. */
const GUI_REFRESH_MS = 5000;

export const collector = new CollectorController(wailsCollectorBridge, GUI_REFRESH_MS);

let toastTimer: ReturnType<typeof setTimeout> | undefined;
export function toast(message: string) {
  store.toast = message;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { store.toast = ""; }, 3200);
}

export async function refreshStatus() {
  try {
    store.status = await appService.status();
    store.statusError = "";
  } catch (error) {
    // A settings save may restart the Agent; its socket is briefly absent and is
    // not a real failure. Keep the last known status until the save settles.
    if (store.restarting) return;
    store.statusError = String(error);
  }
}

export async function refreshCompanion() {
  // GitHub, Codex and system playback run independently of the ESP32 companion
  // service; the Agent answers with disabled sections for providers that are off.
  try {
    store.companion = await companionService.snapshot();
    store.companionError = "";
  } catch (error) {
    // Settings may not be loaded yet; treat the disabled rejection as quiet.
    if (errCode(error) === "companion_disabled") {
      store.companion = null;
      store.companionError = "";
      return;
    }
    store.companionError = String(error);
  }
}

export async function refreshDevices() {
  try {
    store.devices = await monitorService.devices();
  } catch {
    store.devices = [];
  }
}

export async function saveSettings(settings: GuiSettings) {
  store.restarting = true;
  try {
    store.settings = await appService.saveSettings(settings);
    await refreshStatus();
    void collector.refresh();
  } finally {
    store.restarting = false;
  }
}

/** structuredClone cannot copy Vue reactive proxies; settings are plain IPC JSON. */
export function cloneSettings(settings: GuiSettings): GuiSettings {
  return JSON.parse(JSON.stringify(settings)) as GuiSettings;
}

let timers: ReturnType<typeof setInterval>[] = [];
export function startPolling() {
  void refreshStatus();
  // The agent creates its IPC socket shortly after launch; keep retrying so a
  // lost first read doesn't leave settings null (and saves no-op) all session.
  void (async () => {
    for (let attempt = 0; attempt < 30 && !store.settings; attempt++) {
      try { store.settings = await appService.getSettings(); } catch { /* retry */ }
      if (store.settings) return;
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
  })();
  void collector.start();
  timers = [
    setInterval(() => void refreshStatus(), GUI_REFRESH_MS),
    setInterval(() => void refreshCompanion(), GUI_REFRESH_MS),
    setInterval(() => void refreshDevices(), GUI_REFRESH_MS),
  ];
}

export function stopPolling() {
  timers.forEach(clearInterval);
  timers = [];
  collector.stop();
}
