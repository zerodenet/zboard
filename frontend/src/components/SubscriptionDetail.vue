<template>
  <div class="business-detail subscription-detail">
    <div class="subscription-context">
      <div><StatusBadge :tone="statusTone">{{ statusLabel }}</StatusBadge><span>{{ subscription.sku_name || '-' }}</span></div>
      <RowActions label="订阅更多操作" :trigger-key="`subscription-detail-${subscription.id}`">
        <UiButton as-child variant="ghost" size="sm"><RouterLink :to="contextLink('/admin/orders', { user_id: String(subscription.user_id) })">用户订单</RouterLink></UiButton>
        <UiButton as-child variant="ghost" size="sm"><RouterLink :to="contextLink('/admin/users', { user: String(subscription.user_id) })">用户详情</RouterLink></UiButton>
        <UiButton as-child variant="ghost" size="sm"><RouterLink :to="contextLink('/admin/fair-use', { subscription: String(subscription.id) })">Fair Use 观测</RouterLink></UiButton>
        <UiButton v-if="canTerminate" variant="ghost" class="subscription-danger-action" size="sm" :disabled="Boolean(editing)" @click="terminationOpen = true">退订订阅</UiButton>
      </RowActions>
    </div>

    <section class="subscription-summary" aria-label="流量与有效期">
      <div class="subscription-remaining"><span>剩余流量</span><strong :class="{ 'subscription-warning': exhausted }">{{ formatBytes(remaining) }}</strong></div>
      <div class="subscription-expiry"><span>到期时间</span><strong v-if="perpetual">长期有效</strong><time v-else :datetime="subscription.end_at" :class="{ 'subscription-warning': elapsed }">{{ formatDateTime(subscription.end_at) }}</time></div>
      <div class="subscription-usage">
        <div class="usage-track" role="progressbar" :aria-label="`已用 ${formatBytes(subscription.flow_used)}，周期配额 ${formatBytes(subscription.flow_total)}`" :aria-valuenow="usagePercent" :aria-valuemin="0" :aria-valuemax="100"><i :class="{ exhausted }" :style="{ width: `${usagePercent}%` }" /></div>
        <div><span>已用 {{ formatBytes(subscription.flow_used) }} / {{ formatBytes(subscription.flow_total) }}</span><UiButton v-if="editable" id="subscription-edit-quota" variant="ghost" size="sm" :disabled="Boolean(editing)" @click="startEdit('quota')">调整额度</UiButton></div>
      </div>
    </section>
    <PageAlert v-if="subscription.ended_at" :tone="subscription.can_renew ? 'info' : 'warning'" :title="subscription.end_reason === 'exhausted' ? '流量用尽，服务已结束' : '服务已到期'">
      <template v-if="subscription.can_renew">可在 {{ formatDateTime(subscription.renewal_until) }} 前续费恢复。</template>
    </PageAlert>
    <PageAlert v-else-if="exhausted && subscription.status === 'active'" tone="warning" title="本周期流量已用完" />

    <section v-if="editing === 'quota'" class="subscription-edit-section" aria-label="调整周期额度">
      <h3>调整周期额度</h3>
      <SubscriptionQuotaEditor ref="editor" :subscription="editSnapshot!" mode="quota" cancellable @cancel="finishEdit('quota')" @saved="saved('quota')" />
    </section>

    <section class="subscription-settings" aria-label="订阅配置">
      <div class="subscription-section-heading"><h3>重置设置</h3><UiButton v-if="editable && editing !== 'reset'" id="subscription-edit-reset" variant="ghost" size="sm" :disabled="Boolean(editing)" @click="startEdit('reset')">编辑重置</UiButton></div>
      <SubscriptionQuotaEditor v-if="editing === 'reset'" ref="editor" :subscription="editSnapshot!" mode="reset" cancellable @cancel="finishEdit('reset')" @saved="saved('reset')" />
      <dl v-else class="subscription-properties">
        <div><dt>重置周期</dt><dd>{{ resetPolicyLabel }}</dd></div>
        <div><dt>每次配额</dt><dd>{{ subscription.reset_quota_bytes ? formatBytes(subscription.reset_quota_bytes) : '-' }}</dd></div>
        <div><dt>下次重置</dt><dd>{{ formatDateTime(subscription.next_reset_at) }}</dd></div>
      </dl>
    </section>

    <section class="subscription-settings" aria-label="接入设置">
      <h3>接入设置</h3>
      <dl class="subscription-properties">
        <div><dt>设备上限</dt><dd>{{ subscription.device_limit || '-' }}</dd></div>
        <div><dt>速率上限</dt><dd>{{ subscription.speed_limit_mbps ? `${subscription.speed_limit_mbps} Mbps` : '不限' }}</dd></div>
        <div><dt>流量计费</dt><dd>{{ trafficModeLabel }}</dd></div>
      </dl>
    </section>

    <div class="subscription-history-links"><UiButton as-child variant="secondary" size="sm"><RouterLink :to="contextLink('/admin/traffic', { subscription_id: String(subscription.id) })">流量明细<UiIcon name="chevron" /></RouterLink></UiButton></div>
    <details class="subscription-records"><summary>更多信息</summary><dl class="subscription-properties">
      <div><dt>开始时间</dt><dd>{{ formatDateTime(subscription.start_at) }}</dd></div>
      <div><dt>节点组</dt><dd>{{ subscription.node_group_id ? `#${subscription.node_group_id}` : '-' }}</dd></div>
      <div><dt>有效凭证</dt><dd>{{ subscription.active_credential_count }} / {{ subscription.total_credential_count }}</dd></div>
      <div><dt>更新时间</dt><dd>{{ formatDateTime(subscription.updated_at) }}</dd></div>
    </dl></details>

    <ModalDialog :open="terminationOpen" :title="`退订 · ${subscription.plan_name || `订阅 #${subscription.id}`}`" size="sm" :busy="terminationBusy" :return-focus-selector="`[data-row-action-trigger='subscription-detail-${subscription.id}']`" @close="terminationOpen = false">
      <SubscriptionTermination v-if="terminationOpen" :subscription-id="subscription.id" administrative embedded autoload-quote @busy="terminationBusy = $event" @settled="emit('changed')" />
    </ModalDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import type { AdminSubscriptionDetail } from '../api/client'
import { formatBytes, formatDateTime, formatUnknownValue } from '../utils/format'
import { resetPolicyOptions } from '../utils/resetPolicy'
import { withAdminReturnTo } from '../utils/navigation'
import SubscriptionQuotaEditor from './SubscriptionQuotaEditor.vue'
import SubscriptionTermination from './SubscriptionTermination.vue'
import RowActions from './RowActions.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import StatusBadge from './StatusBadge.vue'
import PageAlert from './PageAlert.vue'
import ModalDialog from './ModalDialog.vue'

const props = defineProps<{ subscription: AdminSubscriptionDetail }>()
const emit = defineEmits<{ changed: [] }>()
const route = useRoute()
const editing = ref<'' | 'quota' | 'reset'>('')
const editSnapshot = ref<AdminSubscriptionDetail | null>(null)
const editor = ref<InstanceType<typeof SubscriptionQuotaEditor> | null>(null)
const terminationOpen = ref(false), terminationBusy = ref(false)
const remaining = computed(() => Math.max(0, props.subscription.flow_total - props.subscription.flow_used))
const exhausted = computed(() => props.subscription.flow_used >= props.subscription.flow_total)
const usagePercent = computed(() => props.subscription.flow_total > 0 ? Math.max(0, Math.min(100, Math.round(props.subscription.flow_used / props.subscription.flow_total * 100))) : 0)
const perpetual = computed(() => props.subscription.end_at?.startsWith('9999-'))
const elapsed = computed(() => props.subscription.status === 'expired' || Boolean(props.subscription.ended_at))
const editable = computed(() => ['active', 'expired'].includes(props.subscription.status) && !props.subscription.ended_at)
const canTerminate = computed(() => ['active', 'expired'].includes(props.subscription.status))
const statusTone = computed(() => props.subscription.status === 'active' ? 'success' : props.subscription.status === 'expired' ? 'warning' : 'neutral')
const statusLabel = computed(() => ({ active: '有效', expired: '已到期', canceled: '已取消', ended: '已结束' }[props.subscription.status] || formatUnknownValue('状态', props.subscription.status)))
const resetPolicyLabel = computed(() => resetPolicyOptions.find(option => option.value === props.subscription.reset_policy)?.label || formatUnknownValue('重置策略', props.subscription.reset_policy))
const trafficModeLabel = computed(() => ['上行 + 下行', '仅上行', '仅下行'][props.subscription.traffic_calc_mode] || formatUnknownValue('流量计费', props.subscription.traffic_calc_mode))
function contextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
async function startEdit(mode: 'quota' | 'reset') {
  if (editing.value || !editable.value) return
  // Freeze the edit baseline; a read refresh must not become a stale usage write.
  editSnapshot.value = { ...props.subscription }
  editing.value = mode
  await nextTick()
  editor.value?.$el.querySelector('input,button')?.focus({ preventScroll: true })
}
async function finishEdit(mode: 'quota' | 'reset') {
  editing.value = ''; editSnapshot.value = null
  await nextTick()
  document.getElementById(`subscription-edit-${mode}`)?.focus({ preventScroll: true })
}
async function saved(mode: 'quota' | 'reset') { await finishEdit(mode); emit('changed') }
async function confirmLeave() {
  if (terminationBusy.value) return false
  return editor.value ? editor.value.confirmLeave() : true
}
defineExpose({ confirmLeave, dirty: computed(() => Boolean(editor.value?.dirty || editor.value?.saving || terminationBusy.value)) })
</script>

<style scoped>
.subscription-detail { display: grid; gap: 24px; min-width: 0; }
.subscription-context,.subscription-context > div,.subscription-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.subscription-context > div { justify-content: flex-start; flex-wrap: wrap; color: var(--muted); font-size: 13px; }
.subscription-danger-action { color: var(--destructive); }
.subscription-summary { display: grid; grid-template-columns: 1fr auto; gap: 20px 24px; padding: 20px; background: var(--surface-soft); border-radius: var(--radius-card); }
.subscription-remaining,.subscription-expiry { display: grid; align-content: start; gap: 6px; min-width: 0; }
.subscription-remaining > span,.subscription-expiry > span { color: var(--muted); font-size: 12px; }
.subscription-remaining strong { color: var(--text-strong); font-size: 30px; line-height: 1.2; font-weight: 600; font-variant-numeric: tabular-nums; }
.subscription-expiry strong,.subscription-expiry time { color: var(--text-strong); font-size: 13px; line-height: 1.6; font-weight: 500; font-variant-numeric: tabular-nums; }
.subscription-expiry { text-align: right; }
.subscription-warning { color: var(--destructive) !important; }
.subscription-usage { grid-column: 1 / -1; }
.subscription-usage > div:last-child { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 8px; color: var(--muted); font-size: 12px; }
.subscription-usage .exhausted { background: var(--destructive); }
.subscription-settings,.subscription-edit-section { display: grid; gap: 14px; min-width: 0; }
.subscription-detail h3 { margin: 0; font-size: 15px; font-weight: 600; }
.subscription-properties { display: grid; gap: 12px; margin: 0; font-size: 13px; }
.subscription-properties > div { display: flex; align-items: baseline; justify-content: space-between; gap: 24px; min-width: 0; }
.subscription-properties dt { color: var(--muted); flex-shrink: 0; }
.subscription-properties dd { margin: 0; text-align: right; overflow-wrap: anywhere; color: var(--text-strong); font-variant-numeric: tabular-nums; }
.subscription-history-links { display: flex; justify-content: flex-start; }
.subscription-records { border-top: 1px solid var(--border); padding-top: 16px; }
.subscription-records summary { width: fit-content; font-size: 13px; color: var(--muted); cursor: pointer; }
.subscription-records[open] summary { margin-bottom: 16px; }
.subscription-edit-section { padding: 16px; border: 1px solid var(--border); border-radius: var(--radius-card); }
@media (max-width: 560px) {
  .subscription-detail { gap: 22px; }
  .subscription-summary { padding: 16px; gap: 16px 12px; }
  .subscription-remaining strong { font-size: 28px; }
  .subscription-expiry time { max-width: 132px; }
  .subscription-edit-section { padding: 0; border: 0; }
  .subscription-detail :deep(button),.subscription-history-links :deep(a) { min-height: 40px; }
}
</style>
