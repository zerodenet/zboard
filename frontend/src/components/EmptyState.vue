<template>
  <div class="empty-state">
    <span class="empty-icon"><UiIcon :name="icon" /></span>
    <h3>{{ title }}</h3>
    <p>{{ description }}</p>
    <div v-if="$slots.actions" class="empty-actions"><slot name="actions" /></div>
  </div>
</template>

<script setup lang="ts">
import UiIcon from './UiIcon.vue'
withDefaults(defineProps<{ icon?: string; title: string; description: string }>(), { icon: 'activity' })
</script>
