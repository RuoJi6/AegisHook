<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import Icon from "./Icon.vue";

const props = defineProps<{ modelValue: string; label: string; prefix: string }>();
const emit = defineEmits<{ "update:modelValue": [string] }>();
const trigger = ref<HTMLButtonElement>();
const menu = ref<HTMLElement>();
const open = ref(false);
const position = ref<Record<string, string>>({});
const id = `calendar-${getCurrentInstance()!.uid}`;
const weekdays = ["日", "一", "二", "三", "四", "五", "六"];
const month = ref(new Date(new Date().getFullYear(), new Date().getMonth(), 1));
const focused = ref("");

function dateKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function parseDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return year && month && day ? new Date(year, month - 1, day) : new Date();
}
const today = computed(() => dateKey(new Date()));
const title = computed(() => `${month.value.getFullYear()}年${month.value.getMonth() + 1}月`);
const weeks = computed(() => {
  const first = new Date(month.value);
  first.setDate(1 - first.getDay());
  return Array.from({ length: 6 }, (_, row) =>
    Array.from({ length: 7 }, (_, column) => {
      const date = new Date(first);
      date.setDate(first.getDate() + row * 7 + column);
      return { key: dateKey(date), day: date.getDate(), outside: date.getMonth() !== month.value.getMonth() };
    }),
  );
});
function place() {
  if (!open.value || !trigger.value) return;
  const rect = trigger.value.getBoundingClientRect();
  const edge = 8, gap = 6;
  const width = Math.min(280, window.innerWidth - edge * 2);
  const height = menu.value?.getBoundingClientRect().height || 318;
  const below = window.innerHeight - rect.bottom - gap - edge;
  const above = rect.top - gap - edge;
  const up = below < height && above > below;
  position.value = {
    width: `${width}px`,
    left: `${Math.max(edge, Math.min(rect.left, window.innerWidth - width - edge))}px`,
    maxHeight: `${Math.max(80, up ? above : below)}px`,
    ...(up ? { bottom: `${window.innerHeight - rect.top + gap}px` } : { top: `${rect.bottom + gap}px` }),
  };
}
async function focusDay() {
  await nextTick();
  document.getElementById(`${id}-${focused.value}`)?.focus({ preventScroll: true });
}
async function show() {
  const selected = parseDate(props.modelValue);
  focused.value = dateKey(selected);
  month.value = new Date(selected.getFullYear(), selected.getMonth(), 1);
  open.value = true;
  place();
  await nextTick();
  place();
  await focusDay();
}
function close(restoreFocus = false) {
  open.value = false;
  if (restoreFocus) trigger.value?.focus();
}
function choose(value: string) {
  emit("update:modelValue", value);
  close(true);
}
function moveMonth(direction: number) {
  const selected = parseDate(focused.value);
  const next = new Date(month.value.getFullYear(), month.value.getMonth() + direction, 1);
  const days = new Date(next.getFullYear(), next.getMonth() + 1, 0).getDate();
  focused.value = dateKey(new Date(next.getFullYear(), next.getMonth(), Math.min(selected.getDate(), days)));
  month.value = next;
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close(true);
    return;
  }
  const target = event.target as HTMLElement;
  if (!target.hasAttribute("data-calendar-day")) return;
  const date = parseDate(focused.value);
  const offsets: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
  if (event.key in offsets) date.setDate(date.getDate() + offsets[event.key]!);
  else if (event.key === "Home") date.setDate(date.getDate() - date.getDay());
  else if (event.key === "End") date.setDate(date.getDate() + 6 - date.getDay());
  else if (event.key === "PageUp" || event.key === "PageDown") {
    event.preventDefault();
    moveMonth(event.key === "PageUp" ? -1 : 1);
    void focusDay();
    return;
  } else return;
  event.preventDefault();
  focused.value = dateKey(date);
  month.value = new Date(date.getFullYear(), date.getMonth(), 1);
  void focusDay();
}
function outside(event: Event) {
  if (!trigger.value?.contains(event.target as Node) && !menu.value?.contains(event.target as Node)) close();
}
function scroll(event: Event) {
  if (!menu.value?.contains(event.target as Node)) place();
}
onMounted(() => {
  document.addEventListener("pointerdown", outside, true);
  document.addEventListener("focusin", outside);
  document.addEventListener("scroll", scroll, true);
  window.addEventListener("resize", place);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", outside, true);
  document.removeEventListener("focusin", outside);
  document.removeEventListener("scroll", scroll, true);
  window.removeEventListener("resize", place);
});
</script>

<template>
  <button
    ref="trigger" type="button" class="app-date-picker" :aria-label="label"
    aria-haspopup="dialog" :aria-expanded="open" :aria-controls="open ? id : undefined"
    @click="open ? close() : show()"
    @keydown.down.prevent="show()" @keydown.esc.prevent="close(true)"
  >
    <span class="date-prefix">{{ prefix }}</span>
    <span class="date-value" :class="{ placeholder: !modelValue }">{{ modelValue || '选择日期' }}</span>
    <Icon name="CalendarDays" :size="14" />
  </button>
  <Teleport to="body">
    <div v-if="open" :id="id" ref="menu" class="date-menu" role="dialog" :aria-label="`${label}日历`" :style="position" @keydown="keydown">
      <div class="date-menu-heading">
        <strong :id="`${id}-title`" aria-live="polite">{{ title }}</strong>
        <div class="date-menu-navigation">
          <button type="button" class="icon-button" aria-label="上个月" @click="moveMonth(-1)"><Icon name="ChevronLeft" :size="16" /></button>
          <button type="button" class="icon-button" aria-label="下个月" @click="moveMonth(1)"><Icon name="ChevronRight" :size="16" /></button>
        </div>
      </div>
      <div class="date-grid" role="grid" :aria-labelledby="`${id}-title`">
        <div class="date-grid-row date-weekdays" role="row">
          <span v-for="day in weekdays" :key="day" role="columnheader">{{ day }}</span>
        </div>
        <div v-for="(week, index) in weeks" :key="index" class="date-grid-row" role="row">
          <div v-for="day in week" :key="day.key" role="gridcell" :aria-selected="day.key === modelValue">
            <button
              :id="`${id}-${day.key}`" type="button" class="date-day" data-calendar-day
              :class="{ outside: day.outside, selected: day.key === modelValue, today: day.key === today }"
              :aria-label="day.key" :aria-current="day.key === today ? 'date' : undefined"
              :tabindex="day.key === focused ? 0 : -1"
              @focus="focused = day.key" @click="choose(day.key)"
            >{{ day.day }}</button>
          </div>
        </div>
      </div>
      <div class="date-menu-footer">
        <button type="button" class="text-button" @click="choose('')">清空</button>
        <button type="button" class="text-button" @click="choose(dateKey(new Date()))">今天</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.app-date-picker {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  width: 150px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font-size: 12px;
  font-weight: 400;
}
.app-date-picker[aria-expanded="true"] {
  border-color: var(--accent);
  outline: 2px solid color-mix(in srgb, var(--accent) 30%, transparent);
  outline-offset: 2px;
}
.date-prefix, .placeholder, .app-date-picker > svg { color: var(--muted); }
.date-value { flex: 1; text-align: left; white-space: nowrap; }
.app-date-picker > svg { flex-shrink: 0; }
.date-menu {
  position: fixed;
  z-index: 1200;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  box-shadow: 0 8px 24px #0002;
}
.date-menu-heading, .date-menu-navigation, .date-menu-footer { display: flex; align-items: center; justify-content: space-between; }
.date-menu-heading { margin-bottom: 8px; }
.date-menu-heading strong { font-size: 13px; font-weight: 600; }
.date-menu-navigation { gap: 2px; }
.date-grid-row { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 2px; }
.date-weekdays { margin-bottom: 4px; font-size: 12px; line-height: 28px; text-align: center; color: var(--muted); }
.date-day { width: 100%; height: 32px; padding: 0; border: 0; border-radius: 6px; font-size: 12px; color: var(--text); background: transparent; }
.date-day:hover { background: var(--soft); }
.date-day.outside { color: var(--muted); opacity: .6; }
.date-day.today { color: var(--accent); font-weight: 600; }
.date-day.selected { color: var(--accent); background: var(--accent-soft); font-weight: 600; opacity: 1; }
.date-menu-footer { margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--border); }
@media (max-width: 700px) { .app-date-picker { flex: 1 1 140px; width: auto; } }
</style>
