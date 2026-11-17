<template>
  <nav v-if="pages.length > 1" class="account-page-navigation" :aria-label="root?.label">
    <RouterLink v-for="page in pages" :key="page.id" :to="page.path" :aria-current="page.id === current?.id ? 'page' : undefined">{{ page.label }}</RouterLink>
  </nav>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { navigationState } from '../stores/navigation'
import { currentMenuPage, descendantPages, menuRoot } from '../utils/navigationTree'
const route = useRoute(), nodes = computed(() => navigationState.account.snapshot.value.nodes)
const current = computed(() => currentMenuPage(nodes.value, route.path))
const root = computed(() => menuRoot(nodes.value, current.value))
const pages = computed(() => root.value && !root.value.path && route.path !== '/account' ? descendantPages(nodes.value, root.value.id).filter(page => page.path.startsWith('/account')) : [])
</script>
<style scoped>
.account-page-navigation { display: flex; flex-wrap: wrap; gap: 8px 24px; margin-bottom: 24px; }
.account-page-navigation a { display: inline-flex; align-items: center; min-height: 36px; padding: 4px 0; border-bottom: 2px solid transparent; color: var(--muted-foreground); font-size: 13px; text-decoration: none; }
.account-page-navigation a[aria-current='page'] { border-color: var(--foreground); color: var(--foreground); font-weight: 500; }
.account-page-navigation a:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }
@media(max-width:600px) { .account-page-navigation { gap: 6px 20px; margin-bottom: 18px; } .account-page-navigation a { min-height: 40px; } }
</style>
