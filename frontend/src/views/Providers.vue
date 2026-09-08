<template>
  <section class="standard-page">
    <PageHeader title="外部供应商" description="集中管理外部账户、加密凭据和供应商能力；DNS、证书与未来支付渠道仍保留各自独立的业务资源。" eyebrow="Infrastructure">
      <template #actions><PageRefreshButton label="刷新" :loading="loading || refreshing" @click="refreshAll" /><UiButton type="button" @click="openAccount()"><UiIcon name="plus" />添加供应商账户</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" success-title="操作已提交" error-title="操作失败" />

    <section class="provider-section panel">
      <div class="section-heading"><div><h2>供应商账户</h2><p>支持修改账户名称和更换 Token；保存后仅显示脱敏标识。</p></div></div>
      <DataTable v-if="accounts.length" caption="外部供应商账户" :row-count="accounts.length" :min-width="760">
        <thead><tr><th class="table-primary-column">账户</th><th data-column-priority="2">供应商</th><th data-column-priority="3">能力</th><th>状态</th><th data-column-priority="2">引用</th><th data-column-priority="2">最近验证</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="account in accounts" :key="account.id">
          <td class="table-primary-column"><div class="cell-title"><strong>{{ account.name }}</strong><TableText :value="account.credential_prefix" /></div></td>
          <td data-column-priority="2">{{ providerLabel(account.provider_key) }}</td>
          <td data-column-priority="3"><div class="capabilities"><StatusBadge v-for="capability in account.capabilities" :key="capability" tone="neutral">{{ capability }}</StatusBadge></div></td>
          <td><StatusBadge :tone="account.status === 'active' ? 'success' : account.status === 'invalid' ? 'danger' : 'warning'">{{ account.status === 'active' ? '有效' : account.status === 'invalid' ? '验证失败' : '待验证' }}</StatusBadge><small v-if="account.last_error" class="row-error" :title="account.last_error">{{ account.last_error }}</small></td>
          <td data-column-priority="2">{{ account.usage_count }}<small v-if="account.usage_count > 0" class="usage-help">删除时清理 DNS 管理记录，解除证书账户关联并停止自动续期。</small></td>
          <td data-column-priority="2"><TimeBadge v-if="account.last_verified_at" :value="account.last_verified_at" mode="relative" /><span v-else class="muted-value">尚未验证</span></td>
          <td class="table-action-column"><RowActions :label="`${account.name} 的操作`" :trigger-key="`provider-${account.id}`"><UiButton size="sm" variant="ghost" :disabled="operatingAccount === account.id" @click="openAccount(account)">编辑</UiButton><UiButton size="sm" variant="secondary" :loading="operatingAccount === account.id" @click="verifyAccount(account)">重新验证</UiButton><UiButton size="sm" variant="danger" :disabled="operatingAccount === account.id" title="删除面板凭据并清理关联" @click="removeAccount(account)">删除</UiButton></RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else class="provider-empty-state" icon="settings" title="还没有供应商账户" description="先添加 Cloudflare API Token，随后即可在面板管理 DNS 解析。" />
    </section>

    <ModalDialog :open="accountOpen" :title="editingAccount ? '编辑供应商账户' : '添加供应商账户'" description="Token 加密保存且不会再次回显；编辑时留空可保留原凭据。" :busy="savingAccount" @close="accountOpen = false">
      <div class="modal-form">
        <p v-if="accountErrors.formError.value" class="account-form-error" role="alert">{{ accountErrors.formError.value }}</p>
        <FormField label="供应商"><UiSelect v-model="accountForm.provider_key" :options="providerOptions" :disabled="Boolean(editingAccount)" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="账户名称" :error="accountErrors.fields.name" required><UiInput v-bind="controlAttrs" v-model.trim="accountForm.name" maxlength="80" placeholder="例如：生产 Cloudflare" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="Cloudflare API Token" :hint="editingAccount ? '留空保留原 Token；更换时先验证，新 Token 无效不会覆盖原凭据。' : '建议仅授予 Zone 读取和 DNS 编辑权限。'" :error="accountErrors.fields.api_token" :required="!editingAccount" full><UiInput v-bind="controlAttrs" v-model.trim="accountForm.api_token" type="password" autocomplete="new-password" /></FormField>
      </div>
      <template #footer><UiButton variant="secondary" @click="accountOpen = false">取消</UiButton><UiButton type="button" :loading="savingAccount" @click="saveAccount">{{ editingAccount && !accountForm.api_token ? '保存修改' : '保存并验证' }}</UiButton></template>
    </ModalDialog>

  </section>
</template>

<script setup lang="ts">
import TableText from '../components/TableText.vue'
import { onMounted, reactive, ref } from 'vue'
import { useFormErrors } from '../composables/useFormState'
import { confirmAction } from '../utils/feedback'
import { updateProviderAccount, createProviderAccount, deleteProviderAccount, fetchProviderAccounts, fetchProviderDefinitions, verifyProviderAccount, type ProviderAccount, type ProviderDefinition } from '../api/client'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'

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
const providerOptions = ref<{ label: string; value: string }[]>([])

function providerLabel(key: string) { return definitions.value.find(item => item.key === key)?.name || key }
async function refreshAll() {
  refreshing.value = true; error.value = ''
  try {
    const [defs, providerAccounts] = await Promise.all([fetchProviderDefinitions(), fetchProviderAccounts()])
    definitions.value = defs
    providerOptions.value = defs.filter(item => item.key === 'cloudflare').map(item => ({ label: item.name, value: item.key }))
    accounts.value = providerAccounts
  } catch (cause: any) { error.value = cause?.response?.data?.message || '供应商数据加载失败。' } finally { loading.value = false; refreshing.value = false }
}
function openAccount(account?: ProviderAccount) {
  editingAccount.value = account || null
  Object.assign(accountForm, { provider_key: account?.provider_key || 'cloudflare', name: account?.name || '', api_token: '' })
  accountErrors.clear()
  accountOpen.value = true
}
async function saveAccount() {
  savingAccount.value = true; error.value = ''; message.value = ''; accountErrors.clear()
  try {
    const saved = editingAccount.value
      ? await updateProviderAccount(editingAccount.value.id, { name: accountForm.name, api_token: accountForm.api_token || undefined, expected_revision: editingAccount.value.revision })
      : await createProviderAccount({ ...accountForm })
    accountOpen.value = false
    accountForm.api_token = ''
    message.value = saved.status === 'active' ? '供应商账户已保存。' : '供应商账户已保存，但验证未通过；请编辑账户更换有效 Token 后重试。'
    await refreshAll()
  } catch (cause: any) {
    await accountErrors.applyApiError(cause, '供应商账户保存失败，请稍后重试。')
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
.provider-section { display: grid; gap: 0; overflow: hidden; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 18px 20px; border-bottom: 1px solid var(--line); }
.section-heading > div { min-width: 0; }
.section-heading h2 { margin: 0; font-size: 16px; }.section-heading p { margin: 4px 0 0; color: var(--muted); }
.provider-empty-state { min-height: 150px; padding: 24px; }
.capabilities { display: flex; flex-wrap: wrap; gap: 6px; }
.usage-help { display: block; max-width: 220px; margin-top: 4px; color: var(--muted); }
.account-form-error { grid-column: 1 / -1; color: var(--danger); margin: 0; overflow-wrap: anywhere; }
.row-error { display: block; max-width: 260px; margin-top: 4px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.muted-value { color: var(--muted); }
.modal-form { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; padding: 20px; }.modal-form > :deep(.form-field-full) { grid-column: 1 / -1; }
@media (max-width: 720px) { .modal-form { grid-template-columns: 1fr; }.modal-form > * { grid-column: 1; }.section-heading { align-items: stretch; flex-direction: column; } }
</style>
