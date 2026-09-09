<script setup lang="ts">
import { computed } from "vue";
import AppSelect from "./AppSelect.vue";
import Icon from "./Icon.vue";
const props = defineProps<{
  page: number;
  pageSize: number;
  total: number;
  label: string;
}>();
const emit = defineEmits<{
  "update:page": [number];
  "update:pageSize": [number];
}>();
const pages = computed(() =>
  Math.max(1, Math.ceil(props.total / props.pageSize)),
);
const start = computed(() =>
  props.total ? (props.page - 1) * props.pageSize + 1 : 0,
);
</script>
<template>
  <nav class="record-pagination" :aria-label="`${label}分页`">
    <span class="pagination-count" aria-live="polite"
      >共 {{ total }} 条<span v-if="total">
        · {{ start }}–{{ Math.min(page * pageSize, total) }}</span
      ></span
    >
    <div class="pagination-controls">
      <AppSelect
        :model-value="String(pageSize)"
        :label="`${label}每页条数`"
        :options="
          [10, 20, 50, 100].map((n) => ({
            value: String(n),
            label: `${n} 条 / 页`,
          }))
        "
        @update:model-value="emit('update:pageSize', Number($event))"
      />
      <button
        type="button"
        class="icon-button"
        :disabled="page <= 1"
        aria-label="上一页"
        @click="emit('update:page', page - 1)"
      >
        <Icon name="ChevronLeft" :size="16" />
      </button>
      <span class="pagination-position" aria-live="polite"
        >{{ page }} / {{ pages }}</span
      >
      <button
        type="button"
        class="icon-button"
        :disabled="page >= pages"
        aria-label="下一页"
        @click="emit('update:page', page + 1)"
      >
        <Icon name="ChevronRight" :size="16" />
      </button>
    </div>
  </nav>
</template>
