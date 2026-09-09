import { defineStore } from "pinia";
import { ref, computed } from "vue";
import type {
  UsageRecord,
  AgentInfo,
  Call,
  Instance,
  Installation,
  Rule,
  Scope,
  Settings,
} from "./types";
export async function api<T = any>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const res = await fetch("/api/v1" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await res.json();
  if (!res.ok) {
    if (res.status === 401)
      window.dispatchEvent(new Event("aegis-unauthorized"));
    throw new Error(value.error || "请求失败");
  }
  return value;
}
export const useConsole = defineStore("console", () => {
  const ready = ref(false),
    authenticated = ref(false),
    error = ref(""),
    toast = ref(""),
    loading = ref(false),
    streamOnline = ref(false);
  const calls = ref<Call[]>([]),
    instances = ref<Instance[]>([]),
    installations = ref<Installation[]>([]),
    rules = ref<Rule[]>([]),
    scopes = ref<Scope[]>([]),
    audits = ref<any[]>([]),
    usage = ref<UsageRecord[]>([]),
    updatedAt = ref(""),
    settings = ref<Settings | null>(null),
    pi = ref<any>({}),
    agents = ref<AgentInfo[]>([]);
  const pending = computed(() =>
    calls.value.filter((c) => c.decision === "pending" && c.mode === "human"),
  );
  const online = computed(
    () =>
      instances.value.filter((i) => i.online && i.connectionMode !== "events")
        .length,
  );
  let stream: EventSource | undefined,
    refreshTimer: ReturnType<typeof setTimeout> | undefined;
  async function refresh() {
    if (loading.value) return;
    loading.value = true;
    try {
      const [c, i, inst, r, sc, a, s, u] = await Promise.all([
        api<Call[]>("/calls"),
        api<Instance[]>("/instances"),
        api<Installation[]>("/installations"),
        api<Rule[]>("/rules"),
        api<Scope[]>("/scopes"),
        api<any[]>("/audit"),
        api<Settings>("/settings"),
        api<UsageRecord[]>("/usage"),
      ]);
      calls.value = c;
      instances.value = i;
      installations.value = inst;
      rules.value = r;
      scopes.value = sc;
      audits.value = a;
      settings.value = s;
      usage.value = u;
      updatedAt.value = new Date().toISOString();
      error.value = "";
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      loading.value = false;
    }
  }
  function connect() {
    stream?.close();
    stream = new EventSource("/api/v1/events");
    stream.onopen = () => {
      streamOnline.value = true;
    };
    stream.onerror = () => {
      streamOnline.value = false;
      instances.value = instances.value.map((i) => ({ ...i, online: false }));
      // Refresh also detects an expired admin session after a server restart.
      void refresh();
    };
    stream.addEventListener("update", () => {
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(refresh, 150);
    });
  }
  async function init() {
    try {
      await api("/me");
      authenticated.value = true;
      await refresh();
      connect();
    } catch {
    } finally {
      ready.value = true;
    }
  }
  async function login(token: string) {
    await api("/login", "POST", { token });
    authenticated.value = true;
    await refresh();
    connect();
  }
  async function logout() {
    await api("/logout", "POST", {});
    authenticated.value = false;
    stream?.close();
  }
  function notify(text: string) {
    toast.value = text;
    setTimeout(() => {
      toast.value = "";
    }, 4000);
  }
  async function action(fn: () => Promise<unknown>, message = "已保存") {
    try {
      await fn();
      notify(message);
      await refresh();
      return true;
    } catch (e) {
      error.value = (e as Error).message;
      return false;
    }
  }
  window.addEventListener("aegis-unauthorized", () => {
    authenticated.value = false;
    stream?.close();
  });
  return {
    ready,
    authenticated,
    error,
    toast,
    loading,
    streamOnline,
    calls,
    instances,
    installations,
    rules,
    scopes,
    audits,
    usage,
    updatedAt,
    settings,
    pi,
    agents,
    pending,
    online,
    refresh,
    init,
    login,
    logout,
    notify,
    action,
  };
});
export const date = (s: string | null) =>
  s
    ? new Date(s).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      })
    : "—";
export const decisionLabel = (s: string) =>
  ({ approve: "已允许", reject: "已拒绝", pending: "待审批" })[s] || s;
export const executionLabel = (s: string) =>
  ({
    not_executed: "未执行",
    awaiting_execution: "等待执行结果",
    succeeded: "执行成功",
    failed: "执行失败",
  })[s] || s;
export const callTitle = (c: Call) =>
  ({
    R1: "修改用户密码",
    R2: "修改系统配置",
    R3: "修改账号或权限",
    R4: "删除数据或文件",
    R5: "停止业务服务",
    R6: "拒绝服务操作",
    R7: "漏洞确认后批量取数",
    A7: "读取或查询",
  })[c.ruleId] || c.toolName + " 工具调用";

export const agentName = (id?: string) =>
  ({
    pi: "Pi Agent",
    claude: "Claude Code",
    codex: "Codex",
    opencode: "OpenCode",
    grok: "Grok Build",
  })[id || "pi"] || id;
export const connectionLabel = (i: Instance) =>
  i.connectionMode === "events"
    ? i.state === "disconnected"
      ? "已结束"
      : "事件接入"
    : i.online
      ? "在线"
      : "已断开";
