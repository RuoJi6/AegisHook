<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import {
  api,
  useConsole,
  date,
  decisionLabel,
  callTitle,
  agentName,
} from "../store";
import type { Call } from "../types";
import { usePagination } from "../pagination";
import Pagination from "../components/Pagination.vue";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import CallDetail from "../components/CallDetail.vue";
const store = useConsole(),
  route = useRoute();
const tab = ref("all"),
  search = ref(""),
  expanded = ref(""),
  pendingExpanded = ref(""),
  busy = ref(""),
  rejecting = ref<Call | null>(null),
  reason = ref("");
const isCalls = computed(() => route.path === "/calls");
const filtered = computed(() =>
  store.calls.filter(
    (c) =>
      (tab.value === "all" || c.decision === tab.value) &&
      JSON.stringify(c)
        .toLowerCase()
        .includes((search.value || String(route.query.q || "")).toLowerCase()),
  ),
);
const recordsPage = usePagination(
  () => filtered.value,
  [tab, search, () => route.query.q, () => route.path],
);
const pendingPage = usePagination(() => store.pending, [() => route.path]);
const tabs = [
  ["all", "全部记录"],
  ["reject", "已拒绝"],
  ["approve", "已允许"],
  ["pending", "待审批"],
];
async function decide(c: Call, d: string) {
  busy.value = c.id;
  const ok = await store.action(
    () =>
      api(`/approvals/${c.id}/decision`, "POST", {
        digest: c.digest,
        decision: d,
        comment: d === "reject" ? reason.value : "",
      }),
    d === "approve" ? "已允许本次调用" : "已拒绝，原因已进入 Agent 返回通道",
  );
  if (ok) {
    rejecting.value = null;
    reason.value = "";
  }
  busy.value = "";
}
</script>
<template>
  <div class="page-heading">
    <h1>
      <Icon :name="isCalls ? 'Workflow' : 'ClipboardList'" :size="23" />{{
        isCalls ? "工具调用" : "审批与拦截"
      }}<span v-if="store.pending.length" class="pending-count"
        >{{ store.pending.length }} 待审批</span
      >
    </h1>
    <button @click="store.refresh" :disabled="store.loading">
      <Icon name="RefreshCw" :size="16" />刷新
    </button>
  </div>
  <section v-if="!isCalls" class="panel pending-panel">
    <div class="panel-heading">
      <strong
        ><Icon name="ShieldAlert" :size="17" />待处理 ({{
          store.pending.length
        }})</strong
      >
    </div>
    <div class="table-scroll">
      <table class="review-table pending-table">
        <colgroup>
          <col class="col-id" />
          <col class="col-tool" />
          <col class="col-source" />
          <col class="col-rule" />
          <col class="col-args" />
          <col class="col-time" />
          <col class="col-pending-actions" />
        </colgroup>
        <thead>
          <tr>
            <th>#</th>
            <th>工具</th>
            <th>来源</th>
            <th>匹配规则</th>
            <th>参数</th>
            <th>时间</th>
            <th class="align-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in pendingPage.rows" :key="c.id">
            <tr>
              <td class="muted record-id" :title="c.id">
                {{ c.id.slice(0, 6) }}
              </td>
              <td>
                <code class="tool-tag">{{ c.toolName }}</code>
              </td>
              <td class="source-cell">
                <strong>{{ agentName(c.agent) }}</strong
                ><small :title="c.cwd">项目：{{
                  c.cwd.split(/[\\/]/).filter(Boolean).pop() || "未提供"
                }}</small
                ><small :title="c.sessionId">{{
                  c.sessionId.slice(0, 12)
                }}</small>
              </td>
              <td class="reason-cell">
                <span class="badge model"
                  ><Icon
                    :name="c.mode === 'model' ? 'Bot' : 'UserRound'"
                    :size="12"
                  />人工审查</span
                ><small :title="c.comment">{{
                  c.comment || "未命中明确规则，等待审批"
                }}</small>
              </td>
              <td>
                <button
                  class="parameter-preview"
                  :title="JSON.stringify(c.argumentsObj)"
                  @click="
                    pendingExpanded = pendingExpanded === c.id ? '' : c.id
                  "
                  aria-label="查看待审批详情"
                >
                  {{ JSON.stringify(c.argumentsObj) }}
                </button>
              </td>
              <td class="muted nowrap">{{ date(c.createdAt) }}</td>
              <td>
                <div class="actions row-actions">
                  <button
                    class="primary"
                    :disabled="!!busy"
                    @click="decide(c, 'approve')"
                  >
                    <Icon name="Check" :size="15" />允许本次</button
                  ><button
                    class="danger-button"
                    :disabled="!!busy"
                    @click="rejecting = c"
                  >
                    <Icon name="X" :size="15" />拒绝
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="pendingExpanded === c.id" class="expanded-row">
              <td colspan="7"><CallDetail :call="c" /></td>
            </tr>
          </template>
          <tr v-if="!store.pending.length">
            <td colspan="7" class="empty-cell">暂无待审批操作</td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-model:page="pendingPage.page"
      v-model:page-size="pendingPage.pageSize"
      :total="pendingPage.total"
      label="待处理"
    />
  </section>
  <section class="panel records">
    <div class="records-heading">
      <h2>全部记录 ({{ store.calls.length }})</h2>
      <div class="record-filters">
        <div class="search-input">
          <Icon name="Search" :size="15" /><input
            v-model="search"
            placeholder="搜索操作或规则"
            aria-label="搜索操作或规则"
          />
        </div>
        <AppSelect
          v-model="tab"
          label="筛选裁决"
          :options="tabs.map((t) => ({ value: t[0]!, label: t[1]! }))"
        />
        <a
          class="icon-button"
          href="/api/v1/calls/export"
          download
          aria-label="导出记录"
          title="导出记录"
          ><Icon name="Download" :size="17"
        /></a>
      </div>
    </div>
    <div class="table-scroll">
      <table class="review-table records-table">
        <colgroup>
          <col class="col-id" />
          <col class="col-tool" />
          <col class="col-source" />
          <col class="col-rule" />
          <col class="col-args" />
          <col class="col-state" />
          <col class="col-time" />
          <col class="col-time" />
          <col class="col-details" />
        </colgroup>
        <thead>
          <tr>
            <th>#</th>
            <th>工具</th>
            <th>来源</th>
            <th>匹配规则</th>
            <th>参数</th>
            <th>状态</th>
            <th>申请时间</th>
            <th>决定时间</th>
            <th><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in recordsPage.rows" :key="c.id">
            <tr :class="{ selected: expanded === c.id }">
              <td class="muted record-id" :title="c.id">
                {{ c.id.slice(0, 6) }}
              </td>
              <td>
                <code class="tool-tag" :title="callTitle(c)">{{
                  c.toolName
                }}</code>
              </td>
              <td class="source-cell">
                <strong>{{ agentName(c.agent) }}</strong
                ><small :title="c.cwd">项目：{{
                  c.cwd.split(/[\\/]/).filter(Boolean).pop() || "未提供"
                }}</small
                ><small :title="c.sessionId">{{
                  c.sessionId.slice(0, 12)
                }}</small>
              </td>
              <td class="reason-cell">
                <span
                  v-if="
                    c.mode === 'model' &&
                    c.ruleId !== 'HUMAN' &&
                    (c.modelVerdict ||
                      c.ruleId === 'D1' ||
                      c.ruleId === 'E_MODEL' ||
                      !c.ruleId)
                  "
                  class="badge model"
                  ><Icon name="Bot" :size="12" />{{ "模型判定" }}</span
                ><strong v-else>{{
                  store.rules.find((r) => r.id === c.ruleId)?.name ||
                  (c.ruleId === "HUMAN" ? "人工审批" : c.ruleId) ||
                  "规则未定"
                }}</strong
                ><small :title="c.comment">{{
                  c.comment ||
                  (c.mode === "model" ? "模型审查中" : "等待人工决策")
                }}</small>
              </td>
              <td>
                <button
                  class="parameter-preview"
                  :title="JSON.stringify(c.argumentsObj)"
                  @click="expanded = expanded === c.id ? '' : c.id"
                  aria-label="查看参数"
                >
                  {{ JSON.stringify(c.argumentsObj) }}
                </button>
              </td>
              <td>
                <span class="badge" :class="c.decision">{{
                  c.decision === "pending" && c.mode === "model"
                    ? "模型审查中"
                    : decisionLabel(c.decision)
                }}</span>
              </td>
              <td class="muted nowrap">{{ date(c.createdAt) }}</td>
              <td class="muted nowrap">{{ date(c.decidedAt) }}</td>
              <td>
                <button
                  class="icon-button detail-toggle"
                  @click="expanded = expanded === c.id ? '' : c.id"
                  :aria-label="expanded === c.id ? '收起详情' : '查看详情'"
                >
                  <Icon
                    :name="expanded === c.id ? 'ChevronUp' : 'ChevronRight'"
                    :size="15"
                  />
                </button>
              </td>
            </tr>
            <tr v-if="expanded === c.id" class="expanded-row">
              <td colspan="9"><CallDetail :call="c" /></td>
            </tr>
          </template>
          <tr v-if="!recordsPage.rows.length">
            <td colspan="9" class="empty-cell">
              {{
                store.calls.length ? "没有符合条件的记录" : "暂无工具调用记录"
              }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-model:page="recordsPage.page"
      v-model:page-size="recordsPage.pageSize"
      :total="recordsPage.total"
      :label="isCalls ? '工具调用' : '审批记录'"
    />
  </section>
  <div v-if="rejecting" class="modal-backdrop" @click.self="rejecting = null">
    <form class="modal" @submit.prevent="decide(rejecting!, 'reject')">
      <div class="panel-heading">
        <h2>拒绝本次调用</h2>
        <button
          type="button"
          class="icon-button"
          @click="rejecting = null"
          aria-label="关闭"
        >
          <Icon name="X" />
        </button>
      </div>
      <p class="muted">原因将返回 Agent，帮助它调整后续行动。</p>
      <label
        >拒绝原因<textarea
          v-model="reason"
          required
          rows="4"
          placeholder="说明此操作为什么不允许，以及可行的替代方式"
        />
      </label>
      <div class="actions end">
        <button type="button" @click="rejecting = null">取消</button
        ><button class="danger-button" :disabled="!!busy">确认拒绝</button>
      </div>
    </form>
  </div>
</template>
