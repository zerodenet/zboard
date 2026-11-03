<template>
  <component
    :is="as"
    class="ui-section"
    :class="[`ui-section-${variant}`, { 'ui-section-compact': compact }]"
  >
    <header v-if="title || description || $slots.meta || $slots.actions" class="panel-header">
      <div>
        <h2 v-if="title" :class="{ 'section-title-with-icon': icon }"><UiIcon v-if="icon" :name="icon" />{{ title }}</h2>
        <p v-if="description">{{ description }}</p>
      </div>
      <div v-if="$slots.meta || $slots.actions" class="ui-section-actions">
        <slot name="meta" />
        <slot name="actions" />
      </div>
    </header>
    <slot />
  </component>
</template>

<script setup lang="ts">
import UiIcon from './UiIcon.vue'
withDefaults(defineProps<{
  as?: 'article' | 'section' | 'aside'
  icon?: string
  title?: string
  description?: string
  variant?: 'flat' | 'outlined' | 'soft'
  compact?: boolean
}>(), {
  as: 'article',
  title: '',
  description: '',
  variant: 'flat',
  compact: false,
})
</script>

<style scoped>
.section-title-with-icon { display: flex; align-items: center; gap: 8px; }
.section-title-with-icon > .ui-icon { width: 16px; height: 16px; flex: none; }
</style>
