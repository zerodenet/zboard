<template>
  <MaintenanceScreen
    v-if="showMaintenance"
    :title="app.maintenance.title"
    :message="app.maintenance.message"
    :migration-in-progress="app.maintenance.migration_in_progress"
    :has-token="app.isAuthenticated"
  />
  <template v-else>
    <AnnouncementStack v-if="showAnnouncementPrompt" :items="app.announcements" />
    <RouterView />
  </template>
  <div v-if="app.maintenance.enabled && app.isAdmin" class="admin-maintenance-banner">整站维护模式已开启<span v-if="app.maintenance.migration_in_progress"> · 数据库迁移正在执行</span><span v-else-if="app.maintenance.migration_cutover_pending"> · 等待数据库切换确认</span></div>
  <FeedbackHost />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import AnnouncementStack from './components/AnnouncementStack.vue'
import FeedbackHost from './components/FeedbackHost.vue'
import MaintenanceScreen from './components/MaintenanceScreen.vue'
import { MAINTENANCE_STATE_EVENT, type MaintenanceState } from './api/client'
import { useAppStore } from './stores/app'

const app = useAppStore()
const route = useRoute()
const showMaintenance = computed(() => app.maintenance.enabled && !app.isAdmin && route.path !== '/login')
const showAnnouncementPrompt = computed(() => (!app.maintenance.enabled || app.isAdmin) && (route.path === '/' || route.path === '/account'))
let statusTimer = 0
function applyMaintenanceEvent(event: Event) {
  const state = (event as CustomEvent<MaintenanceState>).detail
  if (state) app.maintenance = state
}
onMounted(() => {
  void app.loadSystemStatus().catch(() => undefined)
  window.addEventListener(MAINTENANCE_STATE_EVENT, applyMaintenanceEvent)
  statusTimer = window.setInterval(() => void app.loadSystemStatus(true).catch(() => undefined), 60_000)
})
onBeforeUnmount(() => { window.clearInterval(statusTimer); window.removeEventListener(MAINTENANCE_STATE_EVENT, applyMaintenanceEvent) })
</script>

<style scoped>
.admin-maintenance-banner { position: fixed; z-index: 1250; right: 16px; bottom: 16px; padding: 10px 14px; border-radius: 10px; background: var(--danger); color: var(--text-inverse); box-shadow: 0 8px 24px var(--floating-shadow); font-size: 12px; font-weight: 750; }
</style>
