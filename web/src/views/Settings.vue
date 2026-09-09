<script setup lang="ts">
import { ref, watch } from "vue";
import { api, useConsole } from "../store";
import type { Settings } from "../types";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
const store = useConsole();
const form = ref<Settings | null>(null),
  apiKey = ref(""),
  testing = ref(false),
  dirty = ref(false);
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
  const p = await api("/settings/default-prompt");
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
      <p class="muted">明确拒绝的规则始终优先；未定调用进入所选模式。</p>
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
          ><small>由模型直接判断允许或拒绝，不转人工。</small></span
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
      <label class="inline"
        ><input
          type="checkbox"
          v-model="form.model.pricing.enabled"
        />启用费用估算</label
      >
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
          <p class="muted">真实资产与可恢复性 · 模型直接裁决 · D1 默认放行</p>
        </div>
        <button type="button" @click="restore">
          <Icon name="RotateCcw" :size="16" />恢复默认
        </button>
      </div>
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
