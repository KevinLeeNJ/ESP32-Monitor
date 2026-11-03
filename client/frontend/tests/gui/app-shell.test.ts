import { shallowMount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({
  Call: { ByName: vi.fn() },
}));

vi.mock("../../src/store.js", async () => {
  const { reactive } = await import("vue");
  return {
    store: reactive({
      page: "overview",
      status: null,
      statusError: "",
      toast: "",
    }),
    pages: [
      ["overview", "总览", "icon-layout"],
      ["metrics", "指标采集", "icon-chart"],
      ["server", "Server 连接", "icon-server"],
      ["companion", "上位机", "icon-monitor"],
      ["esp32", "ESP32", "icon-cpu"],
      ["startup", "启动与后台", "icon-power"],
      ["diagnostics", "诊断", "icon-tools"],
    ],
    startPolling: vi.fn(),
    stopPolling: vi.fn(),
    quitApp: vi.fn(),
  };
});

import App from "../../src/App.vue";
import { quitApp, startPolling, stopPolling, store } from "../../src/store.js";

describe("App shell", () => {
  beforeEach(() => {
    store.page = "overview";
    store.status = null;
    store.statusError = "";
    store.toast = "";
    vi.mocked(startPolling).mockClear();
    vi.mocked(stopPolling).mockClear();
    vi.mocked(quitApp).mockClear();
  });

  it("starts polling on mount and stops on unmount", () => {
    const wrapper = shallowMount(App);
    expect(startPolling).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    expect(stopPolling).toHaveBeenCalledTimes(1);
  });

  it("navigates between pages and updates the breadcrumb", async () => {
    const wrapper = shallowMount(App);
    const buttons = wrapper.findAll(".nav button");
    expect(buttons).toHaveLength(7);
    expect(buttons[0].attributes("aria-current")).toBe("page");
    expect(wrapper.get(".crumb").text()).toContain("总览");

    await buttons[1].trigger("click");
    expect(store.page).toBe("metrics");
    expect(wrapper.get(".crumb").text()).toContain("指标采集");
    expect(wrapper.findAll(".nav button")[1].attributes("aria-current")).toBe("page");
  });

  it("renders agent connectivity, toast and exit action", async () => {
    const wrapper = shallowMount(App);
    expect(wrapper.get(".topright .pill").text()).toBe("连接 Agent…");

    store.status = { pid: 7 } as never;
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".topright .pill").text()).toBe("Agent 运行中");

    store.statusError = "socket closed";
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".topright .pill").text()).toBe("Agent 不可达");

    store.toast = "设置已保存";
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".toast").text()).toBe("设置已保存");

    await wrapper.get(".sidefoot-actions button").trigger("click");
    expect(quitApp).toHaveBeenCalledTimes(1);
  });
});
