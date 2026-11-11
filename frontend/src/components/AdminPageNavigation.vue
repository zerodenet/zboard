<template>
  <nav v-if="pages.length > 1 || workspaceReturn" class="admin-page-navigation" :class="`admin-page-navigation-${orientation}`" :aria-label="`${sectionLabel}页面`">
    <RouterLink v-if="workspaceReturn" class="admin-page-navigation-link workspace-return-link" :to="workspaceReturn"><UiIcon name="chevron" class="workspace-return-icon" />{{ current?.section.label }}</RouterLink>
    <RouterLink v-for="page in pages" :key="page.to" :to="page.to"
      class="admin-page-navigation-link" :aria-current="activePage === page.to ? 'page' : undefined" @click="markApprovedNavigationTransition(page.to, $event)">
      {{ page.label }}
    </RouterLink>
  </nav>
</template>

<script setup lang="ts">
import { computed, inject } from 'vue'
import { routeLocationKey } from 'vue-router'
import { resolveAdminNavigation } from '../utils/adminNavigation'
import { adminNavigation, markApprovedNavigationTransition } from '../stores/navigation'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{ orientation?: 'horizontal' | 'vertical' }>(), { orientation: 'horizontal' })
const route = inject(routeLocationKey, undefined)
const current = computed(() => route?.path ? resolveAdminNavigation(route.path, adminNavigation.value) : undefined)
const pages = computed(() => {
  const entry = current.value
  if (!entry || entry.page.footer) return []
  return entry.section.pages.filter(page => !page.footer && page.parentId === entry.page.parentId)
})
const workspaceReturn = computed(() => current.value?.section.workspace?.kind === 'hub' && current.value.page.to !== current.value.section.workspace.entry_path ? current.value.section.workspace.entry_path : '')
const sectionLabel = computed(() => current.value?.section.pages.length && current.value.section.pages.length > 1 ? current.value.section.label : current.value?.domain.label || '当前分组')
const activePage = computed(() => current.value?.page.to)
</script>
<style scoped>
.workspace-return-icon { width: 12px; height: 12px; transform: rotate(180deg); }
.workspace-return-link { gap: 6px; }
</style>
