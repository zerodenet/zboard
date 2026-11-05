<template>
  <header class="page-header">
    <div>
      <p v-if="eyebrow" class="page-eyebrow">{{ eyebrow }}</p>
      <h1>{{ displayTitle }}</h1>
      <p v-if="description" class="page-description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="page-actions"><slot name="actions" /></div>
    <AdminPageNavigation v-if="adminPageNavigation && showSectionNavigation" class="page-header-navigation" />
  </header>
</template>

<script setup lang="ts">
import { computed, inject } from 'vue'
import { routeLocationKey } from 'vue-router'
import { adminNavigation } from '../stores/navigation'
import { resolveAdminNavigation } from '../utils/adminNavigation'
import AdminPageNavigation from './AdminPageNavigation.vue'

const props = withDefaults(defineProps<{ title: string; description?: string; eyebrow?: string; showSectionNavigation?: boolean }>(), { showSectionNavigation: true })
const adminPageNavigation = inject('admin-page-navigation', false)
const route = inject(routeLocationKey, undefined)
// Only the registered page heading follows menu presentation edits. Detail and
// task-specific headings retain the title supplied by their own page.
const displayTitle = computed(() => {
  if (!adminPageNavigation || !route || props.title !== route.meta.title) return props.title
  const page = resolveAdminNavigation(route.path, adminNavigation.value)?.page
  return page?.to === route.path ? page.label : props.title
})
</script>
