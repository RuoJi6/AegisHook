<script setup lang="ts">
defineProps<{ modelValue: boolean; label: string; disabled?: boolean }>();
defineEmits<{ "update:modelValue": [value: boolean] }>();
</script>

<template>
  <button
    type="button"
    role="switch"
    class="rule-switch"
    :aria-label="label"
    :aria-checked="modelValue"
    :aria-busy="disabled || undefined"
    :disabled="disabled"
    @click="$emit('update:modelValue', !modelValue)"
  >
    <span />
  </button>
</template>

<style scoped>
.rule-switch {
  position: relative;
  display: inline-block;
  flex: 0 0 auto;
  width: 40px;
  min-height: 24px;
  height: 24px;
  padding: 0;
  border: 1px solid color-mix(in srgb, var(--muted) 55%, var(--border));
  border-radius: 999px;
  background: color-mix(in srgb, var(--border) 75%, var(--muted));
  transition:
    background 160ms ease,
    border-color 160ms ease;
}
.rule-switch[aria-checked="true"] {
  background: var(--primary);
  border-color: var(--primary);
}
.rule-switch > span {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--surface);
  box-shadow: 0 1px 2px #0003;
  transition: transform 160ms ease;
}
.rule-switch[aria-checked="true"] > span {
  transform: translateX(16px);
  background: var(--primary-text);
}
.rule-switch:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.rule-switch:disabled {
  cursor: wait;
  opacity: 0.6;
}
@media (prefers-reduced-motion: reduce) {
  .rule-switch,
  .rule-switch > span {
    transition: none;
  }
}
</style>
