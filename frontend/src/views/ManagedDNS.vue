<template>
  <section class="standard-page">
    <PageHeader title="DNS 解析" description="管理域名解析记录及其同步状态。">
      <template #actions>
        <PageRefreshButton label="刷新 DNS 解析" :loading="loading || refreshing" @click="refreshAll" />
        <UiButton type="button" :disabled="!activeDNSAccounts.length" @click="openCreateDNS"><UiIcon name="plus" />添加解析</UiButton>
      </template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" success-title="操作已提交" error-title="操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || statusFilter)" @clear="clearFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="域名" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="statusFilter" label="同步状态" :options="statusOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <TableSkeleton v-if="(loading || refreshing) && !records.length" label="正在加载 DNS 解析" :columns="7" />
      <DataTable v-else-if="records.length" caption="面板托管的 DNS 解析" :row-count="total" :min-width="980">
        <thead><tr><th class="table-primary-column">域名</th><th data-column-priority="2">目标节点</th><th data-column-priority="2">供应商</th><th>状态</th><th data-column-priority="3">公共解析</th><th data-column-priority="2">同步时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="record in records" :key="record.id">
          <td class="table-primary-column"><div class="cell-title"><strong>{{ record.record_type }} {{ record.domain_name }}</strong><EndpointAddress :address="record.record_value" /><span>TTL {{ record.ttl === 1 ? '自动' : record.ttl }}<template v-if="record.proxied"> · Cloudflare 代理</template></span></div></td>
          <td data-column-priority="2"><RouterLink class="table-secondary-text" :title="record.node_name || `VPS #${record.node_id}`" :to="`/admin/nodes?node=${record.node_id}`">{{ record.node_name || `VPS #${record.node_id}` }}</RouterLink></td>
          <td data-column-priority="2"><TableText :value="record.provider_name" /></td>
          <td><StatusBadge :tone="dnsTone(record.status)">{{ dnsStatus(record.status) }}</StatusBadge><small v-if="record.last_error" class="row-error" :title="record.last_error">{{ record.last_error }}</small></td>
          <td data-column-priority="3"><StatusBadge :tone="record.public_resolved ? 'success' : 'warning'">{{ record.public_resolved ? '已观察到' : '自动观察中' }}</StatusBadge></td>
          <td data-column-priority="2"><TimeBadge v-if="record.last_synced_at" :value="record.last_synced_at" mode="relative" /><span v-else class="muted-value">尚未同步</span></td>
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
      <EmptyState v-else class="dns-empty-state" icon="nodes" :title="search || statusFilter ? '没有匹配的解析记录' : '还没有 DNS 解析'" :description="search || statusFilter ? '调整搜索词或同步状态后重试。' : '添加解析后，可以在这里查看同步和公共解析状态。'">
        <template #actions><UiButton v-if="search || statusFilter" type="button" variant="secondary" size="sm" @click="clearFilters">清除筛选</UiButton><RouterLink v-else class="ui-button ui-button-secondary ui-button-sm" to="/admin/providers">管理供应商账户</RouterLink></template>
      </EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="dnsOpen" title="添加 DNS 解析" description="选择节点后可使用推荐地址，也可以自行填写。" :busy="savingDNS" :dirty="createState.dirty.value" @close="closeCreateDNS">
      <form id="dns-create-form" ref="createFormElement" class="modal-form" novalidate @submit.prevent="createDNS">
        <PageAlert v-if="createErrors.formError.value" tone="danger" title="无法添加解析" class="field-full">{{ createErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="供应商账户" name="dns-provider" :error="createErrors.fields.provider_account_id" required><UiSelect v-model.number="dnsForm.provider_account_id" v-bind="controlAttrs" :options="accountOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="目标节点" name="dns-node" :error="createErrors.fields.node_id" required>
          <NodeLookup v-model="dnsForm.node_id" v-bind="controlAttrs" />
          <div class="address-discovery-toolbar">
            <UiButton type="button" size="sm" variant="ghost" :loading="createAddressLoading" :disabled="!dnsForm.node_id" @click="loadCreateAddressCandidates"><UiIcon name="refresh" />重新读取节点地址</UiButton>
          </div>
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="完整域名" name="dns-domain" hint="例如 edge.example.com" :error="createErrors.fields.domain_name" required full><UiInput v-model.trim="dnsForm.domain_name" v-bind="controlAttrs" placeholder="edge.example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="IPv4 地址（A）" name="dns-ipv4" hint="IPv4、IPv6 至少填写一个。" :error="createErrors.fields.ipv4_value">
          <UiInput v-model.trim="dnsForm.ipv4_value" v-bind="controlAttrs" placeholder="例如 1.1.1.1" @update:model-value="markCreateAddressEdited('ipv4')" />
          <div v-if="createAddressCandidates?.ipv4.length" class="address-candidates" aria-label="IPv4 地址候选">
            <button v-for="candidate in createAddressCandidates.ipv4" :key="candidate.address" type="button" class="address-candidate" @click="applyCreateCandidate('ipv4', candidate.address)">
              <span>{{ candidate.address }}</span><small>{{ nodeAddressCandidateSourceLabel(candidate.source) }}</small>
            </button>
          </div>
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="IPv6 地址（AAAA）" name="dns-ipv6" hint="同时填写时会创建两条记录。" :error="createErrors.fields.ipv6_value">
          <UiInput v-model.trim="dnsForm.ipv6_value" v-bind="controlAttrs" placeholder="例如 2606:4700:4700::1111" @update:model-value="markCreateAddressEdited('ipv6')" />
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
        <FormField v-slot="{ controlAttrs }" label="TTL" name="dns-ttl" hint="1 为自动；手动设置需为 60–86400 秒。" :error="createErrors.fields.ttl"><UiNumberInput v-model="dnsForm.ttl" v-bind="controlAttrs" :min="1" :max="86400" /></FormField>
        <FormField label="Cloudflare 代理"><label class="check-field"><UiCheckbox v-model="dnsForm.proxied" /><span>启用橙云代理</span></label></FormField>
        <FormField label="已有记录处理" full><label class="check-field"><UiCheckbox v-model="dnsForm.takeover_existing" /><span>若远端已有同名记录，明确接管并更新</span></label></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" @click="requestClose">取消</UiButton><UiButton type="submit" form="dns-create-form" :loading="savingDNS">创建并同步</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" title="编辑 DNS 解析" description="现有记录值不会被自动覆盖；可按需读取节点候选并明确选择替换。" :busy="savingEdit" :dirty="editState.dirty.value" @close="closeEditDNS">
      <form id="dns-edit-form" ref="editFormElement" class="modal-form" novalidate @submit.prevent="saveEdit">
        <PageAlert v-if="editErrors.formError.value" tone="danger" title="无法保存解析" class="field-full">{{ editErrors.formError.value }}</PageAlert>
        <FormField label="解析记录" full><UiInput :model-value="`${editForm.record_type} ${editForm.domain_name}`" disabled /></FormField>
        <FormField v-slot="{ controlAttrs }" label="目标节点" name="edit-dns-node" :error="editErrors.fields.node_id" required>
          <NodeLookup v-model="editForm.node_id" v-bind="controlAttrs" />
          <div class="address-discovery-toolbar">
            <UiButton type="button" size="sm" variant="ghost" :loading="editAddressLoading" :disabled="!editForm.node_id" @click="loadEditAddressCandidates"><UiIcon name="refresh" />读取节点地址</UiButton>
          </div>
        </FormField>
        <FormField v-slot="{ controlAttrs }" :label="editForm.record_type === 'AAAA' ? 'IPv6 地址' : 'IPv4 地址'" name="edit-dns-address" :error="editErrors.fields.record_value" required>
          <UiInput v-model.trim="editForm.record_value" v-bind="controlAttrs" />
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
        <FormField v-slot="{ controlAttrs }" label="TTL" name="edit-dns-ttl" hint="1 为自动；手动设置需为 60–86400 秒。" :error="editErrors.fields.ttl"><UiNumberInput v-model="editForm.ttl" v-bind="controlAttrs" :min="1" :max="86400" /></FormField>
        <FormField label="Cloudflare 代理"><label class="check-field"><UiCheckbox v-model="editForm.proxied" /><span>启用橙云代理</span></label></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" @click="requestClose">取消</UiButton><UiButton type="submit" form="dns-edit-form" :loading="savingEdit">保存并同步</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import EndpointAddress from '../components/EndpointAddress.vue'
import TableText from '../components/TableText.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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
import TableSkeleton from '../components/TableSkeleton.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import NodeLookup from '../components/NodeLookup.vue'
import PageAlert from '../components/PageAlert.vue'
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
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { confirmAction } from '../utils/feedback'
import { preserveAdminReturnTo } from '../utils/navigation'
import { collectFieldErrors, isIntegerInRange } from '../utils/validation'
import {
  applyRecommendedNodeAddress,
  clearPreviousSuggestedAddress,
  nodeAddressCandidateSourceLabel,
} from '../utils/managedDNSAddressSuggestions'

const route = useRoute()
const router = useRouter()
function queryLimit(value: unknown) { const parsed = Number(value); return [25, 50, 100].includes(parsed) ? parsed : 50 }
function queryPage(value: unknown) { const parsed = Number(value); return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 1 }
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const statusFilter = ref(typeof route.query.status === 'string' ? route.query.status : '')
const accounts = ref<ProviderAccount[]>([])
const records = ref<ManagedDNSRecord[]>([])
const total = ref(0)
const limit = ref(queryLimit(route.query.limit))
const offset = ref((queryPage(route.query.page) - 1) * limit.value)
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
const createFormElement = ref<HTMLElement | null>(null)
const editFormElement = ref<HTMLElement | null>(null)
const createErrors = useFormErrors()
const editErrors = useFormErrors()
const createState = useDirtyForm(() => dnsForm)
const editState = useDirtyForm(() => editForm)
useUnsavedChangesGuard(
  () => dnsOpen.value && createState.dirty.value || editOpen.value && editState.dirty.value,
  () => dnsOpen.value ? createState.confirmDiscard() : editState.confirmDiscard(),
)
for (const field of ['provider_account_id', 'node_id', 'domain_name', 'ipv4_value', 'ipv6_value', 'ttl'] as const) watch(() => dnsForm[field], () => createErrors.clear(field))
for (const field of ['node_id', 'record_value', 'ttl'] as const) watch(() => editForm[field], () => editErrors.clear(field))
const activeDNSAccounts = computed(() => accounts.value.filter(item => item.status === 'active' && item.capabilities.includes('dns.records')))
const accountOptions = computed(() => activeDNSAccounts.value.map(item => ({ label: item.name, value: item.id })))
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '待同步', value: 'pending' },
  { label: '同步中', value: 'syncing' },
  { label: '已同步', value: 'active' },
  { label: '存在漂移', value: 'drifted' },
  { label: '同步失败', value: 'failed' },
  { label: '待完成删除', value: 'deleting' },
]
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
function openCreateDNS() { createErrors.clear(); createState.markClean(); dnsOpen.value = true; if (dnsForm.node_id) void loadCreateAddressCandidates() }
function closeCreateDNS() { createAddressController?.abort(); dnsOpen.value = false; Object.assign(dnsForm, { provider_account_id: 0, node_id: 0, domain_name: '', ipv4_value: '', ipv6_value: '', ttl: 1, proxied: false, takeover_existing: false }); resetCreateAddressState(); createErrors.clear() }
function closeEditDNS() { editAddressController?.abort(); editOpen.value = false; editErrors.clear() }
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

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const query = {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(search.value ? { q: search.value } : {}),
    ...(statusFilter.value ? { status: statusFilter.value } : {}),
    ...(page > 1 ? { page: String(page) } : {}),
    ...(limit.value !== 50 ? { limit: String(limit.value) } : {}),
  }
  await (replace ? router.replace({ query }) : router.push({ query }))
}
async function applyFilters() { offset.value = 0; await syncURL(); await refreshAll() }
async function clearFilters() { search.value = ''; statusFilter.value = ''; await applyFilters() }

async function refreshAll() {
  refreshing.value = true
  error.value = ''
  try {
    const [providerAccounts, page] = await Promise.all([fetchProviderAccounts(), fetchManagedDNSRecordsPage({ offset: offset.value, limit: limit.value, q: search.value || undefined, status: statusFilter.value || undefined })])
    accounts.value = providerAccounts
    records.value = page.items
    total.value = page.total
    if (page.total > 0 && offset.value >= page.total) {
      offset.value = Math.floor((page.total - 1) / limit.value) * limit.value
      await syncURL(true)
      await refreshAll()
    }
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
  const valid = await createErrors.applyValidation(collectFieldErrors({
    provider_account_id: !dnsForm.provider_account_id && '请选择供应商账户。',
    node_id: !dnsForm.node_id && '请选择目标节点。',
    domain_name: !dnsForm.domain_name.trim() && '请输入完整域名。',
    ipv4_value: !dnsForm.ipv4_value.trim() && !dnsForm.ipv6_value.trim() && '请至少填写一个 IPv4 或 IPv6 地址。',
    ttl: dnsForm.ttl !== 1 && !isIntegerInRange(dnsForm.ttl, 60, 86400) && 'TTL 应为自动（1）或 60–86400 秒。',
  }), createFormElement, '')
  if (!valid) return
  savingDNS.value = true
  error.value = ''
  message.value = ''
  try {
    const records = [
      ...(dnsForm.ipv4_value ? [{ record_type: 'A' as const, record_value: dnsForm.ipv4_value }] : []),
      ...(dnsForm.ipv6_value ? [{ record_type: 'AAAA' as const, record_value: dnsForm.ipv6_value }] : []),
    ]
    await createManagedDNSRecord({ ...dnsForm, records })
    dnsOpen.value = false
    Object.assign(dnsForm, { provider_account_id: 0, node_id: 0, domain_name: '', ipv4_value: '', ipv6_value: '', ttl: 1, proxied: false, takeover_existing: false })
    resetCreateAddressState()
    message.value = 'DNS 记录已创建，Cloudflare 同步与公共传播观察正在后台执行。'
    await refreshAll()
  } catch (cause: any) {
    await createErrors.applyApiError(cause, 'DNS 记录创建失败。', createFormElement, {
      provider_account_id: 'provider_account_id', node_id: 'node_id', domain_name: 'domain_name', ttl: 'ttl',
      'records.0.record_value': dnsForm.ipv4_value ? 'ipv4_value' : 'ipv6_value',
      'records.1.record_value': 'ipv6_value',
    })
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
  editErrors.clear()
  editState.markClean()
  editOpen.value = true
}
async function saveEdit() {
  const valid = await editErrors.applyValidation(collectFieldErrors({
    node_id: !editForm.node_id && '请选择目标节点。',
    record_value: !editForm.record_value.trim() && '请输入解析地址。',
    ttl: editForm.ttl !== 1 && !isIntegerInRange(editForm.ttl, 60, 86400) && 'TTL 应为自动（1）或 60–86400 秒。',
  }), editFormElement, '')
  if (!valid) return
  savingEdit.value = true
  error.value = ''
  message.value = ''
  try {
    await updateManagedDNSRecord(editForm.id, { provider_account_id: editForm.provider_account_id, node_id: editForm.node_id, domain_name: editForm.domain_name, record_type: editForm.record_type, record_value: editForm.record_value, ttl: editForm.ttl, proxied: editForm.proxied, expected_revision: editForm.revision })
    editOpen.value = false
    message.value = `${editForm.domain_name} 已更新并开始同步。`
    await refreshAll()
  } catch (cause: any) {
    await editErrors.applyApiError(cause, 'DNS 记录更新失败。', editFormElement, { node_id: 'node_id', record_value: 'record_value', ttl: 'ttl' })
  } finally {
    savingEdit.value = false
  }
}
async function removeRecord(record: ManagedDNSRecord) {
  if (!await confirmAction({ title: '删除 DNS 解析？', message: `将删除 ${record.record_type} ${record.domain_name} 的面板管理记录；不调用供应商 API，Cloudflare 上的解析保留，可在供应商控制台单独删除。`, confirmText: '确认删除', tone: 'danger' })) return
  deletingRecord.value = record.id
  error.value = ''
  message.value = ''
  try {
    await deleteManagedDNSRecord(record.id)
    message.value = `${record.record_type} ${record.domain_name} 的面板记录已删除，Cloudflare 解析保留。`
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
  await syncURL()
  await refreshAll()
}

watch(() => route.fullPath, async () => {
  const nextLimit = queryLimit(route.query.limit)
  const nextSearch = typeof route.query.q === 'string' ? route.query.q : ''
  const nextStatus = typeof route.query.status === 'string' ? route.query.status : ''
  const nextOffset = (queryPage(route.query.page) - 1) * nextLimit
  if (nextSearch !== search.value || nextStatus !== statusFilter.value || nextOffset !== offset.value || nextLimit !== limit.value) {
    search.value = nextSearch
    statusFilter.value = nextStatus
    limit.value = nextLimit
    offset.value = nextOffset
    await refreshAll()
  }
})

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
