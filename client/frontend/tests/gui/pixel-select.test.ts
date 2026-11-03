import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import PixelSelect from "../../src/components/PixelSelect.vue";

const options = [
  { value: "cpu", label: "CPU" },
  { value: "memory", label: "内存" },
  { value: "disk", label: "磁盘" },
];

function mountSelect(extra: Record<string, unknown> = {}) {
  return mount(PixelSelect, {
    attachTo: document.body,
    props: { modelValue: "memory", options, label: "指标来源", ...extra },
  });
}

describe("PixelSelect", () => {
  it("shows the selected label and exposes listbox semantics", async () => {
    const wrapper = mountSelect();
    const trigger = wrapper.get("button.pixel-select-trigger");
    expect(trigger.text()).toContain("内存");
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(trigger.attributes("aria-label")).toBe("指标来源");

    await trigger.trigger("click");
    expect(trigger.attributes("aria-expanded")).toBe("true");
    const listbox = wrapper.get("[role=listbox]");
    expect(listbox.isVisible()).toBe(true);
    expect(wrapper.findAll("[role=option]")).toHaveLength(3);
    expect(wrapper.get('[data-index="1"]').attributes("aria-selected")).toBe("true");
    wrapper.unmount();
  });

  it("emits a selection and closes the menu", async () => {
    const wrapper = mountSelect();
    await wrapper.get("button.pixel-select-trigger").trigger("click");
    await wrapper.get('[data-index="2"]').trigger("click");
    expect(wrapper.emitted("update:modelValue")).toEqual([["disk"]]);
    expect(wrapper.get("button.pixel-select-trigger").attributes("aria-expanded")).toBe("false");
    wrapper.unmount();
  });

  it("supports keyboard navigation and escape", async () => {
    const wrapper = mountSelect();
    const trigger = wrapper.get("button.pixel-select-trigger");
    await trigger.trigger("keydown", { key: "ArrowDown" });
    expect(trigger.attributes("aria-expanded")).toBe("true");

    const menu = wrapper.get("[role=listbox]");
    await menu.trigger("keydown", { key: "End" });
    expect(document.activeElement?.textContent).toContain("磁盘");
    await menu.trigger("keydown", { key: "Home" });
    expect(document.activeElement?.textContent).toContain("CPU");
    await menu.trigger("keydown", { key: "Escape" });
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(trigger.element);
    wrapper.unmount();
  });

  it("does not open while disabled", async () => {
    const wrapper = mountSelect({ disabled: true });
    const trigger = wrapper.get("button.pixel-select-trigger");
    expect(trigger.attributes("disabled")).toBeDefined();
    await trigger.trigger("click");
    await trigger.trigger("keydown", { key: "Enter" });
    expect(trigger.attributes("aria-expanded")).toBe("false");
    wrapper.unmount();
  });

  it("closes when the user clicks outside or resizes the window", async () => {
    const wrapper = mountSelect();
    const trigger = wrapper.get("button.pixel-select-trigger");
    await trigger.trigger("click");
    document.body.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    await wrapper.vm.$nextTick();
    expect(trigger.attributes("aria-expanded")).toBe("false");

    await trigger.trigger("click");
    window.dispatchEvent(new Event("resize"));
    await wrapper.vm.$nextTick();
    expect(trigger.attributes("aria-expanded")).toBe("false");
    wrapper.unmount();
  });
});
