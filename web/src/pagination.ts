import { computed, reactive, ref, watch, type WatchSource } from "vue";

export function usePagination<T>(
  source: () => T[],
  resets: WatchSource[] = [],
) {
  const page = ref(1),
    pageSize = ref(10);
  const total = computed(() => source().length);
  const pages = computed(() =>
    Math.max(1, Math.ceil(total.value / pageSize.value)),
  );
  watch(
    pageSize,
    () => {
      page.value = 1;
    },
    { flush: "sync" },
  );
  watch(
    pages,
    (count) => {
      page.value = Math.min(page.value, count);
    },
    { flush: "sync" },
  );
  if (resets.length)
    watch(
      resets,
      () => {
        page.value = 1;
      },
      { flush: "sync" },
    );
  const rows = computed(() =>
    source().slice(
      (page.value - 1) * pageSize.value,
      page.value * pageSize.value,
    ),
  );
  return reactive({ page, pageSize, total, pages, rows });
}
