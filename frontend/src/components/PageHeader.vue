<template>
  <header class="page-header">
    <div>
      <p v-if="eyebrow" class="page-eyebrow">{{ eyebrow }}</p>
      <h1>{{ title }}</h1>
      <p v-if="description" class="page-description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="page-actions"><slot name="actions" /></div>
    <AdminPageNavigation v-if="adminPageNavigation && showSectionNavigation" class="page-header-navigation" />
  </header>
</template>

<script setup lang="ts">
import { inject } from 'vue'
import AdminPageNavigation from './AdminPageNavigation.vue'

withDefaults(defineProps<{ title: string; description?: string; eyebrow?: string; showSectionNavigation?: boolean }>(), { showSectionNavigation: true })
const adminPageNavigation = inject('admin-page-navigation', false)
</script>
