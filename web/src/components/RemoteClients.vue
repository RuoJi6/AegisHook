<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api, date, useConsole } from "../store";
const store = useConsole();
const installer = ref<{ shellUrl: string; powershellUrl: string } | null>(null);
const busy = ref("");
const pending = computed(() =>
  store.clientRequests.filter((r) => r.state === "pending"),
);
const recent = computed(() =>
  store.clientRequests.filter((r) => r.state !== "pending").slice(0, 5),
);
const state = (s: string) =>
  ({
    approved: "已批准，等待领取",
    rejected: "已拒绝",
    consumed: "已接入",
    expired: "已过期",
  })[s] || s;
onMounted(async () => {
  try {
    installer.value = await api("/client-installer");
  } catch (e) {
    store.error = (e as Error).message;
  }
});
async function decide(id: string, decision: string) {
  busy.value = id;
  await store.action(
    () => api(`/client-requests/${id}/decision`, "POST", { decision }),
    "接入申请已处理",
  );
  busy.value = "";
}
async function revoke(id: string) {
  busy.value = id;
  await store.action(
    () => api(`/client-nodes/${id}`, "DELETE"),
    "设备凭据已撤销，后续 Hook 请求将被拒绝",
  );
  busy.value = "";
}
const shellCommand = computed(() =>
  installer.value
    ? `curl -fsS '${installer.value.shellUrl}' -o hook.sh\nbash hook.sh install --agent claude --scope global`
    : "",
);
const psCommand = computed(() =>
  installer.value
    ? `Invoke-WebRequest '${installer.value.powershellUrl}' -OutFile hook.ps1\n.\\hook.ps1 -Action install -Agent claude -Scope global`
    : "",
);
async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    store.notify("命令已复制");
  } catch {
    store.error = "无法自动复制，请选中命令手动复制";
  }
}
</script>

<template>
  <section class="panel remote-clients" aria-label="远程设备">
    <div class="panel-heading">
      <strong>远程设备</strong
      ><span class="badge pending" v-if="pending.length"
        >{{ pending.length }} 个待接入</span
      >
    </div>
    <p class="muted">
      在目标机器运行安装脚本，再核对终端与下方申请编号后批准。脚本只安装或卸载
      Hook，不启动后台服务。
    </p>
    <details class="download-details">
      <summary>获取 Windows / macOS / Linux 安装命令</summary>
      <template v-if="installer">
        <div class="command-heading">
          <strong>macOS / Linux</strong
          ><button @click="copy(shellCommand)">复制命令</button>
        </div>
        <pre><code>{{ shellCommand }}</code></pre>
        <div class="command-heading">
          <strong>Windows PowerShell</strong
          ><button @click="copy(psCommand)">复制命令</button>
        </div>
        <pre><code>{{ psCommand }}</code></pre>
        <p class="muted tiny">
          项目安装使用 --scope project --project /绝对路径；加 --switch
          切换全局与项目范围。PowerShell 对应 -Scope project -Project C:\路径
          -SwitchScope。卸载时将 install 换成 uninstall，并保留原来的
          Agent、范围及客户端目录。
        </p>
        <p class="muted tiny">
          通过 HTTPS 地址或 SSH
          隧道接入。安装链接不含凭据；每台获准设备使用独立、可撤销的连接凭据。
        </p>
      </template>
    </details>
    <div class="request-list">
      <p v-if="!pending.length" class="muted">暂无待处理的接入申请。</p>
      <article
        v-for="r in pending"
        :key="r.id"
        class="request-card"
        :data-request-id="r.id"
      >
        <div>
          <strong>{{ r.name }}</strong
          ><span class="muted"> · {{ r.platform }} · {{ r.ip }}</span>
        </div>
        <code class="request-id">{{ r.id }}</code>
        <small class="muted"
          >申请于 {{ date(r.createdAt) }} · {{ date(r.expiresAt) }} 过期</small
        >
        <div class="actions">
          <button
            class="primary"
            :disabled="busy === r.id"
            @click="decide(r.id, 'approve')"
          >
            允许接入</button
          ><button :disabled="busy === r.id" @click="decide(r.id, 'reject')">
            拒绝</button
          ><button
            class="danger"
            :disabled="busy === r.id"
            @click="decide(r.id, 'block_ip')"
          >
            拒绝并拉黑 IP
          </button>
        </div>
      </article>
    </div>
    <div class="table-scroll" v-if="store.clientNodes.length">
      <table class="remote-node-table">
        <thead>
          <tr>
            <th>已接入设备</th>
            <th>系统</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in store.clientNodes" :key="n.id">
            <td>
              <strong>{{ n.name || "未命名设备" }}</strong
              ><small class="mono">{{ n.id }}</small>
            </td>
            <td>{{ n.platform }}</td>
            <td>{{ n.revoked ? "已撤销" : "可接入" }}</td>
            <td>
              <button
                v-if="!n.revoked"
                class="danger"
                :disabled="busy === n.id"
                @click="revoke(n.id)"
              >
                撤销接入
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <details v-if="store.clientBlocks.length">
      <summary>IP 黑名单（{{ store.clientBlocks.length }}）</summary>
      <div v-for="b in store.clientBlocks" :key="b.id" class="blocked-row">
        <code>{{ b.ip }}</code
        ><button
          @click="
            store.action(
              () => api(`/client-ip-blocks/${b.id}`, 'DELETE'),
              '已解除 IP 黑名单',
            )
          "
        >
          解除拉黑
        </button>
      </div>
      <p class="muted tiny">
        同一出口 IP 可能包含多台设备；解除拉黑不会恢复已撤销的设备凭据。
      </p>
    </details>
    <details v-if="recent.length">
      <summary>最近处理的申请</summary>
      <p v-for="r in recent" :key="r.id" class="muted">
        {{ r.name }} · {{ r.ip }} · {{ state(r.state) }}
      </p>
    </details>
  </section>
</template>

<style scoped>
.remote-clients {
  margin-bottom: 24px;
}
.remote-clients > p,
.remote-clients > details,
.request-list {
  margin: 16px;
}
summary {
  cursor: pointer;
  padding: 10px 0;
  font-weight: 600;
}
pre {
  padding: 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--border) 45%, var(--soft));
  color: var(--text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.command-heading,
.blocked-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 12px 0;
}
.request-card {
  border: 1px solid var(--border, #e3e5eb);
  border-radius: 10px;
  padding: 16px;
  margin-top: 12px;
}
.request-id {
  display: block;
  overflow-wrap: anywhere;
  margin: 10px 0;
}
.request-card .actions {
  margin-top: 14px;
  flex-wrap: wrap;
}
small {
  display: block;
}
@media (max-width: 650px) {
  .remote-node-table {
    min-width: 0;
  }
  .remote-node-table thead {
    display: none;
  }
  .remote-node-table tr {
    display: grid;
    grid-template-columns: 1fr auto;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border, #e3e5eb);
  }
  .remote-node-table td {
    border: 0;
    padding: 4px 0;
    overflow-wrap: anywhere;
    white-space: normal;
  }
  .remote-node-table td:first-child {
    grid-column: 1 / -1;
  }
  .remote-node-table td:last-child {
    grid-column: 1 / -1;
  }
}
</style>
