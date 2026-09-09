import { createApp } from "vue";
import { createPinia } from "pinia";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import Approvals from "./views/Approvals.vue";
import Overview from "./views/Overview.vue";
import Management from "./views/Management.vue";
import Settings from "./views/Settings.vue";
import "./style.css";
import "./minimal.css";
const routes = [
  { path: "/", redirect: "/approvals" },
  { path: "/approvals", component: Approvals },
  { path: "/calls", component: Approvals },
  { path: "/overview", component: Overview },
  { path: "/sessions", component: Overview },
  ...["rules", "scopes", "agents", "audit"].map((p) => ({
    path: "/" + p,
    component: Management,
  })),
  { path: "/settings", component: Settings },
  { path: "/:pathMatch(.*)*", redirect: "/overview" },
];
createApp(App)
  .use(createPinia())
  .use(createRouter({ history: createWebHistory(), routes }))
  .mount("#app");
