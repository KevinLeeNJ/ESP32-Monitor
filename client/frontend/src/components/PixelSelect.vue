<script setup lang="ts" generic="T extends string | number">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from "vue";

const props = defineProps<{
  modelValue: T;
  options: Array<{ value: T; label: string }>;
  label: string;
  disabled?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: T];
  change: [value: T];
}>();

const root = ref<HTMLDivElement>();
const trigger = ref<HTMLButtonElement>();
const menu = ref<HTMLDivElement>();
const open = ref(false);
const dropUp = ref(false);
const menuId = `pixel-select-${useId()}`;

const selectedIndex = computed(() => props.options.findIndex(option => option.value === props.modelValue));
const selectedLabel = computed(() => props.options[selectedIndex.value]?.label ?? "");

function optionButton(index: number): HTMLButtonElement | undefined {
  return menu.value?.querySelector<HTMLButtonElement>(`[data-index="${index}"]`) ?? undefined;
}

function focusActive(index: number) {
  optionButton(index)?.focus();
}

async function show() {
  if (props.disabled || open.value) return;
  open.value = true;
  dropUp.value = false;
  await nextTick();
  const anchor = trigger.value?.getBoundingClientRect();
  const height = menu.value?.offsetHeight ?? 0;
  dropUp.value = !!anchor && anchor.bottom + height + 10 > window.innerHeight && anchor.top > height + 10;
  focusActive(Math.max(0, selectedIndex.value));
}

function close(refocus = false) {
  if (!open.value) return;
  open.value = false;
  if (refocus) trigger.value?.focus();
}

function toggle() {
  if (open.value) close(true);
  else void show();
}

function pick(index: number) {
  const option = props.options[index];
  if (!option) return;
  if (option.value !== props.modelValue) {
    emit("update:modelValue", option.value);
    emit("change", option.value);
  }
  close(true);
}

function moveActive(delta: number) {
  if (!open.value) return;
  const current = props.options.findIndex((_, index) => optionButton(index) === document.activeElement);
  const start = current >= 0 ? current : Math.max(0, selectedIndex.value);
  const next = (start + delta + props.options.length) % props.options.length;
  focusActive(next);
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (props.disabled) return;
  switch (event.key) {
    case "ArrowDown":
    case "ArrowUp":
    case "Enter":
    case " ":
      event.preventDefault();
      if (open.value) moveActive(event.key === "ArrowUp" ? -1 : 1);
      else void show();
      break;
    case "Escape":
      if (open.value) {
        event.preventDefault();
        close(true);
      }
      break;
  }
}

function onMenuKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case "ArrowDown":
      event.preventDefault();
      moveActive(1);
      break;
    case "ArrowUp":
      event.preventDefault();
      moveActive(-1);
      break;
    case "Home":
      event.preventDefault();
      focusActive(0);
      break;
    case "End":
      event.preventDefault();
      focusActive(props.options.length - 1);
      break;
    case "Escape":
      event.preventDefault();
      close(true);
      break;
    case "Tab":
      close();
      break;
  }
}

function onDocumentPointer(event: PointerEvent) {
  if (open.value && !root.value?.contains(event.target as Node)) close();
}

function onViewportChange() {
  if (open.value) close();
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointer);
  window.addEventListener("resize", onViewportChange);
  window.addEventListener("scroll", onViewportChange, true);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointer);
  window.removeEventListener("resize", onViewportChange);
  window.removeEventListener("scroll", onViewportChange, true);
});
</script>

<template>
  <div ref="root" class="pixel-select" :class="{ open, 'drop-up': dropUp }">
    <button ref="trigger" type="button" class="pixel-select-trigger" aria-haspopup="listbox"
      :aria-expanded="open" :aria-controls="menuId" :aria-label="label" :disabled="disabled"
      @click="toggle" @keydown="onTriggerKeydown">
      <span class="pixel-select-label">{{ selectedLabel }}</span>
      <i class="pixelart-icons-font-chevron-down" aria-hidden="true"></i>
    </button>
    <div v-show="open" :id="menuId" ref="menu" class="pixel-select-menu" role="listbox"
      :aria-label="label" @keydown="onMenuKeydown">
      <button v-for="(option, index) in options" :key="String(option.value)" type="button"
        role="option" class="pixel-select-option" :data-index="index"
        :aria-selected="index === selectedIndex" @click="pick(index)">
        <span>{{ option.label }}</span>
        <i v-if="index === selectedIndex" class="pixelart-icons-font-check" aria-hidden="true"></i>
      </button>
    </div>
  </div>
</template>
