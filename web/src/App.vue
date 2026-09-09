<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useConsole } from "./store";
import Icon from "./components/Icon.vue";
const store = useConsole(),
  router = useRouter();
const token = ref(""),
  loginError = ref(""),
  logging = ref(false),
  collapsed = ref(false),
  search = ref("");
const theme = ref(localStorage.getItem("aegis-theme") || "light");
function applyTheme() {
  document.documentElement.dataset.theme = theme.value;
  localStorage.setItem("aegis-theme", theme.value);
}
function toggleTheme() {
  theme.value = theme.value === "light" ? "dark" : "light";
  applyTheme();
}
const groups = [
  {
    name: "工作台",
    items: [
      ["/overview", "总览", "LayoutDashboard"],
      ["/sessions", "执行会话", "MessagesSquare"],
      ["/calls", "工具调用", "Workflow"],
    ],
  },
  {
    name: "安全管理",
    items: [
      ["/rules", "策略规则", "ClipboardList"],
      ["/approvals", "审批与拦截", "ShieldCheck"],
      ["/scopes", "授权范围", "ScanFace"],
      ["/audit", "审计日志", "ScrollText"],
    ],
  },
  {
    name: "系统",
    items: [
      ["/agents", "Agent 接入", "Bot"],
      ["/settings", "系统设置", "Settings"],
    ],
  },
];
async function login() {
  logging.value = true;
  try {
    await store.login(token.value);
    token.value = "";
    loginError.value = "";
  } catch (e) {
    loginError.value = (e as Error).message;
  } finally {
    logging.value = false;
  }
}
function doSearch() {
  router.push({ path: "/calls", query: { q: search.value } });
}
onMounted(() => {
  applyTheme();
  store.init();
});
</script>
<template>
  <div v-if="!store.ready" class="loading-page">
    <Icon name="ShieldCheck" :size="40" />
    <p>正在连接本机控制台…</p>
  </div>
  <div v-else-if="!store.authenticated" class="login-page">
    <form class="login-panel" @submit.prevent="login">
      <div class="brand large">
        <Icon name="ShieldCheck" :size="38" /><span
          >AegisHook<small>Agent 执行审查控制台</small></span
        >
      </div>
      <h1>连接你的本机控制台</h1>
      <p class="muted">
        使用启动终端中提示的 <code>admin.token</code> 文件内的管理令牌登录。
      </p>
      <label
        >管理令牌<input
          v-model="token"
          type="password"
          autocomplete="off"
          required
          placeholder="输入本机管理令牌"
          aria-label="管理令牌"
      /></label>
      <p v-if="loginError" class="form-error">{{ loginError }}</p>
      <button class="primary wide" :disabled="logging">
        {{ logging ? "正在连接…" : "进入控制台" }}<Icon name="ArrowRight" />
      </button>
      <p class="login-note">
        <Icon name="LockKeyhole" :size="14" />仅连接本机 ·
        令牌不会发送到外部服务
      </p>
    </form>
  </div>
  <div v-else class="app-shell" :class="{ collapsed }">
    <aside class="sidebar">
      <router-link to="/overview" class="brand"
        ><Icon name="ShieldCheck" :size="34" /><span
          >AegisHook</span
        ></router-link
      >
      <nav>
        <div v-for="group in groups" :key="group.name" class="nav-group">
          <div class="nav-label">{{ group.name }}</div>
          <router-link
            v-for="item in group.items"
            :key="item[0]"
            :to="item[0]"
            :aria-label="item[1]"
            :title="item[1]"
            class="nav-item"
            ><Icon :name="item[2]" /><span>{{ item[1] }}</span
            ><b
              v-if="item[0] === '/approvals' && store.pending.length"
              class="count-bubble"
              >{{ store.pending.length }}</b
            ></router-link
          >
        </div>
      </nav>
      <div class="sidebar-foot">
        <router-link to="/agents" class="health"
          ><i class="dot" :class="{ green: store.online > 0 }" /><span>{{
            !store.streamOnline
              ? "审查服务连接失败"
              : store.online
                ? "已连接 " + store.online + " 个会话"
                : "暂无持续连接"
          }}</span></router-link
        ><button class="profile" @click="store.logout">
          <span class="avatar">管</span><span>管理员</span
          ><Icon name="LogOut" :size="16" />
        </button>
      </div>
    </aside>
    <div class="workspace">
      <header class="topbar">
        <button
          class="icon-button"
          :aria-label="collapsed ? '展开侧栏' : '收起侧栏'"
          @click="collapsed = !collapsed"
        >
          <Icon name="PanelLeft" />
        </button>
        <form class="global-search" @submit.prevent="doSearch">
          <Icon name="Search" :size="18" />
          <input v-model="search" placeholder="搜索" aria-label="全局搜索" />
          <kbd>↵</kbd>
        </form>
        <div class="topbar-actions">
          <router-link
            to="/settings"
            class="icon-button chrome-button"
            aria-label="打开系统设置"
            ><Icon name="Settings" :size="19"
          /></router-link>
          <button
            class="icon-button chrome-button"
            @click="toggleTheme"
            :aria-label="theme === 'light' ? '切换深色主题' : '切换浅色主题'"
          >
            <Icon :name="theme === 'light' ? 'Moon' : 'Sun'" :size="19" />
          </button>
        </div>
      </header>
      <main>
        <div v-if="store.error" role="alert" class="alert danger">
          <Icon name="CircleAlert" />{{ store.error
          }}<button class="text-button" @click="store.error = ''">关闭</button>
        </div>
        <router-view />
      </main>
    </div>
    <div v-if="store.toast" class="toast" role="status">
      <Icon name="CircleCheck" />{{ store.toast }}
    </div>
  </div>
</template>
