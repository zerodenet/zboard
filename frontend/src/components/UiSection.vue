<template>
  <component
    :is="as"
    class="ui-section"
    :class="[`ui-section-${variant}`, { 'ui-section-compact': compact }]"
  >
    <header v-if="title || description || $slots.meta || $slots.actions" class="panel-header">
      <div>
        <h2 v-if="title">{{ title }}</h2>
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
withDefaults(defineProps<{
  as?: 'article' | 'section' | 'aside'
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
