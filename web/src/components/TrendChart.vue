<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, watch } from "vue";
export interface Series {
  key: string;
  label: string;
  color: string;
}
export interface Point {
  date: string;
  [key: string]: number | string | null;
}
const props = defineProps<{
  title: string;
  points: Point[];
  series: Series[];
  kind?: "bar" | "line";
  unit?: string;
}>();
const container = ref<HTMLElement>();
const width = ref(780),
  height = 230,
  left = 48,
  top = 20,
  bottom = 195;
const right = computed(() => width.value - 16);
let observer: ResizeObserver | undefined;
onMounted(() => {
  observer = new ResizeObserver((entries) => {
    width.value = Math.max(240, Math.floor(entries[0]!.contentRect.width));
  });
  if (container.value) observer.observe(container.value);
});
onBeforeUnmount(() => observer?.disconnect());
const n = (p: Point, key: string) =>
  typeof p[key] === "number" ? (p[key] as number) : 0;
const max = computed(
  () =>
    Math.max(
      0,
      ...props.points.map((p) =>
        props.kind === "line"
          ? Math.max(...props.series.map((s) => n(p, s.key)))
          : props.series.reduce((a, s) => a + n(p, s.key), 0),
      ),
    ) || 1,
);
const step = computed(
  () => (right.value - left) / Math.max(1, props.points.length),
);
const x = (i: number) => left + step.value * (i + 0.5);
const y = (v: number) => bottom - (v / max.value) * (bottom - top);
const number = (v: number) =>
  v >= 1e6
    ? (v / 1e6).toFixed(1) + "M"
    : v >= 1000
      ? (v / 1000).toFixed(1) + "k"
      : v < 1 && v > 0
        ? v.toPrecision(2)
        : Number(v.toFixed(2)).toLocaleString();
const stack = (p: Point, i: number) =>
  props.series.slice(0, i + 1).reduce((a, s) => a + n(p, s.key), 0);
function lines(s: Series) {
  let d = "";
  let gap = true;
  props.points.forEach((p, i) => {
    if (p[s.key] === null) {
      gap = true;
      return;
    }
    d += `${gap ? "M" : "L"}${x(i)},${y(n(p, s.key))} `;
    gap = false;
  });
  return d;
}
const tickEvery = computed(() =>
  Math.max(
    1,
    Math.ceil(props.points.length / Math.max(3, Math.floor(width.value / 95))),
  ),
);
const active = ref(-1);
const activePoint = computed(() => props.points[active.value]);
watch(
  () => props.points.length,
  () => {
    active.value = -1;
  },
);
function inspect(event: MouseEvent | PointerEvent) {
  const box = (event.currentTarget as SVGElement).getBoundingClientRect();
  const local = ((event.clientX - box.left) / box.width) * width.value;
  active.value = Math.min(
    props.points.length - 1,
    Math.max(0, Math.floor((local - left) / step.value)),
  );
}
function navigate(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End", "Escape"].includes(event.key))
    return;
  event.preventDefault();
  if (event.key === "Escape") {
    active.value = -1;
    return;
  }
  if (event.key === "Home") active.value = 0;
  else if (event.key === "End") active.value = props.points.length - 1;
  else
    active.value = Math.min(
      props.points.length - 1,
      Math.max(0, active.value + (event.key === "ArrowRight" ? 1 : -1)),
    );
}
const tooltipLeft = computed(() =>
  Math.max(0, Math.min(x(active.value) - 90, width.value - 200)),
);
</script>
<template>
  <div ref="container" class="trend-chart">
    <div class="chart-key">
      <span v-for="s in series" :key="s.key"
        ><i :style="{ background: s.color }" />{{ s.label }}</span
      ><span class="chart-unit">{{ unit }}</span>
    </div>
    <svg
      :viewBox="`0 0 ${width} ${height}`"
      role="img"
      tabindex="0"
      @mousemove="inspect"
      @pointerdown="inspect"
      @mouseleave="active = -1"
      @focus="active = points.length - 1"
      @blur="active = -1"
      @keydown="navigate"
      :aria-label="
        title +
        '，左右方向键切换日期，Home / End 跳到首尾；详细数值见下方数据表'
      "
    >
      <g v-for="v in [0, 0.5, 1]" :key="v">
        <line
          :x1="left"
          :x2="right"
          :y1="y(v * max)"
          :y2="y(v * max)"
          class="chart-grid"
        />
        <text :x="left - 10" :y="y(v * max) + 4" text-anchor="end">
          {{ number(v * max) }}
        </text>
      </g>
      <template v-if="kind === 'line'">
        <g v-for="s in series" :key="s.key">
          <path :d="lines(s)" fill="none" :stroke="s.color" stroke-width="2" />
          <template v-for="(p, i) in points" :key="p.date">
            <circle
              v-if="p[s.key] !== null && n(p, s.key) > 0"
              :cx="x(i)"
              :cy="y(n(p, s.key))"
              r="3"
              :fill="s.color"
            >
              <title>{{ p.date }} · {{ s.label }} {{ p[s.key] }}</title>
            </circle>
          </template>
        </g>
      </template>
      <template v-else>
        <g v-for="(p, i) in points" :key="p.date">
          <rect
            v-for="(s, j) in series"
            :key="s.key"
            :x="x(i) - Math.min(26, step * 0.65) / 2"
            :y="y(stack(p, j))"
            :width="Math.min(26, step * 0.65)"
            :height="(n(p, s.key) / max) * (bottom - top)"
            :fill="s.color"
          >
            <title>{{ p.date }} · {{ s.label }} {{ p[s.key] }}</title>
          </rect>
        </g>
      </template>
      <template v-for="(p, i) in points" :key="p.date">
        <text
          v-if="
            (i % tickEvery === 0 &&
              i < points.length - Math.max(1, tickEvery / 2)) ||
            i === points.length - 1
          "
          :x="x(i)"
          y="220"
          text-anchor="middle"
        >
          {{ p.date.slice(5) }}
        </text>
      </template>
      <line
        v-if="activePoint"
        :x1="x(active)"
        :x2="x(active)"
        :y1="top"
        :y2="bottom"
        stroke="var(--muted)"
        stroke-dasharray="3 3"
        pointer-events="none"
      />
    </svg>
    <div
      v-if="activePoint"
      class="chart-tooltip"
      role="status"
      :style="{ left: tooltipLeft + 'px' }"
    >
      <strong>{{ activePoint.date }}</strong>
      <div v-for="s in series" :key="s.key">
        <span><i :style="{ background: s.color }" />{{ s.label }}</span
        ><b
          >{{
            activePoint[s.key] === null
              ? "未知"
              : (activePoint[s.key] as number).toLocaleString("zh-CN", {
                  maximumFractionDigits: 6,
                })
          }}
          {{ activePoint[s.key] === null ? "" : unit }}</b
        >
      </div>
    </div>
    <details class="chart-data">
      <summary>查看每日数据</summary>
      <div class="chart-table">
        <table>
          <thead>
            <tr>
              <th>日期</th>
              <th v-for="s in series" :key="s.key">{{ s.label }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in points" :key="p.date">
              <td>{{ p.date }}</td>
              <td v-for="s in series" :key="s.key">
                {{
                  p[s.key] === null
                    ? "未计价 / 用量未知"
                    : typeof p[s.key] === "number"
                      ? (p[s.key] as number).toLocaleString("zh-CN", {
                          maximumFractionDigits: 6,
                        })
                      : p[s.key]
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </details>
  </div>
</template>
