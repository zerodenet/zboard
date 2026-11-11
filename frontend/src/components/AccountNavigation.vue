<template>
  <template v-for="item in items" :key="item.node.id">
    <RouterLink v-if="item.node.path" :to="item.node.path" :aria-current="current?.id === item.node.id ? 'page' : undefined" @click="emit('selectPage')">{{ item.node.label }}</RouterLink>
    <div v-else class="account-nav-group" :class="{ 'account-nav-group-active': currentRoot?.id === item.node.id }">
      <RouterLink v-if="item.primary" :to="item.primary.path" :aria-current="current?.id === item.primary.id ? 'page' : undefined" @click="emit('selectPage')">{{ item.node.label }}</RouterLink>
      <DropdownMenuRoot v-if="!item.primary || item.pages.length > 1">
        <DropdownMenuTrigger as-child><UiButton variant="ghost" :icon="Boolean(item.primary)" :aria-label="item.primary ? `${item.node.label}更多页面` : item.node.label" class="account-nav-trigger"><span v-if="!item.primary">{{ item.node.label }}</span><UiIcon name="chevron" /></UiButton></DropdownMenuTrigger>
        <DropdownMenuPortal><DropdownMenuContent class="ui-dropdown-content account-navigation-menu" align="start" :side-offset="6" :collision-padding="16">
          <DropdownMenuItem v-for="page in item.pages.filter(page => page.id !== item.primary?.id)" :key="page.id" as-child class="ui-dropdown-item"><RouterLink :to="page.path" :aria-current="current?.id === page.id ? 'page' : undefined" @click="emit('selectPage')">{{ page.label }}<span v-if="page.path === '/account/announcements' && app.announcementUnreadCount" class="account-nav-unread">{{ app.announcementUnreadCount }}</span></RouterLink></DropdownMenuItem>
        </DropdownMenuContent></DropdownMenuPortal>
      </DropdownMenuRoot>
    </div>
  </template>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuPortal, DropdownMenuContent, DropdownMenuItem } from 'reka-ui'
import { navigationState } from '../stores/navigation'
import { currentMenuPage, descendantPages, menuRoot } from '../utils/navigationTree'
import { useAppStore } from '../stores/app'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
const emit = defineEmits<{ selectPage: [] }>()
const app = useAppStore(), route = useRoute()
const snapshot = computed(() => navigationState.account.snapshot.value)
const current = computed(() => currentMenuPage(snapshot.value.nodes, route.path))
const currentRoot = computed(() => menuRoot(snapshot.value.nodes, current.value))
const items = computed(() => snapshot.value.nodes.filter(node => !node.parent_id).map(node => {
  const pages = descendantPages(snapshot.value.nodes, node.id)
  const hint = snapshot.value.presentation?.workspaces.find(item => item.group_id === node.id)
  return { node, pages, primary: hint?.kind === 'primary' ? pages.find(page => page.path === hint.entry_path) : undefined }
}).filter(item => item.node.path || item.pages.length))
</script>
<style scoped>
.account-nav-group { display: flex; align-items: center; gap: 0; min-width: 0; }
.account-nav-group-active { background: var(--muted-surface); border-radius: var(--radius-md); }
.account-nav-trigger > .ui-icon { width: 12px; transform: rotate(90deg); color: var(--muted-foreground); }
.account-nav-group > a { min-width: 0; }
.account-navigation-menu { min-width: min(180px,calc(100vw - 32px)); }
.account-nav-unread { margin-left: auto; color: var(--destructive); font-variant-numeric: tabular-nums; }
@media(max-width:980px) { .account-nav-group { width: 100%; justify-content: space-between; } .account-nav-group > a { flex: 1; } }
</style>
