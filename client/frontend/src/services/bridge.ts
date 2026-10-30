import { call } from "./wails.js";
import type { AppStatus, DiagnosticsReport, GuiSettings, LogExportResult, ServerRegistration, ServerTestResult } from "../types/app.js";
import type { CompanionSnapshot, GitHubSession } from "../types/companion.js";
import type { DiscoveredDevice, DiscoveryResult, DeviceStatus, PairingStatus } from "../types/monitors.js";

export const appService = {
  status: () => call<AppStatus>("AppService.Status"),
  diagnostics: () => call<DiagnosticsReport>("AppService.DiagnosticsReport"),
  exportLogs: () => call<LogExportResult>("AppService.ExportLogs"),
  serverTest: (baseURL: string) => call<ServerTestResult>("AppService.ServerTest", baseURL),
  registerServer: (baseURL: string, code: string) => call<ServerRegistration>("AppService.RegisterServer", baseURL, code),
  quit: () => call<void>("AppService.QuitApp"),
  getSettings: () => call<GuiSettings>("AppService.GetSettings"),
  saveSettings: (settings: GuiSettings) => call<GuiSettings>("AppService.SaveSettings", settings),
  restartAgent: () => call<void>("AppService.RestartAgent"),
  lastAgentError: () => call<string>("AppService.LastAgentError"),
};

export const companionService = {
  snapshot: () => call<CompanionSnapshot>("CompanionService.GetCompanionSnapshot"),
  nowPlaying: () => call<CompanionSnapshot["music"]>("CompanionService.GetNowPlaying"),
  githubSession: () => call<GitHubSession>("CompanionService.GitHubSession"),
  githubLoginStart: () => call<GitHubSession>("CompanionService.GitHubLoginStart"),
  githubLoginPoll: (sessionID: string) => call<GitHubSession>("CompanionService.GitHubLoginPoll", sessionID),
  githubLoginCancel: (sessionID: string) => call<GitHubSession>("CompanionService.GitHubLoginCancel", sessionID),
  githubLogout: (login: string) => call<void>("CompanionService.GitHubLogout", login),
};

export const monitorService = {
  discover: (timeoutMS: number) => call<DiscoveryResult>("MonitorService.DiscoverMonitors", timeoutMS),
  devices: () => call<DeviceStatus[]>("MonitorService.GetMonitorDevices"),
  pairingStatus: () => call<PairingStatus>("MonitorService.GetPairingStatus"),
  beginPairing: (device: string, trustLAN: boolean) => call<PairingStatus>("MonitorService.BeginPairing", device, trustLAN),
  confirmPairing: (pairingID: string, approve: boolean) => call<PairingStatus>("MonitorService.ConfirmPairing", pairingID, approve),
  revoke: (device: string) => call<void>("MonitorService.RevokeDevice", device),
  setEnabled: (device: string, enabled: boolean) => call<void>("MonitorService.SetDeviceEnabled", device, enabled),
  confirmLANConsent: (device: string, trusted: boolean) => call<void>("MonitorService.ConfirmLANConsent", device, trusted),
  setLANInterface: (ip: string) => call<void>("MonitorService.SetLANInterface", ip),
  serialPorts: () => call<DiscoveredDevice[]>("MonitorService.ListSerialPorts"),
  probeSerial: (port: string) => call<DiscoveredDevice>("MonitorService.ProbeSerial", port),
  connectSerial: (port: string, deviceID: string, trustPhysicalConnection: boolean) =>
    call<void>("MonitorService.ConnectSerial", port, deviceID, trustPhysicalConnection),
};
