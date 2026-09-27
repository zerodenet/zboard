<template>
  <section class="standard-page">
    <PageHeader title="外部供应商" description="管理 DNS 服务商账户及其连接状态。">
      <template #actions><PageRefreshButton label="刷新" :loading="loading || refreshing" @click="refreshAll" /><UiButton type="button" @click="openAccount()"><UiIcon name="plus" />添加供应商账户</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" success-title="操作已提交" error-title="操作失败" />

    <section class="provider-section" aria-label="供应商账户">
      <header class="provider-heading"><div><h2>供应商账户</h2><p>账户凭据仅在创建或更换时填写，保存后不再回显。</p></div><span>{{ accounts.length }} 个账户</span></header>
      <TableSkeleton v-if="(loading || refreshing) && !accounts.length" label="正在加载供应商账户" :columns="7" />
      <DataTable v-else-if="accounts.length" caption="外部供应商账户" :row-count="accounts.length" :min-width="760">
        <thead><tr><th class="table-primary-column">账户</th><th data-column-priority="2">供应商</th><th data-column-priority="3">能力</th><th>状态</th><th data-column-priority="2">引用</th><th data-column-priority="2">最近验证</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="account in accounts" :key="account.id">
          <td class="table-primary-column"><div class="cell-title"><strong>{{ account.name }}</strong><TableText :value="account.credential_prefix" /></div></td>
          <td data-column-priority="2">{{ providerLabel(account.provider_key) }}</td>
          <td data-column-priority="3"><div class="capabilities"><StatusBadge v-for="capability in account.capabilities" :key="capability" tone="neutral" :title="capability">{{ capabilityLabel(capability) }}</StatusBadge></div></td>
          <td><StatusBadge :tone="account.status === 'active' ? 'success' : account.status === 'invalid' ? 'danger' : 'warning'">{{ account.status === 'active' ? '有效' : account.status === 'invalid' ? '验证失败' : '待验证' }}</StatusBadge><small v-if="account.last_error" class="row-error" :title="account.last_error">{{ account.last_error }}</small></td>
          <td data-column-priority="2" :title="account.usage_count > 0 ? '关联的 DNS 解析与证书数量' : undefined">{{ account.usage_count }}</td>
          <td data-column-priority="2"><TimeBadge v-if="account.last_verified_at" :value="account.last_verified_at" mode="relative" /><span v-else class="muted-value">尚未验证</span></td>
          <td class="table-action-column"><RowActions :label="`${account.name} 的操作`" :trigger-key="`provider-${account.id}`"><UiButton size="sm" variant="ghost" :disabled="operatingAccount === account.id" @click="openAccount(account)">编辑</UiButton><UiButton size="sm" variant="secondary" :loading="operatingAccount === account.id" @click="verifyAccount(account)">重新验证</UiButton><UiButton size="sm" variant="danger" :disabled="operatingAccount === account.id" title="删除面板凭据并清理关联" @click="removeAccount(account)">删除</UiButton></RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else class="provider-empty-state" icon="settings" title="还没有供应商账户" description="先添加可用供应商的访问凭据，随后即可在面板管理 DNS 解析。" />
    </section>

    <ModalDialog :open="accountOpen" :title="editingAccount ? '编辑供应商账户' : '添加供应商账户'" description="Token 加密保存且不会再次回显；编辑时留空可保留原凭据。" :busy="savingAccount" :dirty="accountState.dirty.value" @close="accountOpen = false">
      <form id="provider-account-form" ref="accountFormElement" class="modal-form" novalidate @submit.prevent="saveAccount">
        <PageAlert v-if="accountErrors.formError.value" tone="danger" title="保存供应商账户失败">{{ accountErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="供应商" name="provider-kind" :error="accountErrors.fields.provider_key"><UiSelect v-model="accountForm.provider_key" v-bind="controlAttrs" :options="providerOptions" :disabled="Boolean(editingAccount)" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="账户名称" name="provider-name" :error="accountErrors.fields.name" required><UiInput v-bind="controlAttrs" v-model.trim="accountForm.name" maxlength="80" placeholder="例如：生产 Cloudflare" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="供应商访问凭据" name="provider-token" :hint="editingAccount ? '留空保留原凭据；更换时先验证，无效凭据不会覆盖原值。' : '请按供应商要求授予最小 DNS 读取和编辑权限。'" :error="accountErrors.fields.api_token" :required="!editingAccount" full><UiInput v-bind="controlAttrs" v-model.trim="accountForm.api_token" type="password" autocomplete="new-password" /></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" @click="requestClose">取消</UiButton><UiButton type="submit" form="provider-account-form" :loading="savingAccount">{{ editingAccount && !accountForm.api_token ? '保存修改' : '保存并验证' }}</UiButton></template>
    </ModalDialog>

  </section>
</template>

<script setup lang="ts">
import TableText from '../components/TableText.vue'
import { onMounted, reactive, ref, watch } from 'vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { confirmAction } from '../utils/feedback'
import { updateProviderAccount, createProviderAccount, deleteProviderAccount, fetchProviderAccounts, fetchProviderDefinitions, verifyProviderAccount, type ProviderAccount, type ProviderDefinition } from '../api/client'
import DataTable from '../components/DataTable.vue'
import TableSkeleton from '../components/TableSkeleton.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PageAlert from '../components/PageAlert.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import { collectFieldErrors, isUtf8LengthInRange } from '../utils/validation'

const definitions = ref<ProviderDefinition[]>([])
const accounts = ref<ProviderAccount[]>([])
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const message = ref('')
const accountOpen = ref(false)
const editingAccount = ref<ProviderAccount | null>(null)
const accountErrors = useFormErrors()
const savingAccount = ref(false)
const operatingAccount = ref(0)
const accountForm = reactive({ provider_key: 'cloudflare', name: '', api_token: '' })
const accountFormElement = ref<HTMLElement | null>(null)
const accountState = useDirtyForm(() => accountForm)
const providerOptions = ref<{ label: string; value: string }[]>([])
useUnsavedChangesGuard(() => accountOpen.value && accountState.dirty.value, () => accountState.confirmDiscard())
watch(() => accountForm.name, () => accountErrors.clear('name'))
watch(() => accountForm.api_token, () => accountErrors.clear('api_token'))

function providerLabel(key: string) { return definitions.value.find(item => item.key === key)?.name || key }
function capabilityLabel(value: string) { return ({ 'dns.records': 'DNS 解析', 'certificate.origin': '源站证书' } as Record<string, string>)[value] || value }
async function refreshAll() {
  refreshing.value = true; error.value = ''
  try {
    const [defs, providerAccounts] = await Promise.all([fetchProviderDefinitions(), fetchProviderAccounts()])
    definitions.value = defs
    providerOptions.value = defs.filter(item => item.capabilities.includes('dns.records') || item.capabilities.includes('certificate.issue')).map(item => ({ label: item.name, value: item.key }))
    accounts.value = providerAccounts
  } catch (cause: any) { error.value = cause?.response?.data?.message || '供应商数据加载失败。' } finally { loading.value = false; refreshing.value = false }
}
function openAccount(account?: ProviderAccount) {
  editingAccount.value = account || null
  Object.assign(accountForm, { provider_key: account?.provider_key || 'cloudflare', name: account?.name || '', api_token: '' })
  accountErrors.clear()
  accountOpen.value = true
  accountState.markClean()
}
async function saveAccount() {
  const valid = await accountErrors.applyValidation(collectFieldErrors({
    name: !isUtf8LengthInRange(accountForm.name, 1, 80, true) && '请输入 1–80 字节的账户名称。',
    api_token: !editingAccount.value && !accountForm.api_token.trim() && '请输入 Cloudflare API Token。',
  }), accountFormElement, '')
  if (!valid) return
  savingAccount.value = true; error.value = ''; message.value = ''
  try {
    const saved = editingAccount.value
      ? await updateProviderAccount(editingAccount.value.id, { name: accountForm.name, api_token: accountForm.api_token || undefined, expected_revision: editingAccount.value.revision })
      : await createProviderAccount({ ...accountForm })
    accountOpen.value = false
    accountForm.api_token = ''
    message.value = saved.status === 'active' ? '供应商账户已保存。' : '供应商账户已保存，但验证未通过；请编辑账户更换有效 Token 后重试。'
    await refreshAll()
  } catch (cause: any) {
    await accountErrors.applyApiError(cause, '供应商账户保存失败，请稍后重试。', accountFormElement, { name: 'name', api_token: 'api_token', provider_key: 'provider_key' })
  } finally { savingAccount.value = false }
}
async function verifyAccount(account: ProviderAccount) {
  operatingAccount.value = account.id; error.value = ''
  try { await verifyProviderAccount(account.id); message.value = `${account.name} 验证成功。`; await refreshAll() } catch (cause: any) { await refreshAll(); error.value = cause?.response?.data?.message || '账户验证失败。' } finally { operatingAccount.value = 0 }
}
async function removeAccount(account: ProviderAccount) {
  if (!await confirmAction({ title: '删除供应商接入？', message: '此操作删除面板保存的凭据及关联 DNS 管理记录，并解除证书账户关联、停止自动续期。Cloudflare 账户、Token 和远端解析保持原样。', confirmText: '确认删除', tone: 'danger' })) return
  operatingAccount.value = account.id; error.value = ''
  try { await deleteProviderAccount(account.id); message.value = '供应商接入已删除。'; await refreshAll() }
  catch (cause: any) { error.value = cause?.response?.data?.message || '供应商删除失败。' }
  finally { operatingAccount.value = 0 }
}
onMounted(async () => {
  loading.value = true
  await refreshAll()
})
</script>

<style scoped>
.provider-section { min-width: 0; }
.provider-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.provider-heading > div { min-width: 0; }
.provider-heading h2 { margin: 0; font-size: 14px; font-weight: 600; }
.provider-heading p { margin: 4px 0 0; color: var(--muted); font-size: 12px; }
.provider-heading > span { flex: none; color: var(--muted); font-size: 12px; }
.provider-empty-state { min-height: 150px; padding: 24px; }
.capabilities { display: flex; flex-wrap: wrap; gap: 6px; }
.account-form-error { grid-column: 1 / -1; color: var(--danger); margin: 0; overflow-wrap: anywhere; }
.row-error { display: block; max-width: 260px; margin-top: 4px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.muted-value { color: var(--muted); }
.modal-form { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; padding: 20px; }.modal-form > :deep(.form-field-full) { grid-column: 1 / -1; }
@media (max-width: 720px) { .modal-form { grid-template-columns: 1fr; }.modal-form > * { grid-column: 1; }.provider-heading { align-items: flex-start; flex-direction: column; gap: 4px; } }
</style>
