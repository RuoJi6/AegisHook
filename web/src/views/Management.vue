<script setup lang="ts">
import { computed, ref, watch, onMounted } from "vue";
import { useRoute } from "vue-router";
import {
  api,
  useConsole,
  date,
  agentName,
  connectionLabel,
  instanceStatusClass,
} from "../store";
import type { Rule, Scope } from "../types";
import { usePagination } from "../pagination";
import Pagination from "../components/Pagination.vue";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import RuleSemantics from "../components/RuleSemantics.vue";
import RemoteClients from "../components/RemoteClients.vue";
import RulesTable from "../components/RulesTable.vue";
import RuleSwitch from "../components/RuleSwitch.vue";
const semanticDefaults = ref<Rule[]>([]);
const formSemantics = computed(
  () =>
    ruleForm.value?.semantics ||
    semanticDefaults.value.find((r) => r.id === ruleForm.value?.id)?.semantics,
);
onMounted(async () => {
  try {
    semanticDefaults.value = await api("/rules/defaults");
  } catch (e) {
    store.error = (e as Error).message;
  }
});
const store = useConsole(),
  route = useRoute();
const kind = computed(() => route.path.slice(1));
const deviceLabel = (id?: string) =>
  id ? store.clientNodes.find((n) => n.id === id)?.name || id : "控制台本机";
const headings: Record<string, string> = {
  agents: "Agent 接入",
  rules: "策略规则",
  scopes: "授权范围",
  audit: "审计日志",
};
const sub: Record<string, string> = {
  agents: "管理各 Agent Hook 的安装配置、接入验证与当前会话状态。",
  rules: "内置与自定义规则均可编辑；拒绝优先，同类按优先级从高到低匹配。",
  scopes: "为关联项目设置明确的文件路径与目标范围。",
  audit: "追溯每次配置变更、审查裁决和 Hook 生命周期操作。",
};
const selectedAgent = ref("pi");
const currentAgent = computed(() =>
  store.agents.find((a) => a.id === selectedAgent.value),
);
const agentOptions = computed(() =>
  store.agents.map((a) => ({ value: a.id, label: a.name })),
);
const installScope = ref("global"),
  project = ref(""),
  busy = ref(false),
  showInstall = ref(false),
  removeId = ref(""),
  filter = ref("");
const ruleForm = ref<Rule | null>(null),
  scopeForm = ref<Scope | null>(null),
  targets = ref(""),
  paths = ref("");
const testTool = ref("bash"),
  testArgs = ref('{"command":"whoami"}'),
  testResult = ref<any>(null);
const configStatuses: Record<string, string> = {
  configured: "已配置",
  uninstalled: "已卸载",
  entry_error: "入口异常",
};
async function detect() {
  try {
    store.agents = await api("/agents");
  } catch (e) {
    store.error = (e as Error).message;
  }
}
onMounted(detect);
watch(kind, () => {
  filter.value = "";
  ruleForm.value = null;
  scopeForm.value = null;
});
async function install() {
  busy.value = true;
  const ok = await store.action(
    () =>
      api("/installations", "POST", {
        agent: selectedAgent.value,
        scope: installScope.value,
        project: project.value,
      }),
    "安装入口已创建。" +
      (currentAgent.value?.instructions || "请重新启动客户端。"),
  );
  if (ok) showInstall.value = false;
  busy.value = false;
}
async function uninstall() {
  const ok = await store.action(
    () => api("/installations/" + removeId.value, "DELETE"),
    "安装入口已移除；已加载会话需重载或退出",
  );
  if (ok) removeId.value = "";
}
function newRule() {
  ruleForm.value = {
    id: "U_" + Math.random().toString(36).slice(2, 8).toUpperCase(),
    name: "",
    decision: "reject",
    enabled: true,
    builtin: false,
    tool: "",
    field: "",
    pattern: "",
    matcher: "regex",
    target: "arguments",
    priority: 0,
    message: "",
  };
}
async function restoreRule() {
  const id = ruleForm.value?.id;
  await store.action(async () => {
    const defaults: Rule[] = await api("/rules/defaults");
    const preset = defaults.find((r) => r.id === id);
    if (preset) ruleForm.value = { ...preset };
  }, "已填入默认值，保存后生效");
}
watch(
  () => ruleForm.value?.matcher,
  (matcher) => {
    if (matcher === "semantic" && ruleForm.value) {
      ruleForm.value.field = "";
      ruleForm.value.pattern = "";
      ruleForm.value.target = "arguments";
    }
  },
);
async function saveRule() {
  if (
    await store.action(
      () => api("/rules", "POST", ruleForm.value),
      "规则已保存，对后续调用生效",
    )
  )
    ruleForm.value = null;
}
async function tryRule() {
  try {
    testResult.value = await api("/rules/test", "POST", {
      toolName: testTool.value,
      argumentsObj: JSON.parse(testArgs.value),
    });
  } catch (e) {
    store.error = (e as Error).message;
  }
}
function editScope(s?: Scope) {
  scopeForm.value = s
    ? JSON.parse(JSON.stringify(s))
    : { id: "", name: "", project: "", targets: [], paths: [] };
  targets.value = s?.targets.join("\n") || "";
  paths.value = s?.paths.join("\n") || "";
}
async function saveScope() {
  if (!scopeForm.value) return;
  scopeForm.value.platform =
    store.clientNodes.find((n) => n.id === scopeForm.value?.nodeId)?.platform ||
    "";
  scopeForm.value.targets = targets.value
    .split("\n")
    .map((x) => x.trim().toLowerCase())
    .filter(Boolean);
  scopeForm.value.paths = paths.value
    .split("\n")
    .map((x) => x.trim())
    .filter(Boolean);
  if (await store.action(() => api("/scopes", "POST", scopeForm.value)))
    scopeForm.value = null;
}
const auditRows = computed(() =>
  store.audits.filter((a) =>
    JSON.stringify(a).toLowerCase().includes(filter.value.toLowerCase()),
  ),
);
const installationPage = usePagination(() => store.installations, [kind]);
const instancePage = usePagination(() => store.sortedInstances, [kind]);
const scopePage = usePagination(() => store.scopes, [kind]);
const auditPage = usePagination(() => auditRows.value, [kind, filter]);
</script>
<template>
  <div class="page-heading">
    <div>
      <h1>
        <Icon
          :name="
            kind === 'agents'
              ? 'Bot'
              : kind === 'rules'
                ? 'ClipboardList'
                : kind === 'scopes'
                  ? 'ScanFace'
                  : 'ScrollText'
          "
          :size="24"
        />{{ headings[kind] }}
      </h1>
      <p>{{ sub[kind] }}</p>
    </div>
    <div class="actions">
      <button
        @click="
          store.refresh();
          detect();
        "
      >
        <Icon name="RefreshCw" :size="16" />刷新</button
      ><button
        v-if="kind === 'agents'"
        class="primary"
        @click="showInstall = true"
      >
        <Icon name="Plus" :size="17" />安装 Hook</button
      ><button v-if="kind === 'rules'" class="primary" @click="newRule">
        <Icon name="Plus" :size="17" />新建规则</button
      ><button v-if="kind === 'scopes'" class="primary" @click="editScope()">
        <Icon name="Plus" :size="17" />添加范围
      </button>
    </div>
  </div>
  <template v-if="kind === 'agents'"
    ><RemoteClients />
    <section class="panel connection-card">
      <div class="agent-emblem"><Icon name="Bot" :size="32" /></div>
      <div class="agent-summary">
        <div class="inline">
          <h2>{{ currentAgent?.name || "Agent" }}</h2>
          <span
            class="badge"
            :class="currentAgent?.found ? 'approve' : 'pending'"
            >{{ currentAgent?.found ? "已检测到" : "未检测到命令" }}</span
          >
        </div>
        <p class="muted">
          {{ currentAgent?.version || "版本未获取" }} ·
          {{ currentAgent?.platform || "本机" }} ·
          {{
            currentAgent?.integration === "command"
              ? "命令 Hook"
              : currentAgent?.integration === "plugin"
                ? "插件"
                : "Extension"
          }}
        </p>
        <code v-if="currentAgent?.path">{{ currentAgent.path }}</code>
      </div>
      <AppSelect
        v-model="selectedAgent"
        label="选择 Agent"
        :options="agentOptions"
      />
    </section>
    <div class="notice">
      <Icon name="Info" />
      <div>
        <p>{{ currentAgent?.instructions }}</p>
        <p class="muted tiny">{{ currentAgent?.limitations }}</p>
      </div>
    </div>
    <section class="panel">
      <div class="panel-heading">
        <strong>安装范围</strong
        ><span class="muted"
          >当前用户或指定项目 · 历史接入验证不随会话结束清除</span
        >
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Agent / 安装范围</th>
              <th>入口路径</th>
              <th>安装状态</th>
              <th>接入验证</th>
              <th>版本</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in installationPage.rows" :key="i.id">
              <td>
                <small>{{ agentName(i.agent) }}</small
                ><strong>{{
                  i.scope === "global" ? "当前用户全局" : "指定项目"
                }}</strong
                ><small>{{ i.project || "当前用户配置目录" }}</small>
              </td>
              <td class="path-text mono">{{ i.entry }}</td>
              <td>
                <span
                  class="badge"
                  :class="
                    i.configStatus === 'configured'
                      ? 'approve'
                      : i.configStatus === 'entry_error'
                        ? 'reject'
                        : ''
                  "
                  >{{ configStatuses[i.configStatus] }}</span
                >
                <small v-if="i.status === 'waiting_reload'"
                  >已有会话仍需重载</small
                >
              </td>
              <td>
                <span class="badge" :class="i.observed ? 'approve' : ''">
                  {{ i.observed ? "已收到过事件" : "尚未收到事件" }}
                </span>
                <small>历史记录，不代表当前会话在线</small>
              </td>
              <td>{{ i.version }}</td>
              <td>
                <button
                  v-if="i.installed"
                  class="text-button reject"
                  @click="removeId = i.id"
                >
                  卸载</button
                ><span v-else class="muted">入口已移除</span>
              </td>
            </tr>
            <tr v-if="!store.installations.length">
              <td colspan="6">
                <div class="empty">
                  <Icon name="Plug" :size="32" /><strong
                    >尚未安装 AegisHook</strong
                  >
                  <p>选择全局或项目范围，给所选 Agent 接入执行审查。</p>
                  <button @click="showInstall = true">安装 Hook</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="installationPage.page"
        v-model:page-size="installationPage.pageSize"
        :total="installationPage.total"
        label="安装范围"
      />
    </section>
    <section class="panel section-gap">
      <div class="panel-heading">
        <strong>会话实际状态</strong
        ><span class="muted"
          >Pi：10 秒心跳 / 30 秒失联；其他 Agent：按事件记录</span
        >
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>会话</th>
              <th>工作目录</th>
              <th>连接</th>
              <th>最近心跳 / 事件</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in instancePage.rows" :key="i.id">
              <td>
                <strong>{{ agentName(i.agent) }}</strong
                ><small class="mono">{{ i.sessionId.slice(0, 16) }}</small>
              </td>
              <td class="path-text">
                {{ i.cwd }}<small>{{ deviceLabel(i.nodeId) }}</small>
              </td>
              <td>
                <span class="badge" :class="instanceStatusClass(i)">{{
                  connectionLabel(i)
                }}</span>
              </td>
              <td>{{ date(i.heartbeat) }}</td>
            </tr>
            <tr v-if="!store.instances.length">
              <td colspan="4" class="empty-cell">
                尚未收到 Agent 会话事件。文件已安装不代表会话已加载。
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="instancePage.page"
        v-model:page-size="instancePage.pageSize"
        :total="instancePage.total"
        label="会话状态"
      />
    </section>
    <p class="muted tiny">
      审查范围以客户端提供的工具事件为准，不逐项拦截脚本内部行为。事件接入表示会话曾上报事件；最近活跃表示
      30
      秒内收到事件或审查心跳，不代表持续在线。安装范围的接入验证保留历史证据，会话结束、失联或服务重启不会清除。
    </p></template
  >
  <template v-if="kind === 'rules'">
    <RulesTable
      :rules="store.rules"
      @edit="ruleForm = JSON.parse(JSON.stringify($event))"
    />
    <section class="panel form-panel section-gap">
      <h2>规则试判</h2>
      <p class="muted">只检查确定性规则，不执行工具，也不调用模型。</p>
      <div class="form-two">
        <label>工具名称<input v-model="testTool" /></label
        ><label
          >参数 JSON<textarea v-model="testArgs" class="mono" rows="3" />
        </label>
      </div>
      <button @click="tryRule">
        <Icon name="FlaskConical" :size="16" />运行试判
      </button>
      <div v-if="testResult" class="notice">
        <span class="badge" :class="testResult.decision">{{
          testResult.decision
        }}</span>
        <p>{{ testResult.comment }}</p>
      </div>
    </section></template
  >
  <template v-if="kind === 'scopes'"
    ><div class="notice">
      <Icon name="Info" />
      <p>
        配置后，对关联项目中可识别的工具 path、url、host、target
        字段检查范围。空列表不增加该维度限制；这不是网络沙箱。
      </p>
    </div>
    <section class="panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>名称 / 项目</th>
              <th>允许目标</th>
              <th>允许路径</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in scopePage.rows" :key="s.id">
              <td>
                <strong>{{ s.name }}</strong
                ><small>{{ deviceLabel(s.nodeId) }} · {{ s.project }}</small>
              </td>
              <td>{{ s.targets.join(", ") || "不额外限制" }}</td>
              <td class="path-text">
                {{ s.paths.join(", ") || "不额外限制" }}
              </td>
              <td>
                <div class="actions">
                  <button class="text-button" @click="editScope(s)">编辑</button
                  ><button
                    class="text-button reject"
                    @click="
                      store.action(
                        () => api('/scopes/' + s.id, 'DELETE'),
                        '范围已删除',
                      )
                    "
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!store.scopes.length">
              <td colspan="4">
                <div class="empty">
                  <Icon name="ScanFace" :size="32" /><strong
                    >尚未添加授权范围</strong
                  >
                  <p>可为项目配置精确目标主机和允许访问的路径。</p>
                  <button @click="editScope()">添加范围</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="scopePage.page"
        v-model:page-size="scopePage.pageSize"
        :total="scopePage.total"
        label="授权范围"
      /></section
  ></template>
  <template v-if="kind === 'audit'"
    ><div class="audit-search search-input">
      <Icon name="Search" :size="16" /><input
        v-model="filter"
        placeholder="搜索操作、对象或详情"
      />
    </div>
    <section class="panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>操作</th>
              <th>对象</th>
              <th>详情</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in auditPage.rows" :key="a.id">
              <td class="nowrap muted">{{ date(a.createdAt) }}</td>
              <td>
                <code>{{ a.action }}</code>
              </td>
              <td class="mono muted">{{ a.subject.slice(0, 16) }}</td>
              <td class="audit-detail">{{ a.detail }}</td>
            </tr>
            <tr v-if="!auditRows.length">
              <td colspan="4" class="empty-cell">暂无匹配的审计记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="auditPage.page"
        v-model:page-size="auditPage.pageSize"
        :total="auditPage.total"
        label="审计日志"
      /></section
  ></template>
  <div
    v-if="showInstall"
    class="modal-backdrop"
    @click.self="showInstall = false"
  >
    <form class="modal" @submit.prevent="install">
      <div class="panel-heading">
        <h2>安装 {{ currentAgent?.name || "Agent" }} Hook</h2>
        <button
          type="button"
          class="icon-button"
          @click="showInstall = false"
          aria-label="关闭"
        >
          <Icon name="X" />
        </button>
      </div>
      <label
        >Agent<AppSelect
          v-model="selectedAgent"
          label="安装 Agent"
          :options="agentOptions"
      /></label>
      <p class="muted tiny">{{ currentAgent?.instructions }}</p>
      <label
        >安装范围<AppSelect
          v-model="installScope"
          label="安装范围"
          :options="[
            { value: 'global', label: '当前用户全局' },
            { value: 'project', label: '指定项目' },
          ]" /></label
      ><label v-if="installScope === 'project'"
        >项目绝对路径<input
          v-model="project"
          required
          placeholder="/absolute/path/to/project"
      /></label>
      <p class="muted">
        只创建 AegisHook 自有入口，不修改其它扩展和项目信任配置。
      </p>
      <div class="actions end">
        <button type="button" @click="showInstall = false">取消</button
        ><button class="primary" :disabled="busy">安装 Hook</button>
      </div>
    </form>
  </div>
  <div v-if="removeId" class="modal-backdrop" @click.self="removeId = ''">
    <div class="modal">
      <h2>卸载此安装入口</h2>
      <p>
        其它安装范围保持有效。客户端可能缓存已加载的
        Hook，移除后请重载或重启客户端；现有连接配置会保留。
      </p>
      <div class="actions end">
        <button @click="removeId = ''">取消</button
        ><button class="danger-button" @click="uninstall">移除入口</button>
      </div>
    </div>
  </div>
  <div v-if="ruleForm" class="modal-backdrop" @click.self="ruleForm = null">
    <form class="modal" @submit.prevent="saveRule">
      <h2>{{ ruleForm.builtin ? "编辑内置规则" : "自定义规则" }}</h2>
      <p class="muted tiny">
        保存后对新调用生效。未匹配调用进入当前审查模式，AI
        提示词在系统设置中单独管理。
      </p>
      <div class="form-two">
        <label
          >规则编号<input
            v-model="ruleForm.id"
            required
            :readonly="ruleForm.builtin"
            :pattern="
              ruleForm.builtin ? undefined : 'U_[A-Za-z0-9_-]+'
            " /></label
        ><label>名称<input v-model="ruleForm.name" required /></label>
      </div>
      <div class="form-two">
        <label
          >裁决<AppSelect
            v-model="ruleForm.decision"
            label="裁决"
            :options="[
              { value: 'reject', label: '拒绝' },
              { value: 'approve', label: '允许' },
            ]" /></label
        ><label
          >工具名<input v-model="ruleForm.tool" placeholder="留空匹配全部"
        /></label>
      </div>
      <div class="form-two">
        <label
          >匹配方式<AppSelect
            v-model="ruleForm.matcher"
            label="匹配方式"
            :options="[
              ...(ruleForm.builtin
                ? [{ value: 'semantic', label: '内置语义识别' }]
                : []),
              { value: 'regex', label: '正则表达式' },
              { value: 'contains', label: '包含文本' },
            ]"
        /></label>
        <label
          >优先级<input
            type="number"
            step="1"
            v-model.number="ruleForm.priority"
            required
        /></label>
      </div>
      <div v-if="ruleForm.matcher === 'semantic'">
        <RuleSemantics
          v-if="formSemantics"
          :semantics="formSemantics"
          :tool="ruleForm.tool"
        />
        <p class="muted tiny">
          需要替换检测条件时，可切换为正则表达式或包含文本；补充说明只影响返回消息。
        </p>
      </div>
      <template v-else>
        <label
          >匹配对象<AppSelect
            v-model="ruleForm.target"
            label="匹配对象"
            :options="[
              { value: 'arguments', label: '工具参数' },
              { value: 'toolName', label: '工具名称' },
            ]"
        /></label>
        <label v-if="ruleForm.target === 'arguments'"
          >参数路径<input
            v-model="ruleForm.field"
            placeholder="例如 command 或 request.method；留空匹配完整参数"
        /></label>
        <label
          >{{
            ruleForm.matcher === "regex"
              ? "匹配表达式（Go 正则）"
              : "匹配文本（区分大小写）"
          }}<textarea v-model="ruleForm.pattern" required rows="3" />
        </label>
      </template>
      <label
        >裁决补充说明<textarea
          v-model="ruleForm.message"
          rows="2"
          maxlength="4000"
          placeholder="匹配时返回 Agent 的说明，可留空"
        />
      </label>
      <div class="inline">
        <RuleSwitch v-model="ruleForm.enabled" label="启用当前规则" /><span
          >启用规则</span
        >
      </div>
      <div class="actions end">
        <button v-if="ruleForm.builtin" type="button" @click="restoreRule">
          恢复默认
        </button>
        <button type="button" @click="ruleForm = null">取消</button
        ><button class="primary">保存规则</button>
      </div>
    </form>
  </div>
  <div v-if="scopeForm" class="modal-backdrop" @click.self="scopeForm = null">
    <form class="modal" @submit.prevent="saveScope">
      <h2>授权范围</h2>
      <label
        >所属设备<AppSelect
          :model-value="scopeForm.nodeId || ''"
          @update:model-value="scopeForm.nodeId = $event"
          label="所属设备"
          :options="[
            { value: '', label: '控制台本机' },
            ...store.clientNodes
              .filter((n) => !n.revoked)
              .map((n) => ({
                value: n.id,
                label: `${n.name || n.id} (${n.platform})`,
              })),
          ]"
      /></label>
      <label>名称<input v-model="scopeForm.name" required /></label
      ><label>项目绝对路径<input v-model="scopeForm.project" required /></label
      ><label
        >允许目标（每行一个精确主机名或 IP）<textarea
          v-model="targets"
          rows="3"
          placeholder="staging.example.test"
        /></label
      ><label
        >允许文件路径（每行一个绝对路径）<textarea v-model="paths" rows="3" />
      </label>
      <div class="actions end">
        <button type="button" @click="scopeForm = null">取消</button
        ><button class="primary">保存范围</button>
      </div>
    </form>
  </div>
</template>
