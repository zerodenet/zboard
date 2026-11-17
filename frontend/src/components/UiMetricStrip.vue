<template>
  <div class="metric-grid ui-metric-strip" :class="`ui-metric-strip-${variant}`" :style="{ '--metric-columns': columns }">
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ columns?: number; variant?: 'cards' | 'plain' | 'panel' }>(), { columns: 4, variant: 'panel' })
</script>

<style scoped>
.ui-metric-strip-panel { padding: 20px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); }
.ui-metric-strip:not(.ui-metric-strip-cards) { grid-template-columns: repeat(var(--metric-columns), minmax(0, 1fr)); gap: 24px; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card) { display: block; min-height: 0; padding: 0; border: 0; border-radius: 0; background: transparent; box-shadow: none; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-icon) { display: none; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-copy) { gap: 6px; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-heading) { display: grid; grid-template-columns: minmax(0, 1fr); gap: 6px; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-value) { font-size: 24px; line-height: 1.25; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-badge) { position: static; display: block; margin-top: 6px; }
.ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-description) { white-space: normal; }
@media (max-width: 760px) { .ui-metric-strip:not(.ui-metric-strip-cards) { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px 16px; } }
@media (max-width: 600px) { .ui-metric-strip:not(.ui-metric-strip-cards) :deep(.overview-card-value) { font-size: 21px; } }
</style>
