<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { Call } from "../types";
import {
  api,
  executionLabel,
  agentName,
  reviewPathLabel,
  matchedRuleName,
} from "../store";
import Icon from "./Icon.vue";
const props = defineProps<{ call: Call }>();
const detail = ref<Call>(props.call);
const call = computed(() => detail.value);
const more = ref(false);
const showCommand = ref(false);
const showScripts = ref(false);
const showModelInput = ref(false);
onMounted(async () => {
  try {
    detail.value = await api<Call>(`/reviews/${encodeURIComponent(props.call.id)}`);
  } catch {
    // Keep the list data if the detail request races with a refresh or deletion.
  }
});
const command = computed(() => {
  const args = detail.value.argumentsObj;
  return typeof args.command === "string"
    ? args.command
    : typeof args.cmd === "string"
      ? args.cmd
      : null;
});
</script>
<template>
  <div class="call-detail">
    <div>
      <div class="request-heading">
        <span class="eyebrow">{{ agentName(call.agent) }} · 工具请求</span>
        <div
          v-if="command !== null"
          class="request-display"
          role="group"
          aria-label="参数显示方式"
        >
          <button
            type="button"
            :aria-pressed="!showCommand"
            @click="showCommand = false"
          >
            JSON 参数
          </button>
          <button
            type="button"
            :aria-pressed="showCommand"
            @click="showCommand = true"
          >
            命令视图
          </button>
        </div>
      </div>
      <pre class="request-content">{{
        showCommand && command !== null
          ? command
          : JSON.stringify(call.argumentsObj, null, 2)
      }}</pre>
      <span class="muted">执行结果：{{ executionLabel(call.execution) }}</span>
      <div v-if="call.scriptFiles?.length" class="script-snapshot">
        <button class="text-button" @click="showScripts = !showScripts">
          {{ showScripts ? "收起脚本快照" : "查看脚本快照" }}
          <Icon :name="showScripts ? 'ChevronUp' : 'ChevronDown'" :size="14" />
        </button>
        <span class="muted">采集状态：{{ call.scriptCaptureStatus || "未知" }}</span>
        <template v-if="showScripts">
          <div v-for="script in call.scriptFiles" :key="script.path + script.sha256" class="script-item">
            <p class="muted mono">{{ script.path }} · {{ script.bytes || 0 }} bytes · {{ script.sha256 || "无摘要" }}</p>
            <p v-if="script.readError" class="muted">{{ script.readError }}</p>
            <pre v-if="script.content">{{ script.content }}</pre>
          </div>
        </template>
      </div>
    </div>
    <div>
      <span class="eyebrow">{{
        call.decision === "pending" ? "审查状态" : "返回给 Agent 的裁决"
      }}</span>
      <p class="feedback">
        <Icon
          :name="
            call.decision === 'reject'
              ? 'ShieldX'
              : call.decision === 'pending'
                ? 'Clock3'
                : 'CircleCheck'
          "
          :class="call.decision"
        />{{
          call.comment ||
          (call.mode === "model"
            ? "模型审查中，工具尚未执行。"
            : "等待人工审批，工具尚未执行。")
        }}
      </p>
      <span class="muted"
        >{{ reviewPathLabel(call) }} · 配置版本 v{{ call.version }}
        <template v-if="call.ruleId">
          · {{ call.ruleId
          }}<template v-if="matchedRuleName(call)"
            >（{{ matchedRuleName(call) }}）</template
          ></template
        ></span
      >
    </div>
  </div>
  <div class="detail-more">
    <button class="text-button" @click="more = !more">
      {{ more ? "收起上下文" : "查看上下文与执行结果"
      }}<Icon :name="more ? 'ChevronUp' : 'ChevronDown'" :size="14" /></button
    ><template v-if="more"
      ><p class="muted">用户消息</p>
      <pre>{{ call.userMessage || "未提供" }}</pre>
      <p class="muted">可见会话上下文</p>
      <pre>{{ call.context || "未提供" }}</pre>
      <template v-if="call.modelVerdict">
        <p class="muted">模型初判：{{ call.modelVerdict.decision }}</p>
        <pre>{{ call.modelVerdict.comment }}</pre>
      </template>
      <p class="muted">执行输出</p>
      <pre>{{ call.result || "尚无执行输出" }}</pre>
      <template v-if="call.modelInput">
        <button class="text-button" @click="showModelInput = !showModelInput">
          {{ showModelInput ? "收起模型输入" : "查看实际模型输入" }}
          <Icon :name="showModelInput ? 'ChevronUp' : 'ChevronDown'" :size="14" />
        </button>
        <pre v-if="showModelInput">{{ JSON.stringify(call.modelInput, null, 2) }}</pre>
        <p class="muted">原始脚本已发送给审查模型</p>
      </template>
      <p class="muted mono">参数摘要 {{ call.digest }}</p></template
    >
  </div>
</template>
