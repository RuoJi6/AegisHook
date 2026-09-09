<script setup lang="ts">
withDefaults(defineProps<{ size?: number }>(), { size: 34 });
</script>

<template>
  <img
    class="brand-logo"
    src="/logo.svg"
    :width="size"
    :height="size"
    alt=""
    aria-hidden="true"
  />
</template>

<style scoped>
.brand-logo {
  display: block;
  flex-shrink: 0;
  object-fit: contain;
}
</style>
