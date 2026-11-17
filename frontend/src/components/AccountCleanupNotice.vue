<template>
  <PageAlert v-if="!app.isAdmin && cleanupPending" tone="warning" title="账号待清理">
    <span v-if="status?.state === 'eligible'" class="cleanup-notice-line">当前账号已达到自动删除条件，删除后无法恢复。</span>
    <span v-else-if="validDeadline" class="cleanup-notice-line">预计于 <time :datetime="status!.eligible_at!">{{ formatDateTime(status!.eligible_at) }}</time> 后自动删除，删除后无法恢复。</span>
    <span v-else class="cleanup-notice-line">当前账号已进入自动清理范围，删除后无法恢复。</span>
    <span v-if="error" class="cleanup-notice-line">清理安排更新失败，显示上次结果。</span>
    <template #actions><UiButton as-child variant="secondary" size="sm"><RouterLink to="/account/plans">购买订阅</RouterLink></UiButton><UiButton v-if="error" variant="secondary" size="sm" :loading="loading" @click="load">重新读取</UiButton></template>
  </PageAlert>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchAccountCleanupStatus, type AccountCleanupStatus } from '../api/client'
import { useRemoteResource } from '../composables/useRemoteResource'
import { useAppStore } from '../stores/app'
import { formatDateTime } from '../utils/format'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
const route = useRoute(), app = useAppStore()
const resource = useRemoteResource<AccountCleanupStatus | null>({ initial: () => null, fetch: ({ signal }) => fetchAccountCleanupStatus({ signal }), errorMessage: '账号清理规则读取失败。' })
const { data: status, error, loading, load } = resource
const cleanupPending = computed(() => Boolean(status.value?.enabled && (status.value.state === 'scheduled' || status.value.state === 'eligible')))
const validDeadline = computed(() => Boolean(status.value?.eligible_at && Number.isFinite(Date.parse(status.value.eligible_at))))
function refreshOnFocus() { if (!app.isAdmin && resource.canAutoRefresh.value) void load() }
onMounted(() => window.addEventListener('focus', refreshOnFocus))
onBeforeUnmount(() => window.removeEventListener('focus', refreshOnFocus))
watch(() => [route.path, app.user.id, app.isAdmin], () => { resource.reset(); if (!app.isAdmin) void load() }, { immediate: true })
</script>
<style scoped>
.cleanup-notice-line { display: block; }
</style>
