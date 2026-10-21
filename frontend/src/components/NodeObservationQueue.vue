<template>
  <DetailDrawer :open="open" title="观察队列" description="全局节点观察处置记录，包括已删除资产。" @close="emit('close')">
    <div class="stack">
      <p>这是辅助观察投影记录，不代表当前节点异常或计量失败；来源状态不用于判断在线、采样新鲜度或配置确认。</p>
      <p>待处理记录将自动领取或重试；已保留记录等待核对，不会自动重试。此处展示入队元数据，未重新验证保留材料。</p>
      <div class="queue-controls">
        <label>处置状态 <UiSelect :model-value="status" :options="statusOptions" aria-label="观察队列状态" @update:model-value="changeStatus" /></label>
        <UiButton variant="secondary" :loading="loading" @click="read(status)">刷新当前状态</UiButton>
      </div>
      <p v-if="error" role="alert">{{ error }} 已加载记录仍保留，下方标明其状态。</p>
      <p v-if="loading" role="status">正在读取观察队列…</p>
      <template v-if="page">
        <p>已加载状态：{{ statusLabel(page.status) }}。以下时间为入队、调度和处置时间，不是事件采样时间。</p>
        <p v-if="page.status !== status">筛选已改变；当前显示上一次成功加载的记录。</p>
        <div v-if="page.items.length" class="queue-table-wrap">
          <table class="queue-table">
            <caption>全局观察队列元数据</caption>
            <thead><tr><th>来源引用 / 节点编号</th><th>处置原因</th><th>资产关系</th><th>材料 / 尝试</th><th>队列时间</th></tr></thead>
            <tbody><tr v-for="item in page.items" :key="item.id">
              <td><code>{{ item.id }}</code><div>节点编号 {{ item.node_id }} · {{ statusLabel(item.status) }}</div></td>
              <td>{{ reasonLabel(item.reason, item.status) }}</td>
              <td>{{ assetLabel(item.asset_relation) }}</td>
              <td>{{ item.material_bytes }} 字节 / {{ item.part_count }} 段<div>领取尝试 {{ item.attempts }}</div></td>
              <td><div>入队 <TimeBadge :value="item.enqueued_at" /></div><div>调度可用 <TimeBadge :value="item.available_at" /></div><div>处置更新 <TimeBadge :value="item.disposition_updated_at" /></div><div v-if="item.status === 'claimed' && item.lease_until">租约截至 <TimeBadge :value="item.lease_until" /></div></td>
            </tr></tbody>
          </table>
        </div>
        <p v-else>此状态暂无记录。</p>
        <UiButton v-if="page.has_more" variant="secondary" :disabled="loading || page.status !== status" @click="read(page.status, page.next_cursor)">下一页</UiButton>
      </template>
    </div>
  </DetailDrawer>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { fetchObservationIntents, type ObservationIntentPage, type ObservationIntentStatus } from '../api/nodeObservationIntents'
import DetailDrawer from './DetailDrawer.vue'
import TimeBadge from './TimeBadge.vue'
import UiButton from './UiButton.vue'
import UiSelect from './UiSelect.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const status = ref<ObservationIntentStatus>('held')
const page = ref<ObservationIntentPage | null>(null)
const loading = ref(false)
const error = ref('')
let request = 0
const statusOptions = [
  { value: 'held', label: '已保留待核对' }, { value: 'pending', label: '待处理 / 自动重试' },
  { value: 'claimed', label: '已领取' }, { value: 'applied', label: '已处置' },
]
function statusLabel(value: string) { return statusOptions.find(item => item.value === value)?.label || '未知状态' }
const assetLabels: Record<string, string> = { same_asset: '与当前资产标签相同（非来源验证）', different_asset: '与当前资产不同', node_missing: '当前节点已不存在', asset_deleted: '原资产已标记删除', unknown: '资产关系未知' }
function assetLabel(value: string) {
  return Object.prototype.hasOwnProperty.call(assetLabels, value) ? assetLabels[value] : '资产关系未知'
}
const reasons: Record<string, string> = {
  node_busy: '节点暂忙，等待自动重试。', projection_failed: '投影存储失败，等待自动重试。',
  node_inactive: '当时节点未启用或连接凭据已撤销，保留原材料。', node_deleted: '原节点已删除，保留历史材料。', asset_deleted: '原资产已有删除标记，保留历史材料。', asset_mismatch: '材料与当前资产不同，不借用新资产身份。',
  unknown_source: '原来源位置或资产材料不完整，保持未知。', unknown_stats_source: '统计事件来源不完整，保持未知。', unattributed_legacy_floor: '升级导入的无归属历史位置，仅作为边界保留。', unattributed_floor: '材料未越过无归属历史边界，保留待核对。',
  stats_position_conflict: '相同统计位置存在不同数值，保留冲突证据。', invalid_material: '保留材料未通过校验，不自动替换。', invalid_reference_material: '引用材料未通过校验，不自动替换。', invalid_stats_payload: '原统计材料不完整，保留待核对。', apply_byte_budget: '材料超过本次应用算法的字节上限，全部保留。',
}
function reasonLabel(reason: string, state: string) {
  if (Object.prototype.hasOwnProperty.call(reasons, reason)) return reasons[reason]
  if (reason) return '未识别的处置原因，保留原记录供核对。'
  if (state === 'pending') return '等待自动领取。'
  if (state === 'claimed') return '等待拥有有效租约的执行器处置。'
  if (state === 'applied') return '投影流程已处置，不表示重新验证来源或当前新鲜度。'
  return '未记录具体原因，保留待核对。'
}
async function read(selected: ObservationIntentStatus, cursor?: string) {
  if (!props.open) return
  const id = ++request
  loading.value = true
  error.value = ''
  try {
    const result = await fetchObservationIntents(selected, cursor)
    if (id !== request || !props.open || selected !== status.value) return
    page.value = result
  } catch {
    if (id !== request || !props.open) return
    error.value = '观察队列读取失败，请刷新重试。'
  } finally {
    if (id === request) loading.value = false
  }
}
function changeStatus(value: unknown) {
  if (!statusOptions.some(item => item.value === value)) return
  status.value = value as ObservationIntentStatus
  void read(status.value)
}
watch(() => props.open, open => {
  ++request
  loading.value = false
  if (open) void read(status.value)
}, { immediate: true })
onBeforeUnmount(() => { ++request })
</script>

<style scoped>
.queue-controls { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
.queue-controls label { display: grid; gap: 4px; }
.queue-table-wrap { overflow-x: auto; }
.queue-table { width: 100%; border-collapse: collapse; }
.queue-table th, .queue-table td { padding: 8px; text-align: left; vertical-align: top; border-bottom: 1px solid var(--line); }
.queue-table code { overflow-wrap: anywhere; }
</style>
