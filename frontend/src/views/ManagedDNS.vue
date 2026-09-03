<template>
  <section class="standard-page">
    <PageHeader title="DNS 解析" description="管理域名到节点的解析关系；供应商凭据在外部供应商页面独立维护。" eyebrow="Infrastructure">
      <template #actions>
        <PageRefreshButton label="刷新 DNS 解析" :loading="loading || refreshing" @click="refreshAll" />
        <UiButton type="button" :disabled="!activeDNSAccounts.length" @click="openCreateDNS"><UiIcon name="plus" />添加解析</UiButton>
      </template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" success-title="操作已提交" error-title="操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <DataTable v-if="records.length" caption="面板托管的 DNS 解析" :row-count="total" :min-width="980">
        <thead><tr><th>域名</th><th>目标节点</th><th>供应商</th><th>状态</th><th>公共解析</th><th>同步时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="record in records" :key="record.id">
          <td><div class="cell-title"><strong>{{ record.record_type }} {{ record.domain_name }}</strong><span>{{ record.record_value }} · TTL {{ record.ttl === 1 ? '自动' : record.ttl }}<template v-if="record.proxied"> · Cloudflare 代理</template></span></div></td>
          <td><RouterLink :to="`/admin/nodes?node=${record.node_id}`">{{ record.node_name }}</RouterLink></td>
          <td>{{ record.provider_name }}</td>
          <td><StatusBadge :tone="dnsTone(record.status)">{{ dnsStatus(record.status) }}</StatusBadge><small v-if="record.last_error" class="row-error">{{ record.last_error }}</small></td>
          <td><StatusBadge :tone="record.public_resolved ? 'success' : 'warning'">{{ record.public_resolved ? '已观察到' : '自动观察中' }}</StatusBadge></td>
          <td><TimeBadge v-if="record.last_synced_at" :value="record.last_synced_at" mode="relative" /><span v-else class="muted-value">尚未同步</span></td>
          <td class="table-action-column">
            <RowActions :label="`${record.record_type} ${record.domain_name} 的操作`" :trigger-key="`dns-${record.id}`">
              <UiButton size="sm" variant="ghost" :disabled="record.status === 'syncing' || record.status === 'deleting'" @click="openEdit(record)"><UiIcon name="settings" />编辑</UiButton>
              <UiButton size="sm" variant="ghost" :loading="operatingRecord === record.id" :disabled="record.status === 'syncing' || record.status === 'deleting'" @click="syncRecord(record)">同步</UiButton>
              <RouterLink class="button button-ghost button-sm" :to="{ path: '/admin/certificates', query: { dns_domain: record.domain_name, dns_node: record.node_id, dns_provider: record.provider_account_id } }">申请证书</RouterLink>
              <UiButton size="sm" variant="danger" :loading="deletingRecord === record.id" :disabled="record.status === 'syncing'" @click="removeRecord(record)"><UiIcon name="trash" />删除</UiButton>
            </RowActions>
          </td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else class="dns-empty-state" icon="nodes" title="还没有托管 DNS 解析" description="先准备具备 DNS 能力的供应商账户，再把手填域名解析到选定节点。">
        <template #actions><RouterLink class="ui-button ui-button-secondary ui-button-sm" to="/admin/providers">管理供应商账户</RouterLink></template>
      </EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="dnsOpen" title="添加 DNS 解析" description="选择节点后自动读取可公开路由的 IPv4 / IPv6 候选；自动值仍可修改。" :busy="savingDNS" @close="closeCreateDNS">
      <div class="modal-form">
        <FormField label="供应商账户" required><UiSelect v-model.number="dnsForm.provider_account_id" :options="accountOptions" /></FormField>
        <FormField label="目标节点" required>
          <NodeLookup v-model="dnsForm.node_id" />
          <div class="address-discovery-toolbar">
            <UiButton type="button" size="sm" variant="ghost" :loading="createAddressLoading" :disabled="!dnsForm.node_id" @click="loadCreateAddressCandidates"><UiIcon name="refresh" />重新读取节点地址</UiButton>
          </div>
        </FormField>
        <FormField label="完整域名" hint="例如 edge.example.com；域名必须手填。" required full><UiInput v-model.trim="dnsForm.domain_name" placeholder="edge.example.com" /></FormField>
        <FormField label="IPv4 地址（A）" hint="IPv4、IPv6 至少填写一个。">
          <UiInput v-model.trim="dnsForm.ipv4_value" placeholder="例如 1.1.1.1" @update:model-value="markCreateAddressEdited('ipv4')" />
          <div v-if="createAddressCandidates?.ipv4.length" class="address-candidates" aria-label="IPv4 地址候选">
            <button v-for="candidate in createAddressCandidates.ipv4" :key="candidate.address" type="button" class="address-candidate" @click="applyCreateCandidate('ipv4', candidate.address)">
              <span>{{ candidate.address }}</span><small>{{ nodeAddressCandidateSourceLabel(candidate.source) }}</small>
            </button>
          </div>
        </FormField>
        <FormField label="IPv6 地址（AAAA）" hint="同时填写时会在一个请求中创建两条独立记录。">
          <UiInput v-model.trim="dnsForm.ipv6_value" placeholder="例如 2606:4700:4700::1111" @update:model-value="markCreateAddressEdited('ipv6')" />
          <div v-if="createAddressCandidates?.ipv6.length" class="address-candidates" aria-label="IPv6 地址候选">
            <button v-for="candidate in createAddressCandidates.ipv6" :key="candidate.address" type="button" class="address-candidate" @click="applyCreateCandidate('ipv6', candidate.address)">
              <span>{{ candidate.address }}</span><small>{{ nodeAddressCandidateSourceLabel(candidate.source) }}</small>
            </button>
          </div>
        </FormField>
        <div v-if="createAddressError || createAddressCandidates" class="address-discovery-feedback field-full" :class="{ 'is-error': createAddressError }" role="status">
          <p v-if="createAddressError">{{ createAddressError }}</p>
          <template v-else-if="createAddressCandidates">
            <p v-if="!createAddressCandidates.ipv4.length && !createAddressCandidates.ipv6.length">未发现可公开路由的节点地址，请手动填写。</p>
            <p v-else>已按节点字段、DNS 解析和已验证 SSH 网卡地址生成候选。点击候选可明确替换输入值。</p>
            <ul v-if="createAddressCandidates.warnings?.length"><li v-for="warning in createAddressCandidates.warnings" :key="warning">{{ warning }}</li></ul>
          </template>
        </div>
        <FormField label="TTL"><UiNumberInput v-model="dnsForm.ttl" :min="1" :max="86400" /></FormField>
        <FormField label="Cloudflare 代理"><label class="check-field"><UiCheckbox v-model="dnsForm.proxied" /><span>启用橙云代理</span></label></FormField>
        <FormField label="已有记录处理" full><label class="check-field"><UiCheckbox v-model="dnsForm.takeover_existing" /><span>若远端已有同名记录，明确接管并更新</span></label></FormField>
      </div>
      <template #footer><UiButton variant="secondary" @click="closeCreateDNS">取消</UiButton><UiButton type="button" :loading="savingDNS" @click="createDNS">创建并同步</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" title="编辑 DNS 解析" description="现有记录值不会被自动覆盖；可按需读取节点候选并明确选择替换。" :busy="savingEdit" @close="closeEditDNS">
      <div class="modal-form">
        <FormField label="解析记录" full><UiInput :model-value="`${editForm.record_type} ${editForm.domain_name}`" disabled /></FormField>
        <FormField label="目标节点" required>
          <NodeLookup v-model="editForm.node_id" />
          <div class="address-discovery-toolbar">
            <UiButton type="button" size="sm" variant="ghost" :loading="editAddressLoading" :disabled="!editForm.node_id" @click="loadEditAddressCandidates"><UiIcon name="refresh" />读取节点地址</UiButton>
          </div>
        </FormField>
        <FormField :label="editForm.record_type === 'AAAA' ? 'IPv6 地址' : 'IPv4 地址'" required>
          <UiInput v-model.trim="editForm.record_value" />
          <div v-if="editAddressCandidateItems.length" class="address-candidates" :aria-label="`${editForm.record_type} 地址候选`">
            <button v-for="candidate in editAddressCandidateItems" :key="candidate.address" type="button" class="address-candidate" @click="editForm.record_value = candidate.address">
              <span>{{ candidate.address }}</span><small>{{ nodeAddressCandidateSourceLabel(candidate.source) }}</small>
            </button>
          </div>
        </FormField>
        <div v-if="editAddressError || editAddressCandidates" class="address-discovery-feedback field-full" :class="{ 'is-error': editAddressError }" role="status">
          <p v-if="editAddressError">{{ editAddressError }}</p>
          <template v-else-if="editAddressCandidates">
            <p v-if="!editAddressCandidateItems.length">未发现与 {{ editForm.record_type }} 匹配的公开地址候选，当前记录值保持不变。</p>
            <p v-else>当前记录值保持不变；点击候选后才会替换。</p>
            <ul v-if="editAddressCandidates.warnings?.length"><li v-for="warning in editAddressCandidates.warnings" :key="warning">{{ warning }}</li></ul>
          </template>
        </div>
        <FormField label="TTL"><UiNumberInput v-model="editForm.ttl" :min="1" :max="86400" /></FormField>
        <FormField label="Cloudflare 代理"><label class="check-field"><UiCheckbox v-model="editForm.proxied" /><span>启用橙云代理</span></label></FormField>
      </div>
      <template #footer><UiButton variant="secondary" @click="closeEditDNS">取消</UiButton><UiButton type="button" :loading="savingEdit" @click="saveEdit">保存并同步</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  createManagedDNSRecord,
  deleteManagedDNSRecord,
  fetchManagedDNSRecordsPage,
  fetchNodeAddressCandidates,
  fetchProviderAccounts,
  syncManagedDNSRecord,
  updateManagedDNSRecord,
  type ManagedDNSRecord,
  type NodeAddressCandidate,
  type NodeAddressCandidates,
  type ProviderAccount,
} from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import NodeLookup from '../components/NodeLookup.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiIcon from '../components/UiIcon.vue'
import UiInput from '../components/UiInput.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiSelect from '../components/UiSelect.vue'
import { confirmAction } from '../utils/feedback'
import {
  applyRecommendedNodeAddress,
  clearPreviousSuggestedAddress,
  nodeAddressCandidateSourceLabel,
} from '../utils/managedDNSAddressSuggestions'

const accounts = ref<ProviderAccount[]>([])
const records = ref<ManagedDNSRecord[]>([])
const total = ref(0)
const offset = ref(0)
const limit = ref(50)
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const message = ref('')
const dnsOpen = ref(false)
const savingDNS = ref(false)
const operatingRecord = ref(0)
const deletingRecord = ref(0)
const editOpen = ref(false)
const savingEdit = ref(false)
const editForm = reactive({ id: 0, provider_account_id: 0, node_id: 0, domain_name: '', record_type: 'A' as 'A' | 'AAAA', record_value: '', ttl: 1, proxied: false, revision: 0 })
const dnsForm = reactive({ provider_account_id: 0, node_id: 0, domain_name: '', ipv4_value: '', ipv6_value: '', ttl: 1, proxied: false, takeover_existing: false })
const activeDNSAccounts = computed(() => accounts.value.filter(item => item.status === 'active' && item.capabilities.includes('dns.records')))
const accountOptions = computed(() => activeDNSAccounts.value.map(item => ({ label: item.name, value: item.id })))
const createAddressCandidates = ref<NodeAddressCandidates | null>(null)
const createAddressLoading = ref(false)
const createAddressError = ref('')
const editAddressCandidates = ref<NodeAddressCandidates | null>(null)
const editAddressLoading = ref(false)
const editAddressError = ref('')
const editAddressCandidateItems = computed<NodeAddressCandidate[]>(() => editForm.record_type === 'AAAA' ? editAddressCandidates.value?.ipv6 || [] : editAddressCandidates.value?.ipv4 || [])
const createAddressState = reactive({ ipv4Manual: false, ipv6Manual: false, ipv4Suggested: '', ipv6Suggested: '' })
let pollTimer: ReturnType<typeof setInterval> | undefined
let createAddressController: AbortController | null = null
let editAddressController: AbortController | null = null
let createAddressRequest = 0
let editAddressRequest = 0

function dnsStatus(status: string) { return ({ pending: '待同步', syncing: '同步中', active: '已同步', drifted: '存在漂移', failed: '同步失败', deleting: '待完成删除' } as Record<string, string>)[status] || status }
function dnsTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'active' ? 'success' : status === 'failed' ? 'danger' : status === 'syncing' || status === 'drifted' ? 'warning' : 'neutral' }
function openCreateDNS() { dnsOpen.value = true; if (dnsForm.node_id) void loadCreateAddressCandidates() }
function closeCreateDNS() { createAddressController?.abort(); dnsOpen.value = false }
function closeEditDNS() { editAddressController?.abort(); editOpen.value = false }
function resetCreateAddressState() {
  createAddressController?.abort()
  createAddressCandidates.value = null
  createAddressError.value = ''
  Object.assign(createAddressState, { ipv4Manual: false, ipv6Manual: false, ipv4Suggested: '', ipv6Suggested: '' })
}
function markCreateAddressEdited(family: 'ipv4' | 'ipv6') {
  if (family === 'ipv4') Object.assign(createAddressState, { ipv4Manual: true, ipv4Suggested: '' })
  else Object.assign(createAddressState, { ipv6Manual: true, ipv6Suggested: '' })
}
function applyCreateCandidate(family: 'ipv4' | 'ipv6', address: string) {
  if (family === 'ipv4') {
    dnsForm.ipv4_value = address
    Object.assign(createAddressState, { ipv4Manual: true, ipv4Suggested: '' })
  } else {
    dnsForm.ipv6_value = address
    Object.assign(createAddressState, { ipv6Manual: true, ipv6Suggested: '' })
  }
}

async function refreshAll() {
  refreshing.value = true
  error.value = ''
  try {
    const [providerAccounts, page] = await Promise.all([fetchProviderAccounts(), fetchManagedDNSRecordsPage({ offset: offset.value, limit: limit.value })])
    accounts.value = providerAccounts
    records.value = page.items
    total.value = page.total
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || 'DNS 解析数据加载失败。'
  } finally {
    loading.value = false
    refreshing.value = false
  }
}
async function loadCreateAddressCandidates() {
  const nodeID = dnsForm.node_id
  if (!nodeID) return
  createAddressController?.abort()
  const controller = new AbortController()
  const request = ++createAddressRequest
  createAddressController = controller
  createAddressLoading.value = true
  createAddressError.value = ''
  try {
    const result = await fetchNodeAddressCandidates(nodeID, { signal: controller.signal })
    if (request !== createAddressRequest || nodeID !== dnsForm.node_id) return
    createAddressCandidates.value = result
    const nextIPv4 = applyRecommendedNodeAddress(dnsForm.ipv4_value, createAddressState.ipv4Manual, result.recommended_ipv4)
    const nextIPv6 = applyRecommendedNodeAddress(dnsForm.ipv6_value, createAddressState.ipv6Manual, result.recommended_ipv6)
    if (nextIPv4 !== dnsForm.ipv4_value) { dnsForm.ipv4_value = nextIPv4; createAddressState.ipv4Suggested = nextIPv4 }
    if (nextIPv6 !== dnsForm.ipv6_value) { dnsForm.ipv6_value = nextIPv6; createAddressState.ipv6Suggested = nextIPv6 }
  } catch (cause: any) {
    if (request === createAddressRequest && !controller.signal.aborted) {
      createAddressCandidates.value = null
      createAddressError.value = cause?.response?.data?.message || '节点地址读取失败，请手动填写或稍后重试。'
    }
  } finally {
    if (request === createAddressRequest) createAddressLoading.value = false
  }
}
async function loadEditAddressCandidates() {
  const nodeID = editForm.node_id
  if (!nodeID) return
  editAddressController?.abort()
  const controller = new AbortController()
  const request = ++editAddressRequest
  editAddressController = controller
  editAddressLoading.value = true
  editAddressError.value = ''
  try {
    const result = await fetchNodeAddressCandidates(nodeID, { signal: controller.signal })
    if (request !== editAddressRequest || nodeID !== editForm.node_id) return
    editAddressCandidates.value = result
  } catch (cause: any) {
    if (request === editAddressRequest && !controller.signal.aborted) {
      editAddressCandidates.value = null
      editAddressError.value = cause?.response?.data?.message || '节点地址读取失败，当前记录值保持不变。'
    }
  } finally {
    if (request === editAddressRequest) editAddressLoading.value = false
  }
}
async function createDNS() {
  savingDNS.value = true
  error.value = ''
  message.value = ''
  try {
    const records = [
      ...(dnsForm.ipv4_value ? [{ record_type: 'A' as const, record_value: dnsForm.ipv4_value }] : []),
      ...(dnsForm.ipv6_value ? [{ record_type: 'AAAA' as const, record_value: dnsForm.ipv6_value }] : []),
    ]
    if (!records.length) {
      error.value = '请至少填写一个 IPv4 或 IPv6 地址。'
      return
    }
    await createManagedDNSRecord({ ...dnsForm, records })
    dnsOpen.value = false
    Object.assign(dnsForm, { provider_account_id: 0, node_id: 0, domain_name: '', ipv4_value: '', ipv6_value: '', ttl: 1, proxied: false, takeover_existing: false })
    resetCreateAddressState()
    message.value = 'DNS 记录已创建，Cloudflare 同步与公共传播观察正在后台执行。'
    await refreshAll()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || 'DNS 记录创建失败。'
  } finally {
    savingDNS.value = false
  }
}
async function syncRecord(record: ManagedDNSRecord) {
  operatingRecord.value = record.id
  error.value = ''
  try {
    const takeover = record.status === 'failed' && record.last_error.includes('明确选择接管')
    await syncManagedDNSRecord(record.id, takeover)
    message.value = `${record.domain_name} 已开始同步。`
    await refreshAll()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || 'DNS 同步启动失败。'
  } finally {
    operatingRecord.value = 0
  }
}
function openEdit(record: ManagedDNSRecord) {
  editAddressController?.abort()
  editAddressCandidates.value = null
  editAddressError.value = ''
  Object.assign(editForm, { id: record.id, provider_account_id: record.provider_account_id, node_id: record.node_id, domain_name: record.domain_name, record_type: record.record_type, record_value: record.record_value, ttl: record.ttl, proxied: record.proxied, revision: record.revision })
  editOpen.value = true
}
async function saveEdit() {
  savingEdit.value = true
  error.value = ''
  message.value = ''
  try {
    await updateManagedDNSRecord(editForm.id, { provider_account_id: editForm.provider_account_id, node_id: editForm.node_id, domain_name: editForm.domain_name, record_type: editForm.record_type, record_value: editForm.record_value, ttl: editForm.ttl, proxied: editForm.proxied, expected_revision: editForm.revision })
    editOpen.value = false
    message.value = `${editForm.domain_name} 已更新并开始同步。`
    await refreshAll()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || 'DNS 记录更新失败。'
  } finally {
    savingEdit.value = false
  }
}
async function removeRecord(record: ManagedDNSRecord) {
  if (!await confirmAction({ title: '删除 DNS 解析？', message: `将先从 Cloudflare 删除 ${record.record_type} ${record.domain_name}，远端删除成功后才会移除面板记录。`, confirmText: '确认删除', tone: 'danger' })) return
  deletingRecord.value = record.id
  error.value = ''
  message.value = ''
  try {
    await deleteManagedDNSRecord(record.id)
    message.value = `${record.record_type} ${record.domain_name} 已从 Cloudflare 和面板删除。`
    await refreshAll()
  } catch (cause: any) {
    await refreshAll()
    error.value = cause?.response?.data?.message || 'DNS 记录删除失败。'
  } finally {
    deletingRecord.value = 0
  }
}
async function changePage(next: { offset: number; limit: number }) {
  offset.value = next.offset
  limit.value = next.limit
  await refreshAll()
}

watch(() => dnsForm.node_id, (nodeID, previousNodeID) => {
  if (nodeID === previousNodeID) return
  createAddressCandidates.value = null
  createAddressError.value = ''
  dnsForm.ipv4_value = clearPreviousSuggestedAddress(dnsForm.ipv4_value, createAddressState.ipv4Suggested, createAddressState.ipv4Manual)
  dnsForm.ipv6_value = clearPreviousSuggestedAddress(dnsForm.ipv6_value, createAddressState.ipv6Suggested, createAddressState.ipv6Manual)
  createAddressState.ipv4Suggested = ''
  createAddressState.ipv6Suggested = ''
  if (dnsOpen.value && nodeID) void loadCreateAddressCandidates()
})
watch(() => editForm.node_id, (nodeID, previousNodeID) => {
  if (nodeID === previousNodeID) return
  editAddressController?.abort()
  editAddressCandidates.value = null
  editAddressError.value = ''
})

onMounted(async () => {
  loading.value = true
  await refreshAll()
  pollTimer = setInterval(() => { if (records.value.some(item => item.status === 'syncing' || !item.public_resolved)) void refreshAll() }, 5000)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
  createAddressController?.abort()
  editAddressController?.abort()
})
</script>

<style scoped>
.dns-empty-state { min-height: 180px; padding: 28px; }
.row-error { display: block; max-width: 260px; margin-top: 4px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.muted-value { color: var(--muted); }
.modal-form { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; padding: 20px; }
.modal-form > :deep(.field-full), .modal-form > .field-full { grid-column: 1 / -1; }
.check-field { min-height: var(--control-height); display: flex; align-items: center; gap: 8px; }
.address-discovery-toolbar { display: flex; justify-content: flex-end; margin-top: 7px; }
.address-candidates { display: grid; gap: 6px; margin-top: 8px; }
.address-candidate { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; padding: 7px 9px; border: 1px solid var(--line); border-radius: 8px; color: var(--text); background: var(--surface-soft); cursor: pointer; text-align: left; }
.address-candidate:hover { border-color: var(--primary); background: var(--primary-soft); }
.address-candidate span { min-width: 0; overflow-wrap: anywhere; font-family: var(--font-mono); font-size: 11px; }
.address-candidate small { flex: 0 0 auto; color: var(--muted); font-size: 9px; }
.address-discovery-feedback { padding: 10px 12px; border: 1px solid var(--line); border-radius: 9px; color: var(--muted); background: var(--surface-soft); font-size: 11px; }
.address-discovery-feedback.is-error { border-color: color-mix(in srgb, var(--danger) 35%, var(--line)); color: var(--danger); background: var(--danger-soft); }
.address-discovery-feedback p { margin: 0; }
.address-discovery-feedback ul { display: grid; gap: 3px; margin: 6px 0 0; padding-left: 18px; }
@media (max-width: 720px) {
  .modal-form { grid-template-columns: 1fr; }
  .modal-form > * { grid-column: 1; }
}
</style>
