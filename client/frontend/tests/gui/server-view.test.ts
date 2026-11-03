import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../src/services/bridge.js", () => ({
  appService: {
    registerServer: vi.fn(),
    getSettings: vi.fn(),
    serverTest: vi.fn(),
  },
}));

vi.mock("../../src/services/wails.js", () => ({
  errCode: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}));

vi.mock("../../src/store.js", async () => {
  const { reactive, ref } = await import("vue");
  return {
    store: reactive({
      settings: null,
      status: null,
      page: "server",
    }),
    cloneSettings: (settings: unknown) => JSON.parse(JSON.stringify(settings)),
    saveSettings: vi.fn(async settings => settings),
    toast: vi.fn(),
    useCollectorRevision: () => ref(0),
    collector: {
      snapshot: null,
      error: "",
      refresh: vi.fn(async () => {}),
      setUploadEnabled: vi.fn(async () => true),
    },
  };
});

import ServerView from "../../src/views/ServerView.vue";
import { appService } from "../../src/services/bridge.js";
import { collector, saveSettings, store, toast } from "../../src/store.js";

function settings(overrides: Record<string, unknown> = {}) {
  return {
    collector: {
      enabled: true,
      upload_url: "https://monitor.example",
      device_name: "",
      ...(overrides.collector as object),
    },
    music: { enabled: false },
    codex: { enabled: false },
    companion: { enabled: false },
    launch: { close_to_background: true, launch_at_login: false, show_in_status_bar: false },
  };
}

describe("ServerView", () => {
  beforeEach(() => {
    store.settings = settings();
    store.status = null;
    vi.mocked(appService.registerServer).mockReset();
    vi.mocked(appService.getSettings).mockReset();
    vi.mocked(appService.serverTest).mockReset();
    vi.mocked(collector.setUploadEnabled).mockClear();
    vi.mocked(toast).mockClear();
  });

  it("validates the address and the six-digit code before calling the Agent", async () => {
    const wrapper = mount(ServerView);
    await wrapper.get("#server-url").setValue("");
    await wrapper.get(".btn.primary").trigger("click");
    expect(wrapper.get(".form-error").text()).toBe("请填写 Server 地址");

    await wrapper.get("#server-url").setValue("https://monitor.example");
    await wrapper.get("#server-code").setValue("12ab34");
    expect((wrapper.get("#server-code").element as HTMLInputElement).value).toBe("1234");
    await wrapper.get(".btn.primary").trigger("click");
    expect(wrapper.get(".form-error").text()).toBe("请输入 6 位验证码");
    expect(appService.registerServer).not.toHaveBeenCalled();
  });

  it("maps Agent error codes to actionable messages", async () => {
    vi.mocked(appService.registerServer).mockRejectedValue(new Error("code_expired"));
    const wrapper = mount(ServerView);
    await wrapper.get("#server-code").setValue("123456");
    await wrapper.get(".btn.primary").trigger("click");
    await flushPromises();
    expect(wrapper.get(".form-error").text()).toContain("验证码已过期");
  });

  it("registers, refreshes settings and enables upload", async () => {
    vi.mocked(appService.registerServer).mockResolvedValue({
      server_url: "https://monitor.example",
      device_name: "测试工作站",
      token: "secret",
    } as never);
    vi.mocked(appService.getSettings).mockResolvedValue(
      settings({ collector: { device_name: "测试工作站" } }) as never,
    );
    const wrapper = mount(ServerView);
    await wrapper.get("#server-code").setValue("654321");
    await wrapper.get(".btn.primary").trigger("click");
    await flushPromises();

    expect(appService.registerServer).toHaveBeenCalledWith("https://monitor.example", "654321");
    expect(collector.refresh).toHaveBeenCalled();
    expect(collector.setUploadEnabled).toHaveBeenCalledWith(true);
    expect(wrapper.text()).toContain("已验证设备");
    expect(wrapper.text()).toContain("测试工作站");
    expect(toast).toHaveBeenCalledWith(expect.stringContaining("正在上传"));
  });

  it("shows the connection probe result", async () => {
    vi.mocked(appService.serverTest).mockResolvedValue({
      ok: true,
      status_code: 204,
      error: "",
      duration_ms: 12,
      url: "https://monitor.example/v2/collector/register",
    });
    const wrapper = mount(ServerView);
    const buttons = wrapper.findAll(".actions .btn");
    await buttons[buttons.length - 1].trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="server-test-result"]').text()).toContain("连接成功");
    expect(wrapper.get('[data-testid="server-test-result"]').text()).toContain("HTTP 204");
  });
});
