<script setup lang="ts">
import type { RuleSemantics } from "../types";
defineProps<{ semantics: RuleSemantics; tool?: string }>();
</script>
<template>
  <section class="rule-semantics">
    <h3>语义检查内容</h3>
    <p>{{ semantics.summary }}</p>
    <p class="muted tiny">
      由后端代码识别；{{
        tool ? `当前仅检查工具 ${tool}` : "当前未额外限定工具名"
      }}。
    </p>
    <ul>
      <li v-for="check in semantics.checks" :key="check">{{ check }}</li>
    </ul>
    <h4>命中样例 <span class="muted tiny">仅展示参数，不执行</span></h4>
    <div
      v-for="(example, index) in semantics.examples"
      :key="index"
      class="semantic-example"
    >
      <code>{{ example.tool }}</code>
      <pre>{{ JSON.stringify(example.argumentsObj, null, 2) }}</pre>
    </div>
    <h4>当前识别范围</h4>
    <p class="muted">{{ semantics.limits }}</p>
    <p class="muted tiny">
      未命中时继续检查其它规则；仍无裁决则进入所选的人工或模型审查模式。
    </p>
  </section>
</template>
