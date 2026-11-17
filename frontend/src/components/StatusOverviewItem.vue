<template>
  <div class="status-overview-item">
    <span class="status-overview-icon"><UiIcon :name="icon" /></span>
    <div class="status-overview-copy">
      <strong>{{ label }}</strong>
      <p v-if="description || $slots.description" v-overflow-title="description"><slot name="description">{{ description }}</slot></p>
    </div>
    <StatusBadge class="status-overview-badge" :tone="tone">{{ status }}</StatusBadge>
  </div>
</template>

<script setup lang="ts">
import { vOverflowTitle } from '../directives/overflowTitle'
import StatusBadge from './StatusBadge.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  icon: string
  label: string
  description?: string
  status: string
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info'
}>(), { description: '', tone: 'neutral' })
</script>

<style scoped>
.status-overview-item {
  min-width: 0;
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 11px 4px;
}
.status-overview-icon {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  color: var(--muted-foreground);
  background: var(--muted-surface);
  font-size: 14px;
}
.status-overview-copy { min-width: 0; }
.status-overview-copy strong { font-size: 13px; line-height: 1.35; }
.status-overview-copy p {
  min-width: 0;
  margin: 2px 0 0;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.status-overview-badge {
  max-width: 100%;
  justify-self: end;
}

@media (max-width: 520px) {
  .status-overview-item { grid-template-columns: 28px minmax(0, 1fr); gap: 9px; }
  .status-overview-badge { grid-column: 2; justify-self: start; }
}
</style>
