<template>
  <nav v-if="pages.length > 1" class="admin-page-navigation" :class="`admin-page-navigation-${orientation}`" :aria-label="`${sectionLabel}页面`">
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

withDefaults(defineProps<{ orientation?: 'horizontal' | 'vertical' }>(), { orientation: 'horizontal' })
const route = inject(routeLocationKey, undefined)
const current = computed(() => route?.path ? resolveAdminNavigation(route.path, adminNavigation.value) : undefined)
const pages = computed(() => {
  const entry = current.value
  if (!entry) return []
  if (entry.section.pages.length > 1) return entry.section.pages
  return entry.domain.sections.flatMap(section => section.pages)
})
const sectionLabel = computed(() => current.value?.section.pages.length && current.value.section.pages.length > 1 ? current.value.section.label : current.value?.domain.label || '当前分组')
const activePage = computed(() => current.value?.page.to)
</script>
