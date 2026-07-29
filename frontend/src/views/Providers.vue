<template>
  <section class="standard-page">
    <PageHeader title="外部供应商" description="集中管理外部账户、加密凭据和供应商能力；DNS、证书与未来支付渠道仍保留各自独立的业务资源。" eyebrow="Infrastructure">
      <template #actions><PageRefreshButton label="刷新" :loading="loading || refreshing" @click="refreshAll" /><UiButton type="button" @click="accountOpen = true"><UiIcon name="plus" />添加供应商账户</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" success-title="操作已提交" error-title="操作失败" />

    <section class="provider-section panel">
      <div class="section-heading"><div><h2>供应商账户</h2><p>凭据只在创建时提交，保存后仅显示脱敏标识。</p></div></div>
      <DataTable v-if="accounts.length" caption="外部供应商账户" :row-count="accounts.length" :min-width="760">
        <thead><tr><th>账户</th><th>供应商</th><th>能力</th><th>状态</th><th>引用</th><th>最近验证</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="account in accounts" :key="account.id">
          <td><div class="cell-title"><strong>{{ account.name }}</strong><span>{{ account.credential_prefix }}</span></div></td>
          <td>{{ providerLabel(account.provider_key) }}</td>
          <td><div class="capabilities"><StatusBadge v-for="capability in account.capabilities" :key="capability" tone="neutral">{{ capability }}</StatusBadge></div></td>
          <td><StatusBadge :tone="account.status === 'active' ? 'success' : account.status === 'invalid' ? 'danger' : 'warning'">{{ account.status === 'active' ? '有效' : account.status === 'invalid' ? '验证失败' : '待验证' }}</StatusBadge><small v-if="account.last_error" class="row-error">{{ account.last_error }}</small></td>
          <td>{{ account.usage_count }}</td>
          <td><TimeBadge v-if="account.last_verified_at" :value="account.last_verified_at" mode="relative" /><span v-else class="muted-value">尚未验证</span></td>
          <td class="table-action-column"><UiButton size="sm" variant="secondary" :loading="operatingAccount === account.id" @click="verifyAccount(account)">重新验证</UiButton></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else class="provider-empty-state" icon="settings" title="还没有供应商账户" description="先添加 Cloudflare API Token，随后即可在面板管理 DNS 解析。" />
    </section>

    <ModalDialog :open="accountOpen" title="添加供应商账户" description="第一阶段支持 Cloudflare；Token 加密保存且不会再次回显。" :busy="savingAccount" @close="accountOpen = false">
      <div class="modal-form">
        <FormField label="供应商"><UiSelect v-model="accountForm.provider_key" :options="providerOptions" /></FormField>
        <FormField label="账户名称" required><UiInput v-model.trim="accountForm.name" maxlength="80" placeholder="例如：生产 Cloudflare" /></FormField>
        <FormField label="Cloudflare API Token" hint="建议仅授予 Zone 读取和 DNS 编辑权限。" required full><UiInput v-model.trim="accountForm.api_token" type="password" autocomplete="new-password" /></FormField>
      </div>
      <template #footer><UiButton variant="secondary" @click="accountOpen = false">取消</UiButton><UiButton type="button" :loading="savingAccount" @click="createAccount">保存并验证</UiButton></template>
    </ModalDialog>

  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { createProviderAccount, fetchProviderAccounts, fetchProviderDefinitions, verifyProviderAccount, type ProviderAccount, type ProviderDefinition } from '../api/client'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
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
async function createAccount() {
  savingAccount.value = true; error.value = ''; message.value = ''
  try {
    const created = await createProviderAccount(accountForm); accountOpen.value = false; Object.assign(accountForm, { provider_key: 'cloudflare', name: '', api_token: '' }); message.value = created.status === 'active' ? '供应商账户已保存并验证。' : '供应商账户已保存，但 Cloudflare 验证失败；请检查 Token 权限后重新验证。'; await refreshAll()
  } catch (cause: any) { error.value = cause?.response?.data?.message || '供应商账户创建失败。' } finally { savingAccount.value = false }
}
async function verifyAccount(account: ProviderAccount) {
  operatingAccount.value = account.id; error.value = ''
  try { await verifyProviderAccount(account.id); message.value = `${account.name} 验证成功。`; await refreshAll() } catch (cause: any) { error.value = cause?.response?.data?.message || '账户验证失败。'; await refreshAll() } finally { operatingAccount.value = 0 }
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
.row-error { display: block; max-width: 260px; margin-top: 4px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.muted-value { color: var(--muted); }
.modal-form { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; padding: 20px; }.modal-form > :deep(.field-full) { grid-column: 1 / -1; }
@media (max-width: 720px) { .modal-form { grid-template-columns: 1fr; }.modal-form > * { grid-column: 1; }.section-heading { align-items: stretch; flex-direction: column; } }
</style>
