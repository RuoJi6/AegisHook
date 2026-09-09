<script setup lang="ts">
import { computed, ref } from "vue";
import type { Call } from "../types";
const props = defineProps<{ calls: Call[] }>();
const selected = ref(""),
  hovered = ref("");
const active = computed(() => hovered.value || selected.value);
const rows = computed(() =>
  [
    { key: "approve", label: "已允许", color: "#20b486" },
    { key: "reject", label: "已拦截", color: "#f05265" },
    { key: "pending", label: "待审查", color: "#e6a236" },
  ].map((r) => ({
    ...r,
    count: props.calls.filter((c) => c.decision === r.key).length,
  })),
);
const current = computed(() => rows.value.find((r) => r.key === active.value));
const total = computed(() => props.calls.length);
const percent = (n: number) =>
  total.value ? ((n / total.value) * 100).toFixed(1) + "%" : "—";
const circumference = 2 * Math.PI * 67;
const arc = (n: number) =>
  total.value ? (n / total.value) * circumference : 0;
const offset = (i: number) =>
  -rows.value.slice(0, i).reduce((a, r) => a + arc(r.count), 0);
</script>
<template>
  <div class="decision-donut">
    <div class="donut-graphic">
      <svg viewBox="0 0 180 180" role="group" aria-label="工具裁决环形图">
        <circle
          cx="90"
          cy="90"
          r="67"
          fill="none"
          stroke="var(--soft)"
          stroke-width="19"
        />
        <template v-for="(r, i) in rows" :key="r.key">
          <circle
            v-if="r.count"
            cx="90"
            cy="90"
            r="67"
            fill="none"
            :stroke="r.color"
            :stroke-width="active === r.key ? 24 : 19"
            :stroke-dasharray="`${arc(r.count)} ${circumference - arc(r.count)}`"
            :stroke-dashoffset="offset(i)"
            transform="rotate(-90 90 90)"
            role="button"
            tabindex="0"
            :aria-label="`${r.label} ${r.count} 次，占比 ${percent(r.count)}`"
            :aria-pressed="selected === r.key"
            @mouseenter="hovered = r.key"
            @mouseleave="hovered = ''"
            @focus="hovered = r.key"
            @blur="hovered = ''"
            @click="selected = selected === r.key ? '' : r.key"
            @keydown.enter.prevent="selected = selected === r.key ? '' : r.key"
            @keydown.space.prevent="selected = selected === r.key ? '' : r.key"
          />
        </template>
      </svg>
      <div class="donut-center" aria-live="polite">
        <strong>{{ current ? current.count : total }}</strong
        ><span>{{ current ? current.label : "工具调用" }}</span
        ><small>{{ current ? percent(current.count) : "当前裁决" }}</small>
      </div>
    </div>
    <div class="donut-legend">
      <button
        v-for="r in rows"
        :key="r.key"
        :class="{ active: active === r.key }"
        :aria-pressed="selected === r.key"
        @mouseenter="hovered = r.key"
        @mouseleave="hovered = ''"
        @click="selected = selected === r.key ? '' : r.key"
      >
        <span><i :style="{ background: r.color }" />{{ r.label }}</span
        ><b>{{ r.count.toLocaleString() }}</b
        ><small>{{ percent(r.count) }}</small>
      </button>
    </div>
    <p class="muted tiny">
      {{
        total
          ? "悬浮或点击分类查看；允许不代表执行成功。"
          : "所选时间范围内暂无工具调用。"
      }}
    </p>
  </div>
</template>
