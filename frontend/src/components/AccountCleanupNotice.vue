<template>
  <PageAlert v-if="error" tone="warning" title="账号清理规则暂时无法读取">
    请重试以确认当前账号的清理安排。
    <template #actions><UiButton variant="secondary" size="sm" :loading="loading" @click="load">重新读取</UiButton></template>
  </PageAlert>
  <PageAlert v-else-if="status?.enabled" :tone="status.state === 'eligible' || status.state === 'scheduled' ? 'warning' : 'info'" title="账号自动删除规则已启用">
    <span class="cleanup-notice-line">连续 {{ status.inactive_days }} 天无有效订阅且无余额、未结事项的账号会被永久删除，无法恢复。</span>
    <span v-if="status.state === 'protected'" class="cleanup-notice-line">当前账号暂不删除：{{ status.reason }}。</span>
    <span v-else-if="status.state === 'eligible'" class="cleanup-notice-line">你的账号已达到清理条件，等待自动删除。如需保留，请及时购买订阅。</span>
    <span v-else-if="status.eligible_at" class="cleanup-notice-line">{{ status.state === 'subscribed' ? '若不续订，预计' : '预计' }}于 <time :datetime="status.eligible_at">{{ formatDateTime(status.eligible_at) }}</time> 后删除。时间会随订阅和资金状态更新。</span>
    <span v-else class="cleanup-notice-line">当前有长期订阅，订阅结束后重新计算清理时间。</span>
    <template v-if="status.state !== 'protected'" #actions><UiButton as-child variant="secondary" size="sm"><RouterLink to="/account/plans">{{ status.state === 'subscribed' ? '查看续订套餐' : '购买订阅' }}</RouterLink></UiButton></template>
  </PageAlert>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
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
function refreshOnFocus() { if (resource.canAutoRefresh.value) void load() }
onMounted(() => window.addEventListener('focus', refreshOnFocus))
onBeforeUnmount(() => window.removeEventListener('focus', refreshOnFocus))
watch(() => [route.path, app.user.id], () => { resource.reset(); void load() }, { immediate: true })
</script>
<style scoped>
.cleanup-notice-line { display: block; }
</style>
