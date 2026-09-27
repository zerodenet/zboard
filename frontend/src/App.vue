<template>
  <MaintenanceScreen
    v-if="showMaintenance"
    :title="app.maintenance.title"
    :message="app.maintenance.message"
    :migration-in-progress="app.maintenance.migration_in_progress"
    :has-token="app.isAuthenticated"
  />
  <template v-else>
    <Transition name="boot-fade">
      <div v-if="bootVisible" class="app-boot" role="status" aria-live="polite">
        <span class="app-boot-mark" aria-hidden="true">Z</span>
        <span>正在载入页面…</span>
        <span class="app-boot-track" aria-hidden="true"><span /></span>
      </div>
    </Transition>
    <AnnouncementStack v-if="showAnnouncementPrompt" :items="app.announcements" />
    <RouterView />
  </template>
  <NavigationProgress />
  <div v-if="app.maintenance.enabled && app.isAdmin" class="admin-maintenance-banner">整站维护模式已开启<span v-if="app.maintenance.migration_in_progress"> · 数据库迁移正在执行</span><span v-else-if="app.maintenance.migration_cutover_pending"> · 等待数据库切换确认</span></div>
  <FeedbackHost />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AnnouncementStack from './components/AnnouncementStack.vue'
import NavigationProgress from './components/NavigationProgress.vue'
import FeedbackHost from './components/FeedbackHost.vue'
import MaintenanceScreen from './components/MaintenanceScreen.vue'
import { MAINTENANCE_STATE_EVENT, type MaintenanceState } from './api/client'
import { useAppStore } from './stores/app'
import { waitForPagePaint } from './composables/navigationProgress'

const app = useAppStore()
const route = useRoute()
const bootVisible = ref(true)
const bootStartedAt = performance.now()
const MIN_BOOT_VISIBLE_MS = 520
watch(() => route.matched.length, async count => {
  if (!count || !bootVisible.value) return
  await waitForPagePaint()
  const remaining = MIN_BOOT_VISIBLE_MS - (performance.now() - bootStartedAt)
  if (remaining > 0) await new Promise<void>(resolve => window.setTimeout(resolve, remaining))
  bootVisible.value = false
}, { immediate: true, flush: 'post' })
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
.app-boot { position: fixed; inset: 0; z-index: 1100; display: grid; place-content: center; justify-items: center; gap: 14px; color: var(--muted); background: var(--page); font-size: 12px; animation: boot-reveal .16s .12s both; }
.boot-fade-leave-active { transition: opacity .16s ease-out; }
.boot-fade-leave-to { opacity: 0; }
.app-boot-mark { display: grid; place-items: center; width: 38px; height: 38px; border: 1px solid var(--line); border-radius: 10px; color: var(--foreground); background: var(--surface); font-size: 18px; font-weight: 700; }
.app-boot-track { width: 96px; height: 2px; overflow: hidden; border-radius: 999px; background: var(--line); }
.app-boot-track span { display: block; width: 35%; height: 100%; border-radius: inherit; background: var(--primary); animation: boot-progress 1.1s ease-in-out infinite alternate; }
@keyframes boot-reveal { from { opacity: 0; } to { opacity: 1; } }
@keyframes boot-progress { from { transform: translateX(0); } to { transform: translateX(185%); } }
@media (prefers-reduced-motion: reduce) { .app-boot, .app-boot-track span { animation: none; } .boot-fade-leave-active { transition: none; } }
.admin-maintenance-banner { position: fixed; z-index: 1250; right: 16px; bottom: 16px; padding: 10px 14px; border-radius: 10px; background: var(--danger); color: var(--text-inverse); box-shadow: 0 8px 24px var(--floating-shadow); font-size: 12px; font-weight: 750; }
</style>
