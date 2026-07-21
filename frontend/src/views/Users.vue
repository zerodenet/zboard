<template>
  <section>
    <PageHeader title="用户与权限" description="所有账户都是用户；管理员是在用户身份上附加的管理权限。" eyebrow="Identity">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="refresh"><UiIcon name="refresh" />刷新</button><button class="button" type="button" @click="openCreate"><UiIcon name="plus" />创建用户</button></template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="user-summary">
      <div><span class="summary-icon"><UiIcon name="users" /></span><div><strong>{{ users.length }}</strong><span>当前列表用户</span></div></div>
      <div><span class="summary-icon success"><UiIcon name="check" /></span><div><strong>{{ activeCount }}</strong><span>正常账户</span></div></div>
      <div><span class="summary-icon admin"><UiIcon name="shield" /></span><div><strong>{{ adminCount }}</strong><span>平台管理员</span></div></div>
      <div><span class="summary-icon warning"><UiIcon name="alert" /></span><div><strong>{{ suspendedCount }}</strong><span>受限账户</span></div></div>
    </div>

    <article class="panel">
      <header class="panel-header"><div><h2>账户列表</h2><p>邮箱是唯一的登录身份和账户标识。</p></div></header>
      <div class="panel-body toolbar">
        <div class="toolbar-group filter-group">
          <label class="search-field"><UiIcon name="search" /><input v-model.trim="search" placeholder="搜索邮箱" @keyup.enter="refresh" /></label>
          <select v-model="statusFilter" aria-label="账户状态" @change="refresh"><option value="">全部状态</option><option value="active">正常</option><option value="suspended">已暂停</option><option value="deactivated">已停用</option></select>
          <button class="button button-secondary button-sm" type="button" @click="refresh">查询</button>
        </div>
        <span class="muted">共 {{ users.length }} 条结果</span>
      </div>
      <div v-if="users.length" class="table-shell">
        <table class="data-table">
          <thead><tr><th>用户</th><th>管理权限</th><th>状态</th><th>账户 ID</th><th></th></tr></thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td><div class="user-cell"><span class="user-avatar">{{ user.email.slice(0, 1).toUpperCase() }}</span><div class="cell-title"><strong>{{ user.email }}</strong></div></div></td>
              <td><StatusBadge :tone="permissionTone(user.is_admin)">{{ permissionName(user.is_admin) }}</StatusBadge></td>
              <td><StatusBadge :tone="statusTone(user.status)">{{ statusName(user.status) }}</StatusBadge></td>
              <td class="mono">#{{ user.id }}</td>
              <td><div class="cell-actions"><RouterLink class="button button-ghost button-sm" :to="`/admin/subscriptions?user_id=${user.id}`">业务档案</RouterLink><button class="button button-secondary button-sm" type="button" @click="openEdit(user)"><UiIcon name="edit" />账户与权限</button></div></td>
            </tr>
          </tbody>
        </table>
      </div>
      <EmptyState v-else icon="users" title="没有匹配用户" description="调整搜索条件，或创建一个新的平台账户。"><template #actions><button class="button" type="button" @click="openCreate"><UiIcon name="plus" />创建用户</button></template></EmptyState>
    </article>

    <ModalDialog :open="createOpen" title="创建平台用户" description="邮箱是唯一账户标识，创建后即可登录。" size="lg" :busy="saving" @close="closeCreate">
      <form id="create-user-form" class="form-grid" @submit.prevent="create">
        <div v-if="createError" class="alert alert-danger field-full"><UiIcon name="alert" />{{ createError }}</div>
        <label class="field field-full"><span>邮箱地址</span><input v-model.trim="form.email" type="email" autocomplete="email" required placeholder="user@example.com" /></label>
        <label class="field field-full"><span>初始密码</span><input v-model="form.password" type="password" minlength="12" maxlength="72" autocomplete="new-password" required /><small class="field-hint">长度为 12–72 个 UTF-8 字节。</small></label>
        <label class="field"><span>账户状态</span><select v-model="form.status"><option value="active">正常</option><option value="suspended">暂停</option><option value="deactivated">停用</option></select></label>
        <label class="check-field"><input v-model="form.isAdmin" type="checkbox" /><span><strong>授予管理员权限</strong><br /><small class="field-hint">用户仍可使用个人中心，并额外获得管理后台访问权。</small></span></label>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="closeCreate">取消</button><button class="button" form="create-user-form" type="submit" :disabled="saving">{{ saving ? '创建中…' : '创建用户' }}</button></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" title="管理用户账户" :description="selectedUser ? selectedUser.email : ''" size="lg" :busy="saving" @close="editOpen = false">
      <form id="edit-user-form" class="stack" @submit.prevent="saveUser">
        <div v-if="editError" class="alert alert-danger"><UiIcon name="alert" />{{ editError }}</div>
        <div class="form-grid">
          <label class="field field-full"><span>邮箱身份</span><input :value="selectedUser?.email" disabled /></label>
          <label class="field"><span>账户状态</span><select v-model="editDraft.status"><option value="active">正常</option><option value="suspended">暂停</option><option value="deactivated">停用</option></select></label>
          <label class="check-field"><input v-model="editDraft.is_admin" type="checkbox" /><span><strong>授予管理员权限</strong><br /><small class="field-hint">关闭后仅移除管理能力，不改变用户身份和个人数据。</small></span></label>
        </div>
        <div class="password-section"><div><strong>重置密码</strong><p>如不需要修改密码，请保持为空。</p></div><label class="field"><span>新密码</span><input v-model="editDraft.password" type="password" minlength="12" maxlength="72" autocomplete="new-password" placeholder="留空则不修改" /></label></div>
        <div v-if="selectedUser?.id === currentUserId" class="alert alert-warning"><UiIcon name="alert" />你正在修改当前登录用户，请谨慎变更管理员权限或账户状态。</div>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="editOpen = false">取消</button><button class="button" form="edit-user-form" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存账户' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { createAdminUser, fetchUsers, updateAdminUser } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'

type UserItem = { id: number; email: string; is_admin: boolean; status: string }
const app = useAppStore()
const users = ref<UserItem[]>([])
const search = ref('')
const statusFilter = ref('')
const error = ref('')
const message = ref('')
const loading = ref(false)
const saving = ref(false)
const createOpen = ref(false)
const createError = ref('')
const editOpen = ref(false)
const editError = ref('')
const selectedUser = ref<UserItem | null>(null)
const currentUserId = computed(() => app.user.id)
const activeCount = computed(() => users.value.filter(user => user.status === 'active').length)
const adminCount = computed(() => users.value.filter(user => user.is_admin).length)
const suspendedCount = computed(() => users.value.filter(user => user.status !== 'active').length)
const form = reactive({ email: '', password: '', isAdmin: false, status: 'active' })
const editDraft = reactive({ is_admin: false, status: 'active', password: '' })

function permissionName(isAdmin: boolean) { return isAdmin ? '管理员权限' : '无额外权限' }
function permissionTone(isAdmin: boolean): 'info' | 'neutral' { return isAdmin ? 'info' : 'neutral' }
function statusName(status: string) { return ({ active: '正常', suspended: '已暂停', deactivated: '已停用' } as Record<string, string>)[status] || status }
function statusTone(status: string): 'success' | 'warning' | 'danger' { return status === 'active' ? 'success' : status === 'suspended' ? 'warning' : 'danger' }

async function refresh() {
  loading.value = true; error.value = ''
  try { users.value = await fetchUsers({ q: search.value || undefined, status: statusFilter.value || undefined }) }
  catch (e: any) { error.value = e?.response?.data?.message || '用户列表加载失败。' }
  finally { loading.value = false }
}

async function create() {
  saving.value = true; createError.value = ''; message.value = ''
  try {
    await createAdminUser({ email: form.email, password: form.password, is_admin: form.isAdmin, status: form.status })
    Object.assign(form, { email: '', password: '', isAdmin: false, status: 'active' })
    createOpen.value = false; message.value = '用户已创建。'; await refresh()
  } catch (e: any) { createError.value = userFormError(e, '用户创建失败，请检查表单内容。') }
  finally { saving.value = false }
}

function openCreate() { createError.value = ''; createOpen.value = true }
function closeCreate() { if (!saving.value) { createError.value = ''; createOpen.value = false } }
function openEdit(user: UserItem) { selectedUser.value = user; editError.value = ''; Object.assign(editDraft, { is_admin: user.is_admin, status: user.status, password: '' }); editOpen.value = true }
async function saveUser() {
  if (!selectedUser.value) return
  saving.value = true; editError.value = ''; message.value = ''
  try {
    await updateAdminUser(selectedUser.value.id, { is_admin: editDraft.is_admin, status: editDraft.status, password: editDraft.password || undefined })
    editDraft.password = ''; editOpen.value = false; message.value = `${selectedUser.value.email} 已更新。`; await refresh()
    if (selectedUser.value.id === app.user.id) await app.loadMe()
  } catch (e: any) { editError.value = userFormError(e, '账户保存失败，请检查表单内容。') }
  finally { saving.value = false }
}
function userFormError(e: any, fallback: string) {
  const detail = e?.response?.data?.message || ''
  const translations: Record<string, string> = {
    'a valid email and a 12 to 72 byte password are required': '请填写有效邮箱，密码长度需为 12–72 个字节。',
    'email already exists': '该邮箱已存在。',
    'invalid status': '账户状态无效。',
    'invalid user status': '账户状态无效。',
    'password must contain 12 to 72 bytes': '密码长度需为 12–72 个字节。',
    'cannot modify the last active admin': '不能停用或降级最后一个有效管理员。'
  }
  if (translations[detail]) return translations[detail]
  return /[\u3400-\u9fff]/.test(detail) ? detail : fallback
}
onMounted(refresh)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.user-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 16px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface); }.user-summary > div { display: flex; align-items: center; gap: 11px; padding: 16px 18px; }.user-summary > div + div { border-left: 1px solid var(--line); }.summary-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.summary-icon.success { color: var(--success); background: var(--success-soft); }.summary-icon.admin { color: #7f56d9; background: #f4f3ff; }.summary-icon.warning { color: var(--warning); background: var(--warning-soft); }.user-summary > div > div { display: grid; }.user-summary strong { font-size: 20px; }.user-summary span:not(.summary-icon) { color: var(--muted); font-size: 10px; }.filter-group select { width: 150px; min-height: 36px; }.user-cell { display: flex; align-items: center; gap: 10px; }.user-avatar { width: 34px; height: 34px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 50%; color: #175cd3; background: var(--info-soft); font-size: 12px; font-weight: 750; }.password-section { display: grid; grid-template-columns: minmax(180px, .7fr) minmax(240px, 1.3fr); gap: 18px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.password-section strong { font-size: 13px; }.password-section p { margin: 4px 0 0; color: var(--muted); font-size: 11px; }
@media (max-width: 900px) { .user-summary { grid-template-columns: repeat(2, 1fr); }.user-summary > div:nth-child(3) { border-left: 0; border-top: 1px solid var(--line); }.user-summary > div:nth-child(4) { border-top: 1px solid var(--line); } }
@media (max-width: 560px) { .user-summary { grid-template-columns: 1fr; }.user-summary > div + div { border-left: 0; border-top: 1px solid var(--line); }.password-section { grid-template-columns: 1fr; } }
</style>
