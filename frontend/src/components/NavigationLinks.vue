<template>
  <template v-for="node in nodes" :key="node.id">
    <RouterLink v-if="node.path" :to="node.path" @click="$emit('selectPage')">
      <UiIcon v-if="icons && node.icon" :name="node.icon" />{{ node.label }}
      <span v-if="node.path === '/account/announcements' && app.announcementUnreadCount" class="nav-count">{{ app.announcementUnreadCount > 99 ? '99+' : app.announcementUnreadCount }}</span>
    </RouterLink>
    <div v-else-if="depth < 8" class="navigation-link-group" role="group" :aria-label="node.label">
      <span class="navigation-group-label">{{ node.label }}</span>
      <NavigationLinks :surface="surface" :icons="icons" :parent-id="node.id" :depth="depth + 1" @select-page="$emit('selectPage')" />
    </div>
  </template>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { Surface } from '../api/plugins'
import { navigationState } from '../stores/navigation'
import { useAppStore } from '../stores/app'
import UiIcon from './UiIcon.vue'
const props = withDefaults(defineProps<{ surface: Surface; icons?: boolean; parentId?: string; depth?: number }>(), { icons: true, parentId: '', depth: 0 })
defineEmits<{ selectPage: [] }>()
const app = useAppStore()
const nodes = computed(() => navigationState[props.surface].snapshot.value.nodes.filter(node => node.parent_id === props.parentId))
</script>

<style scoped>
.nav-count { min-width: 18px; height: 18px; display: inline-grid; place-items: center; margin-left: auto; padding: 0 5px; border-radius: 999px; background: var(--danger); color: var(--text-inverse); font-size: 9px; font-weight: 800; }
.navigation-link-group { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.navigation-group-label { color: var(--muted-foreground); font-size: 11px; padding: 4px 8px; }
</style>
