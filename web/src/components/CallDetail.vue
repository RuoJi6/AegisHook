<script setup lang="ts">
import { computed, ref } from "vue";
import type { Call } from "../types";
import {
  executionLabel,
  agentName,
  reviewPathLabel,
  matchedRuleName,
} from "../store";
import Icon from "./Icon.vue";
const props = defineProps<{ call: Call }>();
const more = ref(false);
const showCommand = ref(false);
const command = computed(() => {
  const args = props.call.argumentsObj;
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
      <p class="muted mono">参数摘要 {{ call.digest }}</p></template
    >
  </div>
</template>
