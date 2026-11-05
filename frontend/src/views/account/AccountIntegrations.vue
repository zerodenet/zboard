<template>
  <section class="account-page stack integrations-page">
    <PageHeader title="外部集成">
      <template #actions><UiButton as-child variant="ghost" size="sm"><RouterLink to="/account/security">账户安全</RouterLink></UiButton><PageRefreshButton :loading="loading" :disabled="creating || revoking" @click="load(offset)" /></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" />
    <PageAlert v-if="integrationDraftRecovery.message.value" tone="info" title="重新认证后的草稿">{{ integrationDraftRecovery.message.value }}<UiButton type="button" variant="ghost" @click="integrationDraftRecovery.restore()">重新检查草稿恢复</UiButton></PageAlert>
    <UiSection variant="outlined" v-if="issuedToken" title="凭据已创建" description="原文仅显示这一次，请复制并保存在客户端的安全配置中。">
      <div class="credential-result">
        <FormField v-slot="{ controlAttrs }" label="集成凭据" name="issued-integration-token" full>
          <UiInput v-bind="controlAttrs" :model-value="issuedToken" readonly autocomplete="off" spellcheck="false" />
        </FormField>
        <div class="button-row"><UiButton type="button" @click="copyToken">复制凭据</UiButton><UiButton variant="secondary" type="button" @click="issuedToken = ''">已保存，关闭原文</UiButton></div>
      </div>
    </UiSection>
    <UiSection variant="outlined" title="创建凭据" description="读取本人的流量数据，最多创建 20 个有效凭据。">
      <form ref="formElement" class="integration-form" novalidate @submit.prevent="create">
        <FormField v-slot="{ controlAttrs }" label="用途名称" name="integration-name" :error="formErrors.fields.name" required>
          <UiInput v-model="name" v-bind="controlAttrs" maxlength="120" placeholder="例如：每日报表" :disabled="creating || !!issuedToken" />
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="有效期" name="integration-lifetime" :error="formErrors.fields.days" required>
          <UiSelect v-model="days" v-bind="controlAttrs" :options="[{ label: '30 天', value: 30 }, { label: '90 天', value: 90 }, { label: '一年', value: 365 }]" :disabled="creating || !!issuedToken" />
        </FormField>
        <UiButton type="submit" :loading="creating" :disabled="loading || !name.trim() || !!issuedToken">创建凭据</UiButton>
        <PageAlert v-if="formErrors.formError.value" tone="danger" title="凭据未创建">{{ formErrors.formError.value }}</PageAlert>
      </form>
    </UiSection>
    <UiSection variant="outlined" title="已创建的凭据" >
      <ul v-if="items.length" class="credential-list" aria-label="集成凭据">
        <li v-for="item in items" :key="item.id">
          <div class="account-record-identity"><strong>{{ item.name || '未命名凭据' }}</strong><small>{{ item.scopes.map(scopeLabel).join('、') || '无可用权限' }}</small><details class="credential-identifier"><summary>凭据标识</summary><code>{{ item.token_prefix }}…</code></details></div>
          <TimeBadge class="credential-expiry" :value="item.expires_at" />
          <StatusBadge :tone="isActive(item) ? 'success' : 'neutral'">{{ stateLabel(item) }}</StatusBadge>
          <UiButton v-if="!item.revoked_at" class="credential-revoke" variant="ghost" size="sm" type="button" :disabled="loading || revoking" @click="selected = item; revokeError = ''">撤销</UiButton><span v-else class="credential-revoke">—</span>
        </li>
      </ul>
      <TableSkeleton v-else-if="loading" label="正在读取凭据" :columns="4" :rows="3" />
      <EmptyState v-else-if="!error" icon="key" title="暂无凭据" description="" />
      <div v-if="offset > 0 || hasNext" class="pager"><UiButton variant="ghost" :disabled="loading || offset === 0" @click="load(Math.max(0, offset - pageSize))">上一页</UiButton><span>第 {{ offset / pageSize + 1 }} 页</span><UiButton variant="ghost" :disabled="loading || !hasNext" @click="load(offset + pageSize)">下一页</UiButton></div>
    </UiSection>
    <details class="integration-instructions"><summary>连接客户端</summary><UiSection variant="outlined" title="接口地址" description="在客户端中设置下列地址，并使用刚创建的凭据进行 Bearer 认证。">
      <dl class="endpoint-list"><dt>能力目录</dt><dd><code>{{ directoryURL }}</code></dd><dt>流量查询</dt><dd><code>{{ invokeURL }}</code></dd></dl>
      <p>客户端只能读取本人的数据。查询时指定开始时间、结束时间和分钟／小时／日粒度，每次最多返回 200 条分组记录。</p>
    </UiSection></details>
    <ConfirmDialog :open="!!selected" title="撤销这个凭据？" :message="`“${selected?.name || '此凭据'}”连接的客户端将停止访问。`" confirm-text="撤销凭据" tone="danger" :busy="revoking" :error="revokeError" @close="selected = null" @confirm="revoke" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { API_BASE } from '../../api/client'
import { listIntegrationCredentials, issueIntegrationCredential, revokeIntegrationCredential, type IntegrationCredential } from '../../api/integrations'
import TimeBadge from '../../components/TimeBadge.vue'
import TableSkeleton from '../../components/TableSkeleton.vue'
import PageHeader from '../../components/PageHeader.vue'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiSection from '../../components/UiSection.vue'
import UiInput from '../../components/UiInput.vue'
import UiSelect from '../../components/UiSelect.vue'
import UiButton from '../../components/UiButton.vue'
import FormField from '../../components/FormField.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import EmptyState from '../../components/EmptyState.vue'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import PageAlert from '../../components/PageAlert.vue'
import { useFormErrors } from '../../composables/useFormState'
import { collectFieldErrors } from '../../utils/validation'
import { useReauthenticationDraft } from '../../composables/useReauthenticationDraft'
import { useAppStore } from '../../stores/app'
const name = ref(''), days = ref(30), issuedToken = ref(''), issuedID = ref(0)
const items = ref<IntegrationCredential[]>([]), offset = ref(0), hasNext = ref(false), pageSize = 20
const loading = ref(false), creating = ref(false), revoking = ref(false), message = ref(''), error = ref(''), revokeError = ref('')
const selected = ref<IntegrationCredential | null>(null)
const formElement = ref<HTMLElement | null>(null), formErrors = useFormErrors()
watch(name, () => formErrors.clear('name'))
watch(days, () => formErrors.clear('days'))
const directoryURL = new URL(`${API_BASE}/integrations/capabilities`, window.location.origin).href
const invokeURL = `${directoryURL}/metering.usage.query/invoke`
let alive = true
const draftApp = useAppStore(), draftOwner = draftApp.user.id
const currentDraftOwner = () => alive && draftApp.user.id === draftOwner && Boolean(draftApp.token) && !draftApp.reauthRequired
function integrationDraft(value: unknown): { name: string; days: number } | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return
  const item = value as Record<string, unknown>
  if (typeof item.name !== 'string' || new TextEncoder().encode(item.name).byteLength > 480 || typeof item.days !== 'number' || ![30, 90, 365].includes(item.days)) return
  return { name: item.name, days: item.days }
}
const integrationDraftRecovery = useReauthenticationDraft({
  scope: 'account.integrations.create', permission: 'account',
  ready: () => !loading.value && !creating.value && !revoking.value && !issuedToken.value,
  read: () => !creating.value && !revoking.value && !issuedToken.value && (name.value || days.value !== 30) ? { name: name.value, days: days.value } : null,
  sanitize: integrationDraft,
  restore: (value, { isCurrent }) => {
    if (!isCurrent() || !currentDraftOwner() || creating.value || revoking.value || issuedToken.value) return false
    // A new local edit takes precedence over an older stored draft.
    if (name.value || days.value !== 30) return false
    name.value = value.name; days.value = value.days
  },
})
watch(() => draftApp.user.id, actor => {
  if (actor === draftOwner) return
  name.value = ''; days.value = 30; issuedToken.value = ''; issuedID.value = 0
  selected.value = null; items.value = []; offset.value = 0; hasNext.value = false
  message.value = ''; error.value = ''; revokeError.value = ''
}, { flush: 'sync' })
async function load(next = offset.value) {
  if (loading.value) return
  loading.value = true; error.value = ''
  try { const rows = await listIntegrationCredentials(next, pageSize + 1); if (!alive || !currentDraftOwner()) return; items.value = rows.slice(0, pageSize); hasNext.value = rows.length > pageSize; offset.value = next }
  catch { if (alive && currentDraftOwner()) error.value = '凭据读取失败，请重试。' }
  finally { loading.value = false }
}
async function create() {
  if (!currentDraftOwner() || loading.value || creating.value || issuedToken.value) return
  if (!await formErrors.applyValidation(collectFieldErrors({
    name: !name.value.trim() && '请输入用途名称。',
    days: ![30, 90, 365].includes(days.value) && '请选择有效期。',
  }), formElement)) return
  if (!currentDraftOwner()) return
  creating.value = true; error.value = ''; message.value = ''
  try { const result = await issueIntegrationCredential(name.value.trim(), days.value); if (!alive || !currentDraftOwner()) return; issuedToken.value = result.token; issuedID.value = result.credential.id; name.value = ''; await load(0) }
  catch (cause) { if (alive && currentDraftOwner()) await formErrors.applyApiError(cause, '创建失败，请检查名称、有效期和有效凭据数量后重试。', formElement, { expires_at: 'days' }) }
  finally { creating.value = false }
}
async function copyToken() {
  try { await navigator.clipboard.writeText(issuedToken.value); message.value = '凭据已复制。' }
  catch { error.value = '无法自动复制，请选中上方凭据手动复制。' }
}
async function revoke() {
  if (!currentDraftOwner() || !selected.value || revoking.value) return
  const id = selected.value.id
  revoking.value = true; revokeError.value = ''
  try { await revokeIntegrationCredential(id); if (!alive || !currentDraftOwner()) return; if (issuedID.value === id) issuedToken.value = ''; selected.value = null; message.value = '凭据已撤销。'; await load(offset.value) }
  catch { if (alive && currentDraftOwner()) revokeError.value = '撤销失败，请重试。' }
  finally { revoking.value = false }
}
function isActive(item: IntegrationCredential) { return !item.revoked_at && !!item.expires_at && new Date(item.expires_at).getTime() > Date.now() }
function stateLabel(item: IntegrationCredential) { return item.revoked_at ? '已撤销' : isActive(item) ? '有效' : item.expires_at ? '已到期' : '不可用于集成' }
function scopeLabel(value: string) { return value === 'metering.usage.query' ? '本人流量查询' : value }
onMounted(() => load(0))
onBeforeUnmount(() => { alive = false; issuedToken.value = '' })
</script>
<style scoped>
.credential-list { margin: 0; padding: 0; list-style: none; }
.credential-list > li { display: grid; grid-template-columns: minmax(0, 1fr) 160px 90px auto; align-items: center; gap: 20px; padding: 16px 20px; }
.credential-list > li:nth-child(even) { background: var(--surface-soft); }
.credential-expiry { justify-self: start; }
.credential-identifier { color: var(--muted-foreground); font-size: 12px; }
.credential-identifier summary { width: fit-content; cursor: pointer; }
.credential-identifier code { display: block; padding-top: 6px; overflow-wrap: anywhere; }
@media (max-width: 600px) {
  .credential-list > li { grid-template-columns: minmax(0, 1fr) auto; gap: 12px; padding: 16px; }
  .credential-list .account-record-identity { grid-column: 1; grid-row: 1 / 3; }
  .credential-expiry { grid-column: 1; grid-row: 3; }
  .credential-list .status-badge { grid-column: 2; grid-row: 1; }
  .credential-revoke { grid-column: 2; grid-row: 2 / 4; min-height: 40px; }
}

.integration-form{display:grid;grid-template-columns:minmax(180px,1fr) minmax(140px,180px) auto;max-width:760px;gap:16px;align-items:end;padding:0}.integration-instructions>summary{width:fit-content;cursor:pointer;font-size:13px;color:var(--muted-foreground)}.integration-instructions>summary:focus-visible{outline:2px solid var(--ring);outline-offset:4px}.integration-instructions>.ui-section{margin-top:16px}.credential-result{padding:0;display:grid;gap:16px}.button-row,.pager{display:flex;gap:12px;align-items:center}.pager{justify-content:flex-end;padding:16px}.token-prefix{display:block;color:var(--muted);margin-top:4px}.endpoint-list{display:grid;gap:8px;padding:0}.endpoint-list dt{font-weight:600}.endpoint-list dd{margin:0;overflow-wrap:anywhere}.ui-section>p{padding:0;color:var(--muted)}@media(max-width:640px){.integration-form{grid-template-columns:1fr}.button-row{flex-wrap:wrap}}
</style>
