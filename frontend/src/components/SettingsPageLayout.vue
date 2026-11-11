<template>
  <div class="settings-page-layout" :class="{ 'settings-page-layout-single': !hasNavigation }">
    <AdminPageNavigation orientation="vertical" />
    <div class="settings-page-content">
      <h2 v-if="title" class="settings-category-title">{{ categoryTitle }}</h2>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AdminPageNavigation from './AdminPageNavigation.vue'
import { adminNavigation } from '../stores/navigation'
import { resolveAdminNavigation } from '../utils/adminNavigation'
const props = defineProps<{ title?: string }>()
const route = useRoute()
const current = computed(() => resolveAdminNavigation(route.path, adminNavigation.value))
const hasNavigation = computed(() => {
  const entry = current.value
  return Boolean(entry && !entry.page.footer && (entry.section.pages.filter(page => !page.footer && page.parentId === entry.page.parentId).length > 1 || entry.section.workspace?.kind === 'hub'))
})
const categoryTitle = computed(() => resolveAdminNavigation(route.path, adminNavigation.value)?.page.label || props.title)
</script>
