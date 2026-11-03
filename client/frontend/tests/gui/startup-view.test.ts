import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../src/store.js", async () => {
  const { reactive } = await import("vue");
  return {
    store: reactive({
      settings: null,
      status: null,
      statusError: "",
      toast: "",
      page: "startup",
    }),
    saveSettings: vi.fn(async settings => settings),
    toast: vi.fn(),
    cloneSettings: (settings: unknown) => JSON.parse(JSON.stringify(settings)),
    quitApp: vi.fn(),
    restartAgent: vi.fn(),
  };
});

import StartupView from "../../src/views/StartupView.vue";
import { saveSettings, store, toast } from "../../src/store.js";

function settings() {
  return {
    collector: { enabled: true },
    music: { enabled: false },
    codex: { enabled: false },
    companion: { enabled: true },
    launch: {
      close_to_background: true,
      launch_at_login: false,
      show_in_status_bar: true,
    },
  };
}

describe("StartupView", () => {
  beforeEach(() => {
    store.settings = settings();
    store.status = {
      pid: 4321,
      start_time: "2026-11-03T10:00:00Z",
      collector_enabled: true,
      companion_enabled: false,
      github_enabled: true,
      codex_enabled: false,
      music_enabled: true,
    };
    store.statusError = "";
    store.toast = "";
    vi.mocked(saveSettings).mockClear();
    vi.mocked(toast).mockClear();
  });

  it("renders switches and the live backend summary", () => {
    const wrapper = mount(StartupView);
    expect((wrapper.get('input[aria-label="指标采集"]').element as HTMLInputElement).checked).toBe(true);
    expect((wrapper.get('input[aria-label="系统播放"]').element as HTMLInputElement).checked).toBe(false);
    expect((wrapper.get('input[aria-label="上位机服务"]').element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.text()).toContain("PID 4321");
    expect(wrapper.text()).toContain("2 / 3 启用");
    expect(wrapper.findAll(".pill").map(node => node.text())).toContain("未启用");
  });

  it("saves a detached copy when a switch changes", async () => {
    const wrapper = mount(StartupView);
    await wrapper.get('input[aria-label="指标采集"]').setValue(false);
    await flushPromises();

    expect(saveSettings).toHaveBeenCalledTimes(1);
    const saved = vi.mocked(saveSettings).mock.calls[0][0] as ReturnType<typeof settings>;
    expect(saved.collector.enabled).toBe(false);
    expect(saved).not.toBe(store.settings);
    expect(toast).toHaveBeenCalledWith("设置已保存");
  });

  it("restores the previous settings when saving fails", async () => {
    vi.mocked(saveSettings).mockRejectedValueOnce(new Error("disk full"));
    const wrapper = mount(StartupView);
    await wrapper.get('input[aria-label="指标采集"]').setValue(false);
    await flushPromises();

    expect(store.settings?.collector.enabled).toBe(true);
    expect(toast).toHaveBeenCalledWith(expect.stringContaining("保存失败"));
  });

  it("marks the agent unreachable without hiding the last known status", () => {
    store.statusError = "pipe unavailable";
    const wrapper = mount(StartupView);
    expect(wrapper.text()).toContain("不可达");
    expect(wrapper.text()).toContain("PID 4321");
  });
});
