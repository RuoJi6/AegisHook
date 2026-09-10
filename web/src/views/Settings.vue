<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { api, useConsole } from "../store";
import type { Settings } from "../types";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import SettingSwitch from "../components/SettingSwitch.vue";
import {
  hasDataGuard,
  setDataGuard,
  type PromptTemplate,
} from "../prompt-guard";
const store = useConsole();
const promptTemplate = ref<PromptTemplate | null>(null);
const dataGuardEnabled = computed(
  () =>
    !!form.value &&
    !!promptTemplate.value &&
    hasDataGuard(form.value.prompt, promptTemplate.value),
);
const form = ref<Settings | null>(null),
  apiKey = ref(""),
  testing = ref(false),
  dirty = ref(false);
async function loadPromptTemplate() {
  try {
    promptTemplate.value = await api<PromptTemplate>(
      "/settings/default-prompt",
    );
    return promptTemplate.value;
  } catch (error) {
    store.error = String(error);
    return null;
  }
}
onMounted(loadPromptTemplate);
function toggleDataGuard(enabled: boolean) {
  if (!form.value || !promptTemplate.value) return;
  try {
    form.value.prompt = setDataGuard(
      form.value.prompt,
      enabled,
      promptTemplate.value,
    );
    dirty.value = true;
  } catch (error) {
    store.error = String(error);
  }
}
watch(
  () => store.settings,
  (s) => {
    if (s && !dirty.value) {
      form.value = JSON.parse(JSON.stringify(s));
      if (!form.value!.model.pricing.currency)
        form.value!.model.pricing.currency = "CNY";
    }
  },
  { immediate: true },
);
async function save() {
  if (!form.value) return;
  const payload: any = { ...form.value };
  if (apiKey.value) payload.apiKey = apiKey.value;
  const ok = await store.action(() => api("/settings", "PUT", payload));
  if (ok) {
    dirty.value = false;
    form.value = JSON.parse(JSON.stringify(store.settings!));
    apiKey.value = "";
  }
}
async function test() {
  testing.value = true;
  await store.action(
    () => api("/model/test", "POST", {}),
    "模型连接与裁决格式测试通过",
  );
  testing.value = false;
  if (!dirty.value && store.settings)
    form.value = JSON.parse(JSON.stringify(store.settings));
}
async function restore() {
  const p = promptTemplate.value || (await loadPromptTemplate());
  if (!p || !form.value) return;
  form.value!.prompt = p.prompt;
  dirty.value = true;
}
</script>
<template>
  <div class="page-heading">
    <div>
      <h1><Icon name="Settings" :size="24" />系统设置</h1>
      <p>配置审查模式与模型。变更只影响之后提交的工具调用。</p>
    </div>
    <button class="primary" @click="save">
      <Icon name="Check" :size="17" />保存设置
    </button>
  </div>
  <form
    v-if="form"
    class="settings-grid"
    @submit.prevent="save"
    @input="dirty = true"
    @change="dirty = true"
  >
    <section class="panel form-panel">
      <h2>审查模式</h2>
      <p class="muted">
        所有接入的工具调用先检查授权范围和已启用规则。规则命中允许或拒绝后直接返回；只有未命中的调用进入下方所选模式。拒绝规则优先于允许规则。
      </p>
      <label class="choice" :class="{ chosen: form.mode === 'human' }"
        ><input type="radio" v-model="form.mode" value="human" /><Icon
          name="UserRound"
        /><span
          ><strong>人工审查</strong
          ><small>规则未定的调用逐次等待你批准。</small></span
        ></label
      ><label class="choice" :class="{ chosen: form.mode === 'model' }"
        ><input
          type="radio"
          v-model="form.mode"
          value="model"
          :disabled="!store.settings?.model.tested"
        /><Icon name="Bot" /><span
          ><strong>模型审查</strong
          ><small>规则未命中的调用由模型判断允许或拒绝，不转人工。</small></span
        ></label
      >
      <div class="notice">
        <Icon name="Info" />
        <p>
          人工待审批从提交时计时，超时未处理自动拒绝；模型连接失败、超时或非法输出也会拒绝。
        </p>
      </div>
      <div class="form-two">
        <label
          >人工等待时限（秒）<input
            type="number"
            min="10"
            max="86400"
            v-model.number="form.approvalSeconds" /></label
        ><label
          >模型请求超时（秒）<input
            type="number"
            min="1"
            max="120"
            v-model.number="form.modelSeconds"
        /></label>
      </div>
    </section>
    <section class="panel form-panel">
      <div class="inline">
        <h2>审查模型</h2>
        <span
          class="badge"
          :class="store.settings?.model.tested ? 'approve' : 'pending'"
          >{{ store.settings?.model.tested ? "测试通过" : "未验证" }}</span
        >
      </div>
      <p class="muted">
        支持 OpenAI 兼容与 Anthropic Messages 协议，独立于 Pi 的模型登录。
      </p>
      <label
        >API 协议<AppSelect
          v-model="form.model.protocol"
          label="API 协议"
          :options="[
            { value: 'openai', label: 'OpenAI 兼容（Chat Completions）' },
            { value: 'anthropic', label: 'Anthropic（Messages）' },
          ]" /></label
      ><label
        >Base URL<input
          v-model="form.model.baseUrl"
          placeholder="https://your-provider.example/v1" /></label
      ><label
        >模型名称<input
          v-model="form.model.model"
          placeholder="填写服务商提供的模型 ID" /></label
      ><label
        >API Key<input
          type="password"
          v-model="apiKey"
          autocomplete="new-password"
          :placeholder="
            form.model.hasKey ? '已保存，留空保留原值' : '本地无鉴权服务可留空'
          "
      /></label>
      <div class="inline">
        <button type="button" @click="test" :disabled="testing || dirty">
          <Icon name="Plug" :size="16" />{{
            testing ? "测试中…" : "测试已保存的配置"
          }}</button
        ><span class="tiny muted">先保存配置，再测试连接。</span>
      </div>
    </section>
    <section class="panel form-panel prompt-panel pricing-panel">
      <h2>审查模型费用估算</h2>
      <p class="muted">
        填写当前模型的每百万 Token
        单价。包含连接测试，按请求时的单价留存；未配置的历史请求不会补算。实际账单以服务商为准。
      </p>
      <SettingSwitch
        v-model="form.model.pricing.enabled"
        label="启用费用估算"
        description="开启后，按填写的模型单价估算审查请求费用，包含连接测试。未配置的历史请求不会补算，实际账单以服务商为准。保存设置后生效。"
        @update:model-value="dirty = true"
      />
      <div class="pricing-fields">
        <label
          >计价币种<AppSelect
            v-model="form.model.pricing.currency"
            label="计价币种"
            :options="[
              { value: 'CNY', label: '人民币 CNY' },
              { value: 'USD', label: '美元 USD' },
            ]"
        /></label>
        <label
          >输入单价<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.input"
        /></label>
        <label
          >输出单价<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.output"
        /></label>
        <label
          >缓存读取单价<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.cacheRead"
        /></label>
        <label
          >缓存写入单价<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.cacheWrite"
        /></label>
      </div>
      <small class="muted"
        >0
        表示免费；切换模型或服务商时，请同步核对单价。缓存写入使用统一单价，不区分缓存时长。</small
      >
    </section>
    <section class="panel form-panel prompt-panel">
      <div class="panel-heading">
        <div>
          <h2>模型审查提示词</h2>
          <p class="muted">结合上下文审查 · 拒绝规则优先 · 模型直接裁决</p>
        </div>
        <button type="button" @click="restore">
          <Icon name="RotateCcw" :size="16" />恢复默认
        </button>
      </div>
      <SettingSwitch
        :model-value="dataGuardEnabled"
        :disabled="!promptTemplate"
        label="限制漏洞确认后的批量取数"
        :description="'结合上下文中的漏洞验证证据，拦截超出最小验证需要的持续取数，包括分页、遍历 ID 和拆分请求；不按 --dump 等命令关键词判断。\n默认关闭，按需开启。切换会插入或移除对应规则块，保存设置后生效。\n仅作用于进入模型审查的调用，前置允许规则仍可能直接放行。'"
        @update:model-value="toggleDataGuard"
      />
      <label
        >提示词内容<textarea
          class="prompt-editor mono"
          v-model="form.prompt"
          rows="19"
        />
      </label>
      <div class="inline muted tiny">
        <Icon name="History" :size="15" />当前配置 v{{
          form.version
        }}；每次保存生成新版本，历史调用保留原版本快照。
      </div>
    </section>
  </form>
</template>
