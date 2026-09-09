<script setup lang="ts">
import {
  computed,
  getCurrentInstance,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import Icon from "./Icon.vue";
const props = defineProps<{
  modelValue: string;
  label: string;
  options: { value: string; label: string; disabled?: boolean }[];
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [string]; change: [string] }>();
const trigger = ref<HTMLButtonElement>(),
  menu = ref<HTMLElement>();
const open = ref(false),
  active = ref(0),
  position = ref<Record<string, string>>({});
const menuId = `select-${getCurrentInstance()!.uid}`;
const selected = computed(() =>
  props.options.find((o) => o.value === props.modelValue),
);
let search = "",
  searchAt = 0;
function place() {
  if (!open.value || !trigger.value) return;
  const r = trigger.value.getBoundingClientRect(),
    gap = 6,
    edge = 8;
  const wanted = Math.min(280, props.options.length * 38 + 10);
  const below = window.innerHeight - r.bottom - gap - edge,
    above = r.top - gap - edge;
  const up = below < wanted && above > below;
  const height = Math.max(40, Math.min(wanted, up ? above : below));
  const width = Math.min(Math.max(r.width, 180), window.innerWidth - edge * 2);
  position.value = {
    left: `${Math.max(edge, Math.min(r.left, window.innerWidth - width - edge))}px`,
    width: `${width}px`,
    maxHeight: `${height}px`,
    ...(up
      ? { bottom: `${window.innerHeight - r.top + gap}px` }
      : { top: `${r.bottom + gap}px` }),
  };
}
function revealActive() {
  nextTick(() =>
    document
      .getElementById(`${menuId}-${active.value}`)
      ?.scrollIntoView({ block: "nearest" }),
  );
}
function show() {
  if (props.disabled) return;
  active.value = Math.max(
    0,
    props.options.findIndex((o) => o.value === props.modelValue),
  );
  open.value = true;
  place();
  revealActive();
}
function choose(index: number) {
  const option = props.options[index];
  if (!option || option.disabled) return;
  emit("update:modelValue", option.value);
  emit("change", option.value);
  trigger.value?.dispatchEvent(new Event("change", { bubbles: true }));
  open.value = false;
  trigger.value?.focus();
}
function move(direction: number) {
  for (let n = 0; n < props.options.length; n++) {
    active.value =
      (active.value + direction + props.options.length) % props.options.length;
    if (!props.options[active.value]?.disabled) break;
  }
  revealActive();
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Tab") {
    open.value = false;
    return;
  }
  if (event.key === "Escape") {
    event.preventDefault();
    open.value = false;
    return;
  }
  if (
    ["ArrowDown", "ArrowUp", "Home", "End", "Enter", " "].includes(event.key)
  ) {
    event.preventDefault();
    if (!open.value) {
      show();
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp")
      move(event.key === "ArrowDown" ? 1 : -1);
    else if (event.key === "Home") {
      active.value = props.options.length - 1;
      move(1);
    } else if (event.key === "End") {
      active.value = 0;
      move(-1);
    } else choose(active.value);
    return;
  }
  if (
    event.key.length === 1 &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  ) {
    if (!open.value) show();
    search = Date.now() - searchAt > 700 ? event.key : search + event.key;
    searchAt = Date.now();
    const i = props.options.findIndex(
      (o) =>
        !o.disabled &&
        o.label.toLocaleLowerCase().startsWith(search.toLocaleLowerCase()),
    );
    if (i >= 0) {
      active.value = i;
      revealActive();
    }
  }
}
function outside(event: Event) {
  if (
    !trigger.value?.contains(event.target as Node) &&
    !menu.value?.contains(event.target as Node)
  )
    open.value = false;
}
function scroll(event: Event) {
  if (!menu.value?.contains(event.target as Node)) place();
}
watch(
  () => props.disabled,
  (v) => {
    if (v) open.value = false;
  },
);
watch(
  () => props.options,
  () => {
    if (open.value) {
      active.value = Math.min(active.value, props.options.length - 1);
      place();
    }
  },
);
onMounted(() => {
  document.addEventListener("pointerdown", outside, true);
  document.addEventListener("focusin", outside);
  window.addEventListener("resize", place);
  document.addEventListener("scroll", scroll, true);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", outside, true);
  document.removeEventListener("focusin", outside);
  window.removeEventListener("resize", place);
  document.removeEventListener("scroll", scroll, true);
});
</script>
<template>
  <button
    ref="trigger"
    type="button"
    role="combobox"
    class="app-select"
    :disabled="disabled"
    :aria-label="label"
    aria-haspopup="listbox"
    :aria-expanded="open"
    :aria-controls="open ? menuId : undefined"
    :aria-activedescendant="open ? `${menuId}-${active}` : undefined"
    :title="selected?.label"
    @click="open ? (open = false) : show()"
    @keydown="keydown"
  >
    <span>{{ selected?.label || "请选择" }}</span
    ><Icon name="ChevronDown" :size="15" :class="{ rotated: open }" />
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      :id="menuId"
      ref="menu"
      class="select-menu"
      role="listbox"
      :aria-label="label"
      :style="position"
      @pointerdown.prevent
    >
      <div
        v-for="(option, index) in options"
        :id="`${menuId}-${index}`"
        :key="option.value"
        role="option"
        class="select-option"
        :class="{ active: index === active, disabled: option.disabled }"
        :aria-selected="option.value === modelValue"
        :aria-disabled="option.disabled || undefined"
        @pointermove="active = index"
        @click="choose(index)"
      >
        <span>{{ option.label }}</span
        ><Icon v-if="option.value === modelValue" name="Check" :size="15" />
      </div>
    </div>
  </Teleport>
</template>
