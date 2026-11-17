<template>
  <details class="credential-migration" @toggle="toggle">
    <summary>连接凭证</summary>
    <div class="credential-content">
      <PageAlert v-if="operationError || error" tone="danger" title="凭证状态未更新">{{ operationError || error }}</PageAlert>
      <p v-if="loading && !loaded" role="status">正在读取凭证状态…</p>
      <template v-if="status">
        <div class="credential-heading"><strong>{{ label }}</strong><UiButton variant="ghost" size="sm" :disabled="busy || loading" @click="load">刷新</UiButton></div>
        <p v-if="status.state === 'legacy'">当前按接入点维护凭证。合并后使用一份订阅身份；节点确认前保留原配置。</p>
        <p v-else-if="status.state === 'awaiting_confirmation'">{{ status.pending_confirmation }} 个旧凭证正在等待节点确认，新旧配置暂时并存。<template v-if="status.compatibility_until">部分旧配置将从 {{ formatDateTime(status.compatibility_until) }} 起陆续停用。</template></p>
        <p v-else-if="status.state === 'compatibility'">旧配置将从 {{ formatDateTime(status.compatibility_until) }} 起陆续停用。请提醒用户更新订阅；原订阅链接保持可用。</p>
        <p v-else-if="status.state === 'revoking'">正在核对旧凭证撤销结果，尚未确认的节点会继续重试。</p>
        <p v-else>使用一份订阅身份。退役凭证仅保留历史流量归属依据。</p>
        <div class="credential-actions">
          <UiButton v-if="active && status.state === 'legacy'" variant="secondary" size="sm" :disabled="busy || loading" @click="change('prepare')">{{ busy ? '正在准备…' : '合并连接凭证' }}</UiButton>
          <UiButton v-if="active && status.can_rollback" variant="secondary" size="sm" :disabled="busy || loading" @click="change('rollback')">{{ busy ? '正在处理…' : '恢复旧凭证' }}</UiButton>
        </div>
      </template>
      <UiButton v-else-if="error" variant="secondary" size="sm" :disabled="loading" @click="load">重新读取</UiButton>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { fetchSubscriptionCredentialMigration, prepareSubscriptionCredentialMigration, rollbackSubscriptionCredentialMigration, type SubscriptionCredentialMigrationStatus } from '../api/client'
import { useRemoteResource } from '../composables/useRemoteResource'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { formatDateTime } from '../utils/format'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'

const props = defineProps<{ subscriptionId: number; active: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const expanded = ref(false), busy = ref(false), operationError = ref('')
let disposed = false
onBeforeUnmount(() => { disposed = true })
const resource = useRemoteResource<SubscriptionCredentialMigrationStatus | null>({
  initial: () => null,
  fetch: ({ signal }) => fetchSubscriptionCredentialMigration(props.subscriptionId, { signal }),
  errorMessage: cause => normalizeApiErrorMessage(cause, '暂时无法读取连接凭证状态。'),
})
const { data: status, loading, loaded, error } = resource
async function load() { if (await resource.load()) operationError.value = '' }
const label = computed(() => ({ legacy: '按接入点维护', awaiting_confirmation: '等待节点确认', compatibility: '旧配置兼容中', revoking: '旧凭证清退中', current: '已合并' }[status.value?.state || 'legacy']))
function toggle(event: Event) {
  expanded.value = (event.target as HTMLDetailsElement).open
  if (expanded.value && !loaded.value && !loading.value) void load()
}
watch(() => props.subscriptionId, () => {
  resource.reset(); operationError.value = ''
  if (expanded.value) void load()
})
async function change(action: 'prepare' | 'rollback') {
  if (busy.value || !props.active) return
  const id = props.subscriptionId
  busy.value = true; operationError.value = ''
  try {
    const result = await (action === 'prepare' ? prepareSubscriptionCredentialMigration(id) : rollbackSubscriptionCredentialMigration(id))
    if (disposed || id !== props.subscriptionId) return
    resource.replace(result); emit('changed')
  } catch (cause) {
    if (!disposed && id === props.subscriptionId) operationError.value = normalizeApiErrorMessage(cause, '操作结果暂未确认，请刷新凭证状态。')
  } finally { if (!disposed) busy.value = false }
}
defineExpose({ busy })
</script>

<style scoped>
.credential-migration { min-width: 0; }
summary { width: fit-content; color: var(--muted); font-size: 13px; cursor: pointer; }
.credential-content { display: grid; gap: 12px; margin-top: 14px; min-width: 0; }
.credential-heading,.credential-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.credential-heading { justify-content: space-between; }
.credential-heading strong { font-size: 13px; font-weight: 500; }
.credential-content p { margin: 0; color: var(--muted); font-size: 13px; line-height: 1.65; overflow-wrap: anywhere; }
.credential-actions:empty { display: none; }
</style>
