<template>
  <nav class="table-pager cursor-pager" data-variant="stripe" aria-label="历史记录分页">
    <span class="cursor-pager-summary">{{ count }} / {{ total ?? '—' }}</span>
    <div class="cursor-pager-controls">
      <label class="cursor-pager-size"><span class="sr-only">每次加载条数</span><UiSelect :model-value="limit" :options="pageSizeOptions" aria-label="每次加载条数" @update:model-value="changeLimit" /></label>
      <UiButton class="cursor-pager-nav cursor-pager-newer" variant="secondary" size="sm" icon type="button" :disabled="!hasPrevious || loading" aria-label="较新记录" title="较新记录" @click="$emit('previous')"><UiIcon name="chevron" /></UiButton>
      <UiButton class="cursor-pager-nav cursor-pager-older" variant="secondary" size="sm" icon type="button" :disabled="!hasNext || loading" aria-label="较旧记录" title="较旧记录" @click="$emit('next')"><UiIcon name="chevron" /></UiButton>
    </div>
  </nav>
</template>

<script setup lang="ts">
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiSelect from './UiSelect.vue'

withDefaults(defineProps<{ count: number; total?: number | null; limit: number; loading?: boolean; hasPrevious?: boolean; hasNext?: boolean }>(), {
  loading: false,
  hasPrevious: false,
  hasNext: false,
})
const pageSizeOptions = [25, 50, 100].map(value => ({ label: String(value), value }))
const emit = defineEmits<{ previous: []; next: []; limit: [value: number] }>()
function changeLimit(value: number) { emit('limit', Number(value)) }
</script>

<style scoped>
.cursor-pager {
  min-height: 42px;
  padding: 6px 12px;
  color: var(--muted);
  font-size: 10px;
}
.cursor-pager-summary { font-variant-numeric: tabular-nums; }
.cursor-pager-controls { display: flex; align-items: center; gap: 5px; }
.cursor-pager-size { display: inline-flex; align-items: center; margin-right: 7px; }
.cursor-pager :deep(.cursor-pager-size .p-select) {
  width: 62px;
  min-height: 28px;
  height: 28px;
  border-radius: 6px;
  box-shadow: none;
}
.cursor-pager :deep(.cursor-pager-size .p-select-label) { padding: 5px 7px; font-size: 10px; line-height: 16px; }
.cursor-pager :deep(.cursor-pager-size .p-select-dropdown) { width: 22px; }
.cursor-pager-nav.p-button {
  width: 28px;
  min-width: 28px;
  min-height: 28px;
  height: 28px;
  padding: 0;
  border-color: var(--line);
  border-radius: 6px;
  color: var(--text-secondary);
  background: var(--surface);
  box-shadow: none;
}
.cursor-pager-nav.p-button:hover:not(:disabled) {
  border-color: var(--line-strong);
  color: var(--text-strong);
  background: var(--surface-hover);
}
.cursor-pager-nav.p-button:disabled { color: var(--subtle); opacity: .42; }
.cursor-pager-nav .ui-icon { width: 12px; height: 12px; }
.cursor-pager-newer .ui-icon { transform: rotate(180deg); }

@media (max-width: 720px) {
  .cursor-pager { align-items: center; flex-direction: row; }
  .cursor-pager-controls { justify-content: flex-end; }
}
@media (max-width: 480px) {
  .cursor-pager-summary { display: none; }
  .cursor-pager-controls { width: 100%; justify-content: flex-end; }
}
</style>
