<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useConsole, date, callTitle, decisionLabel } from "../store";
import AppSelect from "./AppSelect.vue";
import Icon from "./Icon.vue";
import TrendChart from "./TrendChart.vue";
import type { Point } from "./TrendChart.vue";
const store = useConsole(),
  route = useRoute(),
  router = useRouter();
const days = computed(() =>
  [7, 30, 90].includes(Number(route.query.days)) ? Number(route.query.days) : 7,
);
const model = computed({
  get: () => (typeof route.query.model === "string" ? route.query.model : ""),
  set: (v: string) =>
    void router.replace({ query: { ...route.query, model: v || undefined } }),
});
const currency = computed({
  get: () => (route.query.currency === "USD" ? "USD" : "CNY"),
  set: (v: string) =>
    void router.replace({ query: { ...route.query, currency: v } }),
});
const dayKey = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const dateKeys = computed(() => {
  const end = new Date(store.updatedAt || Date.now());
  return Array.from({ length: days.value }, (_, i) => {
    const d = new Date(end);
    d.setDate(end.getDate() - (days.value - 1 - i));
    return dayKey(d);
  });
});
const inRange = (s: string) => dateKeys.value.includes(dayKey(new Date(s)));
const calls = computed(() => store.calls.filter((c) => inRange(c.createdAt)));
const records = computed(() =>
  store.usage.filter(
    (u) =>
      inRange(u.createdAt) &&
      (!model.value || `${u.protocol}:${u.model}` === model.value),
  ),
);
const models = computed(() => [
  { value: "", label: "全部审查模型" },
  ...Array.from(new Set(store.usage.map((u) => `${u.protocol}:${u.model}`)))
    .sort()
    .map((v) => ({ value: v, label: v })),
]);
const known = computed(() => records.value.filter((r) => r.usage));
const totals = computed(() =>
  known.value.reduce(
    (a, r) => {
      const u = r.usage!;
      a.input += u.input;
      a.output += u.output;
      a.cacheRead += u.cacheRead;
      a.cacheWrite += u.cacheWrite;
      return a;
    },
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
  ),
);
const totalTokens = computed(() =>
  Object.values(totals.value).reduce((a, b) => a + b, 0),
);
const priced = computed(() =>
  records.value.filter(
    (r) => r.cost !== null && r.pricing.currency === currency.value,
  ),
);
const cost = computed(() => priced.value.reduce((a, r) => a + r.cost!, 0));
const legacy = computed(
  () =>
    calls.value.filter(
      (c) => c.modelVerdict && !store.usage.some((u) => u.reviewId === c.id),
    ).length,
);
const missing = computed(() => records.value.length - known.value.length);
const money = (n: number) =>
  `${currency.value === "USD" ? "$" : "¥"}${n.toLocaleString("zh-CN", { minimumFractionDigits: 4, maximumFractionDigits: 6 })}`;
const fmt = (n: number) => n.toLocaleString("zh-CN");
const compact = (n: number) =>
  n >= 1e6
    ? (n / 1e6).toFixed(2) + "M"
    : n >= 10000
      ? (n / 1000).toFixed(1) + "k"
      : fmt(n);
const inputTotal = computed(
  () => totals.value.input + totals.value.cacheRead + totals.value.cacheWrite,
);
const cacheRate = computed(() =>
  inputTotal.value
    ? `${((totals.value.cacheRead / inputTotal.value) * 100).toFixed(1)}%`
    : "—",
);
const points = computed(() =>
  dateKeys.value.map((key) => {
    const cs = calls.value.filter((c) => dayKey(new Date(c.createdAt)) === key),
      us = records.value.filter((u) => dayKey(new Date(u.createdAt)) === key);
    const p: Point = {
      date: key,
      approve: cs.filter((c) => c.decision === "approve").length,
      reject: cs.filter((c) => c.decision === "reject").length,
      pending: cs.filter((c) => c.decision === "pending").length,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
      cost: 0,
    };
    for (const r of us) {
      if (r.usage)
        for (const k of ["input", "output", "cacheRead", "cacheWrite"] as const)
          p[k] = (p[k] as number) + r.usage[k];
      if (r.cost !== null && r.pricing.currency === currency.value)
        p.cost = (p.cost as number) + r.cost;
    }
    const unknown = us.some((r) => !r.usage),
      old = calls.value.some(
        (c) =>
          c.modelVerdict &&
          dayKey(new Date(c.createdAt)) === key &&
          !store.usage.some((u) => u.reviewId === c.id),
      );
    if ((unknown || old) && !us.some((r) => r.usage))
      for (const k of ["input", "output", "cacheRead", "cacheWrite"])
        p[k] = null;
    if (old || us.some((r) => r.cost === null)) p.cost = null;
    return p;
  }),
);
const tokenSeries = [
  { key: "input", label: "输入（未缓存）", color: "#3b82f6" },
  { key: "cacheRead", label: "缓存读取", color: "#20b486" },
  { key: "cacheWrite", label: "缓存写入", color: "#e6a236" },
  { key: "output", label: "输出", color: "#8b5cf6" },
];
const recent = computed(() =>
  calls.value.filter((c) => c.decision === "reject").slice(0, 5),
);
</script>
<template>
  <div class="dashboard">
    <div class="dashboard-toolbar">
      <span class="muted tiny"
        ><i class="dot" :class="{ green: store.streamOnline }" />{{
          store.streamOnline ? "实时更新" : "连接中断 · 显示上次数据"
        }}
        · {{ date(store.updatedAt) }} · 本地时区</span
      >
      <div class="segmented" aria-label="统计时间范围">
        <button
          v-for="n in [7, 30, 90]"
          :key="n"
          :aria-pressed="days === n"
          :class="{ active: days === n }"
          @click="router.replace({ query: { ...route.query, days: n } })"
        >
          {{ n }} 天
        </button>
      </div>
    </div>
    <div class="dashboard-metrics">
      <article class="panel metric-card">
        <span><Icon name="Activity" />工具调用</span
        ><strong>{{ fmt(calls.length) }}</strong
        ><small
          >近 {{ days }} 天 ·
          {{
            calls.filter((c) => c.execution === "succeeded").length
          }}
          次执行成功</small
        >
      </article>
      <article class="panel metric-card">
        <span><Icon name="ShieldX" />已拦截</span
        ><strong class="reject">{{
          fmt(calls.filter((c) => c.decision === "reject").length)
        }}</strong
        ><small
          >{{
            calls.filter((c) => c.decision === "approve").length
          }}
          次已允许</small
        >
      </article>
      <article class="panel metric-card">
        <span><Icon name="Clock3" />当前待审查</span
        ><strong class="pending">{{
          store.calls.filter((c) => c.decision === "pending").length
        }}</strong
        ><small>{{ store.online }} 个在线会话</small>
      </article>
      <article class="panel metric-card">
        <span><Icon name="Bot" />审查 Token</span
        ><strong>{{ known.length ? compact(totalTokens) : "—" }}</strong
        ><small>{{ known.length }} / {{ records.length }} 次请求有用量</small>
      </article>
      <article class="panel metric-card">
        <span><Icon name="Circle" />估算费用 · {{ currency }}</span
        ><strong class="cost-number">{{
          priced.length ? money(cost) : "—"
        }}</strong
        ><small
          >{{ priced.length }} 次已计价 ·
          <router-link to="/settings">配置单价</router-link></small
        >
      </article>
    </div>
    <section class="panel usage-panel">
      <div class="panel-heading">
        <strong><Icon name="Bot" />审查模型 Token 消耗</strong
        ><AppSelect v-model="model" label="统计模型" :options="models" />
      </div>
      <div class="usage-layout">
        <div class="usage-summary">
          <span class="muted">合计（输入 + 输出）</span
          ><strong>{{ known.length ? compact(totalTokens) : "—" }}</strong
          ><small class="muted">含连接测试 · 不含 Agent 自身用量</small>
          <div v-for="s in tokenSeries" :key="s.key" class="token-breakdown">
            <div>
              <span>{{ s.label }}</span
              ><b :style="{ color: s.color }">{{
                fmt(totals[s.key as keyof typeof totals])
              }}</b>
            </div>
            <div class="token-track">
              <i
                :style="{
                  width:
                    (totalTokens
                      ? (totals[s.key as keyof typeof totals] / totalTokens) *
                        100
                      : 0) + '%',
                  background: s.color,
                }"
              />
            </div>
          </div>
          <div class="cache-rate">
            <span>输入缓存命中率</span><b>{{ cacheRate }}</b>
          </div>
        </div>
        <div class="usage-chart">
          <div v-if="!known.length" class="chart-notice">
            暂无可用的 Token 记录，后续模型请求返回 usage 后开始统计。
          </div>
          <TrendChart
            title="审查 Token 趋势"
            :points="points"
            :series="tokenSeries"
            unit="Token"
          />
        </div>
      </div>
      <div class="dashboard-footnote">
        {{ missing }} 次请求用量未知 ·
        {{ legacy }} 次历史审查未采集；未知数据不按 0
        费用计算。用量统计从本次升级后开始。
      </div>
    </section>
    <div class="dashboard-charts">
      <section class="panel">
        <div class="panel-heading">
          <strong><Icon name="Activity" />工具审查趋势</strong
          ><span class="muted tiny">按提交日期 · 当前裁决</span>
        </div>
        <TrendChart
          title="工具审查趋势"
          :points="points"
          :series="[
            { key: 'approve', label: '允许', color: '#20b486' },
            { key: 'reject', label: '拦截', color: '#f05265' },
            { key: 'pending', label: '待审查', color: '#e6a236' },
          ]"
          unit="次"
        />
      </section>
      <section class="panel">
        <div class="panel-heading">
          <strong><Icon name="Activity" />费用趋势</strong
          ><AppSelect
            v-model="currency"
            label="统计币种"
            :options="[
              { value: 'CNY', label: 'CNY 人民币' },
              { value: 'USD', label: 'USD 美元' },
            ]"
          />
        </div>
        <TrendChart
          title="费用趋势"
          :points="points"
          :series="[{ key: 'cost', label: '已计价费用', color: '#8b5cf6' }]"
          kind="line"
          :unit="currency"
        />
        <div class="dashboard-footnote">
          {{
            records.filter((r) => r.cost === null).length
          }}
          次未计价；存在未知费用的日期留空。币种分别统计，不作汇率换算。
        </div>
      </section>
    </div>
    <section class="panel recent-blocks">
      <div class="panel-heading">
        <strong><Icon name="ShieldAlert" />近期拦截</strong
        ><router-link class="text-button" to="/approvals"
          >查看记录<Icon name="ArrowRight" :size="14"
        /></router-link>
      </div>
      <div v-if="!recent.length" class="dashboard-empty">
        所选时间范围内暂无拦截记录
      </div>
      <div v-for="c in recent" :key="c.id" class="recent-block">
        <code class="tool-tag">{{ c.toolName }}</code>
        <div>
          <strong>{{ callTitle(c) }}</strong>
          <p class="muted">{{ c.comment }}</p>
        </div>
        <span class="muted tiny">{{ date(c.createdAt) }}</span
        ><span class="badge reject">{{ decisionLabel(c.decision) }}</span>
      </div>
    </section>
  </div>
</template>
