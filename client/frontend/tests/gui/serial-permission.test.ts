import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() } }));
vi.mock("../../src/store.js", async () => {
  const { reactive } = await import("vue");
  return {
    store: reactive({ devices: [], devicesError: "", status: { companion_enabled: true }, settings: null }),
    toast: vi.fn(), cloneSettings: vi.fn(), refreshDevices: vi.fn(), saveSettings: vi.fn(),
  };
});

import { Call } from "@wailsio/runtime";
import Esp32View from "../../src/views/Esp32View.vue";
import { toast } from "../../src/store.js";

describe("serial permissions", () => {
  for (const failingCall of ["ProbeSerial", "ConnectSerial"]) {
    it(`shows a useful toast when ${failingCall} is denied`, async () => {
      vi.mocked(toast).mockClear();
      vi.mocked(Call.ByName).mockImplementation((name: string) => {
        if (name.endsWith("DiscoverMonitors")) return Promise.resolve({ devices: [{ port: "/dev/ttyACM0", name: "USB Monitor", connection_type: "serial" }] }) as never;
        if (name.endsWith(failingCall)) return Promise.reject(new Error("serial_permission_denied")) as never;
        if (name.endsWith("ProbeSerial")) return Promise.resolve({ device_id: "board", name: "Monitor", paired: true }) as never;
        throw new Error(`Unexpected call: ${name}`);
      });
      const wrapper = mount(Esp32View);
      await wrapper.get(".heading button").trigger("click");
      await flushPromises();
      await wrapper.get(".discovery-row button").trigger("click");
      await flushPromises();
      expect(toast).toHaveBeenLastCalledWith(expect.stringContaining("无权访问串口 /dev/ttyACM0"));
      expect(toast).toHaveBeenLastCalledWith(expect.stringContaining("dialout"));
      expect(toast).toHaveBeenLastCalledWith(expect.stringContaining("udev"));
      wrapper.unmount();
    });
  }
});
