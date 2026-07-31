<template>
  <section class="standard-page">
    <PageHeader title="免费证书" description="在节点上申请并保存 Let's Encrypt 证书，绑定协议服务后自动注入 Zero 配置，并在到期窗口内续期。" eyebrow="Infrastructure">
      <template #actions><PageRefreshButton label="刷新证书" :loading="loading" @click="refresh" /><UiButton type="button" @click="openCreate"><UiIcon name="plus" />申请证书</UiButton></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="证书操作已提交" error-title="证书操作失败" />

    <PageAlert tone="info" title="ACME 验证方式">
      默认通过 Cloudflare DNS-01 自动完成验证，不占用节点端口；已有 Web 服务时也可选择 HTTP-01 Webroot，由现有服务在公网 80 端口提供挑战文件。证书私钥始终留在节点上。
    </PageAlert>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters><WorkbenchFilterBar :active="Boolean(search || statusFilter)" @clear="clearFilters"><WorkbenchFilterInput v-model="search" label="搜索" placeholder="证书名称或域名" @apply="applyFilters" /><WorkbenchFilterSelect v-model="statusFilter" label="证书状态" :options="statusOptions" @apply="applyFilters" /></WorkbenchFilterBar></template>
      <DataTable v-if="certificates.length" caption="免费证书列表；证书材料保存在目标节点，面板仅展示公开元数据和运行状态" :row-count="total" :min-width="1080" table-class="certificate-table">
        <thead><tr><th class="table-primary-column">证书</th><th data-column-priority="2">节点</th><th>状态</th><th data-column-priority="2">签发环境</th><th>到期时间</th><th data-column-priority="3">自动续期</th><th class="numeric-column" data-column-priority="3">使用数</th><th data-column-priority="3">最近操作</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="certificate in certificates" :key="certificate.id">
          <td class="table-primary-column"><div class="cell-title"><strong>{{ certificate.name }}</strong><span>{{ certificate.domains.join('、') }}</span></div></td>
          <td data-column-priority="2"><RouterLink :to="`/admin/nodes?node=${certificate.node_id}`">{{ certificate.node_name || `VPS #${certificate.node_id}` }}</RouterLink></td>
          <td><StatusBadge :tone="certificateTone(certificate.status)" :icon="certificateIcon(certificate.status)">{{ certificateStatusLabel(certificate.status) }}</StatusBadge><small v-if="certificate.last_error" class="certificate-error" :title="certificate.last_error">{{ certificate.last_error }}</small></td>
          <td data-column-priority="2"><StatusBadge :tone="certificate.environment === 'production' ? 'info' : 'warning'">{{ certificate.environment === 'production' ? '生产证书' : '测试证书' }}</StatusBadge></td>
          <td><TimeBadge v-if="certificate.not_after" :value="certificate.not_after" :tone="expiryTone(certificate)" /><span v-else class="muted-value">尚未签发</span></td>
          <td data-column-priority="3"><span>{{ certificate.auto_renew ? `提前 ${certificate.renew_before_days} 天` : '已关闭' }}</span><small v-if="certificate.next_renewal_at"><TimeBadge :value="certificate.next_renewal_at" mode="relative" /></small></td>
          <td class="numeric-column" data-column-priority="3">{{ certificate.usage_count }}</td>
          <td data-column-priority="3"><div v-if="certificate.latest_operation" class="operation-cell"><StatusBadge :tone="operationTone(certificate.latest_operation.status)">{{ operationLabel(certificate.latest_operation) }}</StatusBadge><TimeBadge :value="certificate.latest_operation.created_at" mode="relative" /></div><span v-else class="muted-value">暂无操作</span></td>
          <td class="table-action-column"><RowActions :label="`${certificate.name} 的操作`" :trigger-key="`certificate-${certificate.id}`">
            <UiButton variant="secondary" size="sm" type="button" :disabled="operationRunning(certificate)" @click="openRenewal(certificate)"><UiIcon name="settings" />续期策略</UiButton>
            <UiButton size="sm" type="button" :loading="operatingID === certificate.id" :disabled="operationRunning(certificate)" @click="runCertificateOperation(certificate)"><UiIcon name="refresh" />{{ certificate.not_after ? '立即续期' : '开始申请' }}</UiButton>
            <UiButton variant="danger" size="sm" type="button" :loading="deletingID === certificate.id" :disabled="operationRunning(certificate)" @click="removeCertificate(certificate)"><UiIcon name="trash" />删除</UiButton>
          </RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else icon="shield" title="还没有托管证书" description="创建证书资产后，系统会在目标节点完成申请、保存和后续自动续期。"><template #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />申请第一张证书</UiButton></template></EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="createOpen" :dirty="createState.dirty.value" title="申请免费证书" description="创建资产后按所选 ACME 验证方式立即发起签发。" size="lg" :busy="saving" @close="closeCreate">
      <form id="certificate-create-form" ref="createFormElement" class="certificate-form" novalidate @submit.prevent="create">
        <PageAlert v-if="createErrors.formError.value" tone="danger" title="无法创建证书">{{ createErrors.formError.value }}</PageAlert>
        <FormField label="目标节点" name="certificate-node" :error="createErrors.fields.node_id" required full><template #default="{ controlAttrs }"><NodeLookup v-model="createForm.node_id" v-bind="controlAttrs" /></template></FormField>
        <FormField v-slot="{ controlAttrs }" label="证书名称" name="certificate-name" :error="createErrors.fields.name" required><UiInput v-model.trim="createForm.name" v-bind="controlAttrs" placeholder="例如：香港入口证书" maxlength="80" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="ACME 联系邮箱" name="certificate-email" :error="createErrors.fields.contact_email" required><UiInput v-model.trim="createForm.contact_email" v-bind="controlAttrs" type="email" placeholder="ops@example.com" maxlength="254" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="证书域名" name="certificate-domains" :error="createErrors.fields.domains" hint="每行或逗号分隔一个域名，最多 10 个；HTTP-01 不支持通配符和 IP。" required full><UiTextarea v-model="createForm.domains" v-bind="controlAttrs" rows="5" placeholder="edge.example.com&#10;cdn.example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="验证方式" name="certificate-challenge" :error="createErrors.fields.challenge_type" required><UiSelect v-model="createForm.challenge_type" v-bind="controlAttrs" :options="challengeOptions" /></FormField>
        <FormField v-if="createForm.challenge_type === 'dns-01'" v-slot="{ controlAttrs }" label="Cloudflare DNS 账户" name="certificate-provider" :error="createErrors.fields.provider_account_id" required><UiSelect v-model.number="createForm.provider_account_id" v-bind="controlAttrs" :options="providerOptions" /></FormField>
        <FormField v-else v-slot="{ controlAttrs }" label="节点 Webroot" name="certificate-webroot" :error="createErrors.fields.webroot_path" hint="现有 Web 服务必须从该目录提供 /.well-known/acme-challenge/。" required><UiInput v-model.trim="createForm.webroot_path" v-bind="controlAttrs" placeholder="/var/www/html" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="签发环境" name="certificate-environment" :error="createErrors.fields.environment"><UiSelect v-model="createForm.environment" v-bind="controlAttrs" :options="environmentOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="提前续期天数" name="certificate-renew-days" :error="createErrors.fields.renew_before_days"><UiNumberInput v-model="createForm.renew_before_days" v-bind="controlAttrs" :min="1" :max="60" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="自动续期" name="certificate-auto-renew"><label class="check-field"><UiCheckbox v-model="createForm.auto_renew" v-bind="controlAttrs" /><span>到达续期窗口后由面板自动执行</span></label></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="certificate-create-form" type="submit" :loading="saving">创建并申请</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="renewalOpen" title="续期策略" description="修改自动续期窗口；运行中的签发或续期不会被中断。" :busy="savingPolicy" @close="renewalOpen = false">
      <form id="certificate-renewal-form" ref="renewalFormElement" class="renewal-form" novalidate @submit.prevent="saveRenewal">
        <PageAlert v-if="renewalErrors.formError.value" tone="danger" title="Unable to save renewal policy">{{ renewalErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="自动续期" name="renewal-enabled"><label class="check-field"><UiCheckbox v-model="renewalForm.auto_renew" v-bind="controlAttrs" /><span>启用到期前自动续期</span></label></FormField>
        <FormField v-slot="{ controlAttrs }" label="提前续期天数" name="renewal-days"><UiNumberInput v-model="renewalForm.renew_before_days" v-bind="controlAttrs" :min="1" :max="60" /></FormField>
      </form>
      <template #footer><UiButton variant="secondary" type="button" :disabled="savingPolicy" @click="renewalOpen = false">取消</UiButton><UiButton form="certificate-renewal-form" type="submit" :loading="savingPolicy">保存策略</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createManagedCertificate, deleteManagedCertificate, fetchManagedCertificatesPage, fetchProviderAccounts, issueManagedCertificate, renewManagedCertificate, updateManagedCertificateRenewal, type CertificateOperation, type ManagedCertificate, type ProviderAccount } from '../api/client'
import DataTable from '../components/DataTable.vue'
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
import UiTextarea from '../components/UiTextarea.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { confirmAction } from '../utils/feedback'
import { preserveAdminReturnTo } from '../utils/navigation'
import { collectFieldErrors, isIntegerInRange, isUtf8LengthInRange } from '../utils/validation'

const route = useRoute()
const router = useRouter()
const search = ref(String(route.query.q || ''))
const statusFilter = ref(String(route.query.status || ''))
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * 50)
const limit = ref(50)
const message = ref('')
const operatingID = ref(0)
const deletingID = ref(0)
const saving = ref(false)
const createOpen = ref(false)
const createFormElement = ref<HTMLElement | null>(null)
const emptyCreateForm = () => ({ node_id: 0, provider_account_id: 0, name: '', domains: '', contact_email: '', environment: 'production' as 'production' | 'staging', auto_renew: true, renew_before_days: 30, challenge_type: 'dns-01' as 'dns-01' | 'http-01-webroot', webroot_path: '/var/www/html' })
const createForm = reactive(emptyCreateForm())
const providerAccounts = ref<ProviderAccount[]>([])
const createErrors = useFormErrors()
const createState = useDirtyForm(() => createForm)
const renewalOpen = ref(false)
const savingPolicy = ref(false)
const renewalFormElement = ref<HTMLElement | null>(null)
const renewalErrors = useFormErrors()
const renewalForm = reactive({ id: 0, auto_renew: true, renew_before_days: 30, revision: 0 })
const renewalState = useDirtyForm(() => renewalForm)
const statusOptions = [
  { label: '全部状态', value: '' }, { label: '待申请', value: 'pending' }, { label: '申请中', value: 'issuing' },
  { label: '有效', value: 'active' }, { label: '续期中', value: 'renewing' }, { label: '失败', value: 'failed' }, { label: '已过期', value: 'expired' },
]
const environmentOptions = [{ label: '生产环境（受信任）', value: 'production' }, { label: '测试环境（不受信任）', value: 'staging' }]
const challengeOptions = [{ label: 'Cloudflare DNS-01（推荐）', value: 'dns-01' }, { label: 'HTTP-01 Webroot', value: 'http-01-webroot' }]
const providerOptions = computed(() => providerAccounts.value.filter(item => item.provider_key === 'cloudflare' && item.status === 'active' && item.capabilities.includes('dns.records')).map(item => ({ label: item.name, value: item.id })))
const { items: certificates, total, loading, refreshing, error, load: refresh } = useRemoteTable<ManagedCertificate>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchManagedCertificatesPage({ offset: offset.value, limit: limit.value, q: search.value || undefined, status: statusFilter.value || undefined }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '证书列表加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const hasRunningOperation = computed(() => certificates.value.some(operationRunning))
let pollTimer: ReturnType<typeof setInterval> | undefined

useUnsavedChangesGuard(
  () => createOpen.value && createState.dirty.value,
  () => createState.confirmDiscard({ title: '放弃证书申请？', message: '尚未创建的证书信息将丢失。', confirmText: '放弃申请' }),
)
for (const field of ['node_id', 'provider_account_id', 'name', 'domains', 'contact_email', 'environment', 'renew_before_days', 'challenge_type', 'webroot_path']) watch(() => (createForm as any)[field], () => createErrors.clear(field))
watch(() => renewalForm.renew_before_days, () => renewalErrors.clear('renew_before_days'))
watch(renewalOpen, open => { if (open) renewalState.markClean() })
useUnsavedChangesGuard(
  () => renewalOpen.value && renewalState.dirty.value,
  () => renewalState.confirmDiscard({ title: 'Discard renewal changes?', message: 'The unsaved renewal policy will be lost.', confirmText: 'Discard changes' }),
)

function certificateStatusLabel(status: ManagedCertificate['status']) { return ({ pending: '待申请', issuing: '申请中', active: '有效', renewing: '续期中', failed: '操作失败', expired: '已过期' } as Record<string, string>)[status] || status }
function certificateTone(status: ManagedCertificate['status']): 'success' | 'warning' | 'danger' | 'neutral' | 'info' { return status === 'active' ? 'success' : status === 'issuing' || status === 'renewing' ? 'warning' : status === 'failed' || status === 'expired' ? 'danger' : 'neutral' }
function certificateIcon(status: ManagedCertificate['status']) { return status === 'active' ? 'shield' : status === 'issuing' || status === 'renewing' ? 'refresh' : status === 'failed' || status === 'expired' ? 'alert' : 'clock' }
function operationRunning(certificate: ManagedCertificate) { return certificate.latest_operation?.status === 'running' || certificate.status === 'issuing' || certificate.status === 'renewing' }
function operationTone(status: CertificateOperation['status']): 'success' | 'warning' | 'danger' { return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'warning' }
function operationLabel(operation: CertificateOperation) { const type = operation.operation_type === 'renew' ? '续期' : '申请'; return operation.status === 'running' ? `${type}中` : operation.status === 'succeeded' ? `${type}成功` : `${type}失败` }
function expiryTone(certificate: ManagedCertificate): 'success' | 'warning' | 'danger' { if (!certificate.not_after) return 'danger'; const days = (new Date(certificate.not_after).getTime() - Date.now()) / 86400000; return days <= 0 ? 'danger' : days <= certificate.renew_before_days ? 'warning' : 'success' }
function parseDomains(value: string) { return [...new Set(value.split(/[\n,，]+/).map(item => item.trim().toLowerCase()).filter(Boolean))] }

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(search.value ? { q: search.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(page > 1 ? { page: String(page) } : {}) } }
  await (replace ? router.replace(location) : router.push(location))
}
async function applyFilters() { offset.value = 0; await syncURL(); await refresh() }
async function clearFilters() { search.value = ''; statusFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await refresh() }
function openCreate() { Object.assign(createForm, emptyCreateForm()); createErrors.clear(); createState.markClean(); createOpen.value = true }
function closeCreate() { if (!saving.value) createOpen.value = false }
async function create() {
  const domains = parseDomains(createForm.domains)
  const valid = await createErrors.applyValidation(collectFieldErrors({
    node_id: !createForm.node_id && '请选择目标节点。',
    name: !isUtf8LengthInRange(createForm.name.trim(), 1, 80, true) && '证书名称需包含 1 到 80 个 UTF-8 字节。',
    domains: (!domains.length || domains.length > 10) && '请输入 1–10 个域名。',
    contact_email: !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(createForm.contact_email.trim()) && '请输入有效的 ACME 联系邮箱。',
    renew_before_days: !isIntegerInRange(createForm.renew_before_days, 1, 60) && '提前续期天数必须为 1–60 之间的整数。',
    provider_account_id: createForm.challenge_type === 'dns-01' && !createForm.provider_account_id && '请选择已验证的 Cloudflare DNS 账户。',
    webroot_path: createForm.challenge_type === 'http-01-webroot' && !createForm.webroot_path.startsWith('/') && '请输入节点上的绝对 Webroot 路径。',
  }), createFormElement, '请更正标记字段后再申请证书。')
  if (!valid) return
  saving.value = true; message.value = ''; error.value = ''
  try {
    const certificate = await createManagedCertificate({ node_id: createForm.node_id, provider_account_id: createForm.challenge_type === 'dns-01' ? createForm.provider_account_id : 0, name: createForm.name.trim(), domains, contact_email: createForm.contact_email.trim(), environment: createForm.environment, auto_renew: createForm.auto_renew, renew_before_days: createForm.renew_before_days, challenge_type: createForm.challenge_type, webroot_path: createForm.challenge_type === 'http-01-webroot' ? createForm.webroot_path : '' })
    await issueManagedCertificate(certificate.id)
    createOpen.value = false
    message.value = `证书 #${certificate.id} 已创建，节点签发操作正在后台执行。`
    await refresh()
  } catch (cause: any) {
    await createErrors.applyApiError(cause, '证书创建或签发启动失败；若资产已创建，可在列表中重试。', createFormElement, { node_id: 'node_id', provider_account_id: 'provider_account_id', name: 'name', domains: 'domains', contact_email: 'contact_email', environment: 'environment', renew_before_days: 'renew_before_days', challenge_type: 'challenge_type', webroot_path: 'webroot_path' })
  } finally { saving.value = false }
}
async function runCertificateOperation(certificate: ManagedCertificate) {
  const renewing = Boolean(certificate.not_after)
  const challengeNotice = certificate.challenge_type === 'dns-01' ? '将通过 Cloudflare DNS 自动验证，不占用节点端口。' : '将先检查域名全部 A/AAAA 地址的公网 80 端口，再由现有 Webroot 提供挑战文件。'
  if (!await confirmAction({ title: renewing ? '立即续期证书？' : '开始申请证书？', message: `${challengeNotice} 成功后${certificate.usage_count ? '会重新发布绑定协议的完整 Zero 配置' : '可在协议服务中绑定使用'}。`, confirmText: renewing ? '开始续期' : '开始申请' })) return
  operatingID.value = certificate.id; error.value = ''; message.value = ''
  try {
    if (renewing) await renewManagedCertificate(certificate.id)
    else await issueManagedCertificate(certificate.id)
    message.value = `${renewing ? '续期' : '申请'}操作已启动。`
    await refresh()
  } catch (cause: any) { error.value = cause?.response?.data?.message || '证书操作启动失败。' }
  finally { operatingID.value = 0 }
}
async function removeCertificate(certificate: ManagedCertificate) {
  if (!await confirmAction({
    title: '删除托管证书？',
    message: `将删除“${certificate.name}”的面板记录；签发/续期历史和节点上的证书文件会保留，避免破坏审计记录或误删仍被其他服务使用的文件。`,
    confirmText: '确认删除',
    tone: 'danger',
  })) return
  deletingID.value = certificate.id
  error.value = ''
  message.value = ''
  try {
    await deleteManagedCertificate(certificate.id)
    message.value = `证书“${certificate.name}”已删除；节点文件未被移除。`
    await refresh()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '证书删除失败。'
  } finally {
    deletingID.value = 0
  }
}
function openRenewal(certificate: ManagedCertificate) { Object.assign(renewalForm, { id: certificate.id, auto_renew: certificate.auto_renew, renew_before_days: certificate.renew_before_days, revision: certificate.revision }); renewalOpen.value = true }
async function saveRenewal() {
  renewalErrors.clear()
  const valid = await renewalErrors.applyValidation(collectFieldErrors({
    renew_before_days: !isIntegerInRange(renewalForm.renew_before_days, 1, 60) && 'Renewal lead time must be an integer between 1 and 60 days.',
  }), renewalFormElement, 'Correct the marked field before saving the renewal policy.')
  if (!valid) return
  if (!isIntegerInRange(renewalForm.renew_before_days, 1, 60)) { error.value = '提前续期天数必须为 1–60 之间的整数。'; return }
  savingPolicy.value = true; error.value = ''
  try {
    await updateManagedCertificateRenewal(renewalForm.id, { auto_renew: renewalForm.auto_renew, renew_before_days: renewalForm.renew_before_days, expected_revision: renewalForm.revision })
    renewalOpen.value = false; message.value = '自动续期策略已更新。'; await refresh()
  } catch (cause: any) { error.value = cause?.response?.data?.message || '自动续期策略保存失败。' }
  finally { savingPolicy.value = false }
}
function updatePolling() {
  if (hasRunningOperation.value && !pollTimer) pollTimer = setInterval(() => { void refresh() }, 3000)
  if (!hasRunningOperation.value && pollTimer) { clearInterval(pollTimer); pollTimer = undefined }
}
watch(hasRunningOperation, updatePolling)
watch(() => route.fullPath, async () => {
  const nextSearch = String(route.query.q || ''), nextStatus = String(route.query.status || ''), nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * limit.value
  if (nextSearch !== search.value || nextStatus !== statusFilter.value || nextOffset !== offset.value) { search.value = nextSearch; statusFilter.value = nextStatus; offset.value = nextOffset; await refresh() }
})
onMounted(async () => {
  const [accounts] = await Promise.all([fetchProviderAccounts(), refresh()])
  providerAccounts.value = accounts
  updatePolling()
  const dnsDomain = String(route.query.dns_domain || '').trim()
  const dnsNode = Number(route.query.dns_node || 0)
  const dnsProvider = Number(route.query.dns_provider || 0)
  if (dnsDomain && dnsNode > 0) {
    openCreate()
    createForm.node_id = dnsNode
    createForm.name = `${dnsDomain} 证书`
    createForm.domains = dnsDomain
    createForm.provider_account_id = dnsProvider
  }
})
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<style scoped>
.certificate-error { display: block; max-width: 230px; margin-top: 5px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.operation-cell { display: grid; justify-items: start; gap: 5px; }
.muted-value { color: var(--muted); }
.certificate-form { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; padding: 20px; }
.certificate-form > :deep(.page-alert), .certificate-form > :deep(.field-full) { grid-column: 1 / -1; }
.renewal-form { display: grid; gap: 15px; padding: 20px; }
.check-field { min-height: var(--control-height); align-items: center; }
@media (max-width: 720px) { .certificate-form { grid-template-columns: 1fr; }.certificate-form > * { grid-column: 1; } }
</style>
