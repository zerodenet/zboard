<template>
  <section class="data-workbench" :aria-busy="loading" :data-density="density">
    <header class="workbench-toolbar">
      <div class="workbench-filters"><slot name="filters" /></div>
      <div class="workbench-actions">
        <span v-if="refreshing" class="workbench-refreshing" role="status"><UiIcon name="refresh" />更新中</span>
        <span v-if="total !== undefined" class="workbench-total"><strong>{{ total }}</strong><span>条</span></span>
        <UiButton v-if="showDensity" variant="ghost" size="sm" type="button" :aria-pressed="density === 'comfortable'" :aria-label="density === 'compact' ? '切换为舒适行高' : '切换为紧凑行高'" @click="$emit('update:density', density === 'compact' ? 'comfortable' : 'compact')"><UiIcon name="menu" />{{ density === 'compact' ? '紧凑' : '舒适' }}</UiButton>
        <slot name="actions" />
      </div>
    </header>
    <div v-if="$slots.selection" class="workbench-selection"><slot name="selection" /></div>
    <div class="workbench-content"><slot /></div>
    <footer v-if="$slots.footer" class="workbench-footer"><slot name="footer" /></footer>
  </section>
</template>

<script setup lang="ts">
import UiIcon from './UiIcon.vue'
import UiButton from './UiButton.vue'

withDefaults(defineProps<{ total?: number; loading?: boolean; refreshing?: boolean; density?: 'compact' | 'comfortable'; showDensity?: boolean }>(), { total: undefined, loading: false, refreshing: false, density: 'compact', showDensity: false })
defineEmits<{ 'update:density': [value: 'compact' | 'comfortable'] }>()
</script>
