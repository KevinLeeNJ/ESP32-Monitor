import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() } }));
vi.mock("../../src/services/bridge.js", () => ({
  appService: {}, companionService: {},
  monitorService: { devices: vi.fn() },
}));

import { monitorService } from "../../src/services/bridge.js";
import { refreshDevices, store } from "../../src/store.js";

describe("device configuration errors", () => {
  it("shows one toast per rejection and alerts again after recovery", async () => {
    const device = { device_id: "board", device_name: "Monitor", config_error: "invalid_config" };
    vi.mocked(monitorService.devices).mockResolvedValue([device] as never);
    await refreshDevices();
    expect(store.toast).toContain("Monitor 的配置被拒绝");
    store.toast = "";
    await refreshDevices();
    expect(store.toast).toBe("");
    vi.mocked(monitorService.devices).mockResolvedValue([{ ...device, config_error: undefined }] as never);
    await refreshDevices();
    vi.mocked(monitorService.devices).mockResolvedValue([device] as never);
    await refreshDevices();
    expect(store.toast).toContain("Monitor 的配置被拒绝");
  });
});
