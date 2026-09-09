<script setup lang="ts">
import Dashboard from "../components/Dashboard.vue";
import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import {
  useConsole,
  agentName,
  connectionLabel,
  isOnlineInstance,
  isRecentEventInstance,
  date,
  callTitle,
  decisionLabel,
  executionLabel,
} from "../store";
import { usePagination } from "../pagination";
import Pagination from "../components/Pagination.vue";
import Icon from "../components/Icon.vue";
import CallDetail from "../components/CallDetail.vue";
const store = useConsole(),
  route = useRoute();
const selected = ref("");
const isSessions = computed(() => route.path === "/sessions");
const sessionCalls = computed(() =>
  store.calls.filter((c) => !selected.value || c.instanceId === selected.value),
);
const selectedInstance = computed(() => store.instances.find((i) => i.id === selected.value));
const details = ref("");
const instancePage = usePagination(() => store.sortedInstances, [() => route.path]);
const timelinePage = usePagination(
  () => sessionCalls.value,
  [selected, () => route.path],
);
</script>
<template>
  <div class="page-heading">
    <div>
      <h1>
        <Icon
          :name="isSessions ? 'MessagesSquare' : 'LayoutDashboard'"
          :size="24"
        />{{ isSessions ? "执行会话" : "执行总览" }}
      </h1>
      <p>审查每一次工具调用，让 Agent 在授权范围内行动。</p>
    </div>
    <router-link class="button primary" to="/agents"
      ><Icon name="Plus" :size="17" />接入 Agent</router-link
    >
  </div>
  <Dashboard v-if="!isSessions" />
  <div class="inline overview-info">
    <span class="badge model"
      ><Icon name="ShieldCheck" :size="14" />{{
        store.settings?.mode === "model" ? "模型审查" : "人工审查"
      }}</span
    ><span class="muted"
      >配置版本 v{{ store.settings?.version }} ·
      {{ store.sessionSummary }}</span
    ><router-link to="/settings" class="text-button"
      >管理审查模式<Icon name="ChevronRight" :size="14"
    /></router-link>
  </div>
  <div v-if="isSessions" class="session-layout">
    <section class="panel session-list">
      <div class="panel-heading">
        <strong>Agent 会话</strong
        ><span class="muted">{{ store.instances.length }}</span>
      </div>
      <button
        v-for="i in instancePage.rows"
        :key="i.id"
        class="session-item"
        :class="{ active: selected === i.id }"
        :aria-pressed="selected === i.id"
        @click="selected = i.id"
      >
        <div class="inline">
          <Icon name="Bot" /><strong>{{ agentName(i.agent) }}</strong
          ><i
            class="dot"
            :class="{ green: isOnlineInstance(i), event: isRecentEventInstance(i) }"
            :title="connectionLabel(i)"
          />
        </div>
        <p class="mono" :title="i.sessionId">{{ i.sessionId.slice(0, 16) }}</p>
        <small :title="i.cwd">项目：{{ i.cwd.split(/[\\/]/).filter(Boolean).pop() || "未提供" }}</small>
        <small>{{ connectionLabel(i) }} · {{ date(i.heartbeat) }}</small>
      </button>
      <div v-if="!store.instances.length" class="empty">
        <Icon name="Unplug" :size="30" /><strong>暂无会话</strong>
        <p>在终端启动已安装 Hook 的 Agent。</p>
      </div>
      <Pagination
        v-model:page="instancePage.page"
        v-model:page-size="instancePage.pageSize"
        :total="instancePage.total"
        label="Agent 会话"
      />
    </section>
    <section class="panel">
      <div class="panel-heading">
        <strong><Icon name="Activity" />执行时间线</strong
        ><button class="text-button" @click="selected = ''">全部会话</button>
      </div>
      <p class="timeline-scope muted tiny">{{ selectedInstance ? agentName(selectedInstance.agent) + " · " + selectedInstance.sessionId.slice(0, 12) : "当前显示全部会话" }}</p>
      <div v-if="!sessionCalls.length" class="empty">
        <Icon name="Workflow" :size="36" /><strong>等待首次工具调用</strong>
        <p>安装 Hook 后重载或重启 Agent，完成客户端的信任确认后开始审查。</p>
        <router-link to="/agents" class="text-button accent"
          >查看接入状态<Icon name="ArrowRight" :size="15"
        /></router-link>
      </div>
      <div v-for="c in timelinePage.rows" :key="c.id" class="timeline-item">
        <div class="timeline-source muted tiny">
          <strong>{{ agentName(c.agent) }}</strong> · <span :title="c.cwd">项目：{{ c.cwd.split(/[\\/]/).filter(Boolean).pop() || "未提供" }}</span> · <span :title="c.sessionId">{{ c.sessionId.slice(0, 12) }}</span>
        </div>
        <div class="timeline-row">
          <i class="timeline-node" :class="c.decision" /><span
            class="muted nowrap"
            >{{ date(c.createdAt) }}</span
          ><strong>{{ callTitle(c) }}</strong
          ><code class="tool-tag">{{ c.toolName }}</code
          ><span class="badge" :class="c.decision">{{
            decisionLabel(c.decision)
          }}</span
          ><span class="muted tiny">{{ executionLabel(c.execution) }}</span
          ><button
            class="icon-button"
            @click="details = details === c.id ? '' : c.id"
            aria-label="查看调用详情"
          >
            <Icon
              :name="details === c.id ? 'ChevronUp' : 'ChevronDown'"
              :size="16"
            />
          </button>
        </div>
        <CallDetail v-if="details === c.id" :call="c" />
      </div>
      <Pagination
        v-model:page="timelinePage.page"
        v-model:page-size="timelinePage.pageSize"
        :total="timelinePage.total"
        label="执行时间线"
      />
    </section>
  </div>
</template>
