<script setup lang="ts">
import { ref, useId } from "vue";
import Icon from "./Icon.vue";

defineProps<{
  modelValue: boolean;
  label: string;
  description: string;
  disabled?: boolean;
}>();
defineEmits<{ "update:modelValue": [value: boolean] }>();
const id = useId();
const helpOpen = ref(false);
</script>

<template>
  <div class="setting-switch-row">
    <div class="setting-switch-label">
      <span :id="`${id}-label`">{{ label }}</span>
      <span
        class="setting-help"
        @mouseenter="helpOpen = true"
        @mouseleave="helpOpen = false"
        @focusin="helpOpen = true"
        @focusout="helpOpen = false"
        @keydown.esc="helpOpen = false"
      >
        <button
          type="button"
          class="setting-help-button"
          :aria-label="`${label}说明`"
          :aria-describedby="`${id}-help`"
          :aria-expanded="helpOpen"
          @click="helpOpen = true"
        >
          <Icon name="CircleHelp" :size="14" />
        </button>
        <span
          v-show="helpOpen"
          :id="`${id}-help`"
          role="tooltip"
          class="setting-help-tooltip"
        >
          {{ description }}
        </span>
      </span>
    </div>
    <button
      type="button"
      role="switch"
      class="setting-switch"
      :aria-labelledby="`${id}-label`"
      :aria-checked="modelValue"
      :disabled="disabled"
      @click="$emit('update:modelValue', !modelValue)"
    >
      <span class="setting-switch-thumb" />
    </button>
  </div>
</template>

<style scoped>
.setting-switch-row {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 36px;
  margin: 0 0 18px;
}
.setting-switch-label {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  font-size: 13px;
  font-weight: 550;
}
.setting-help {
  display: inline-flex;
  flex-shrink: 0;
}
.setting-help-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 28px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--muted);
}
.setting-help-button:hover {
  color: var(--text);
  background: var(--soft);
}
.setting-help-tooltip {
  position: absolute;
  z-index: 20;
  top: calc(100% - 3px);
  left: 0;
  width: min(380px, 100%);
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow);
  color: var(--text);
  font-size: 12px;
  font-weight: 400;
  line-height: 1.75;
  white-space: pre-line;
}
.setting-switch {
  position: relative;
  flex-shrink: 0;
  width: 38px;
  height: 22px;
  padding: 0;
  border: 0;
  border-radius: 999px;
  background: color-mix(in srgb, var(--muted) 45%, var(--surface));
  transition: background 160ms ease;
}
.setting-switch[aria-checked="true"] {
  background: var(--accent);
}
.setting-switch-thumb {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 3px #0002;
  transition: transform 160ms ease;
}
.setting-switch[aria-checked="true"] .setting-switch-thumb {
  transform: translateX(16px);
}
@media (prefers-reduced-motion: reduce) {
  .setting-switch,
  .setting-switch-thumb {
    transition: none;
  }
}
</style>
