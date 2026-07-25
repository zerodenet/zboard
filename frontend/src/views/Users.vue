<template>
  <section class="standard-page">
    <PageHeader title="用户与权限" description="所有账户都是用户；管理员是在用户身份上附加的管理权限。" eyebrow="Identity">
      <template #actions><PageRefreshButton label="刷新用户" :loading="loading" @click="refresh" /><UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建用户</UiButton></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="用户信息已更新" error-title="用户操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || statusFilter)" @clear="clearFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="用户邮箱" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="statusFilter" label="账户状态" :options="filterStatusOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <DataTable v-if="users.length" caption="用户与权限列表" :row-count="total" :min-width="980">
          <thead><tr><th class="table-primary-column">用户</th><th data-column-priority="2">管理权限</th><th>状态</th><th class="numeric-column" data-column-priority="2">订阅（有效/全部）</th><th class="numeric-column" data-column-priority="3">订单（待处理/全部）</th><SortableHeader field="created_at" label="创建时间" :sort-field="sortField" :direction="sortDirection" :priority="3" @sort="setSort" /><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td class="table-primary-column"><div class="user-cell"><span class="user-avatar">{{ user.email.slice(0, 1).toUpperCase() }}</span><div class="cell-title"><strong>{{ user.email }}</strong><span class="mono">#{{ user.id }}</span></div></div></td>
              <td data-column-priority="2"><StatusBadge :tone="permissionTone(user.is_admin)">{{ permissionName(user.is_admin) }}</StatusBadge></td>
              <td><StatusBadge :tone="statusTone(user.status)">{{ statusName(user.status) }}</StatusBadge></td>
              <td class="numeric-column" data-column-priority="2">{{ formatNumber(user.active_subscription_count) }} / {{ formatNumber(user.total_subscription_count) }}</td>
              <td class="numeric-column" data-column-priority="3">{{ formatNumber(user.pending_order_count) }} / {{ formatNumber(user.total_order_count) }}</td>
              <td data-column-priority="3"><TimeBadge :value="user.created_at" /></td>
              <td class="table-action-column"><RowActions :label="`${user.email} 的操作`" :trigger-key="`user-${user.id}`"><UiButton variant="secondary" size="sm" type="button" :data-user-detail-trigger="user.id" @click="openDetail(user.id)">查看详情</UiButton><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/subscriptions', { user_id: String(user.id) })">查看订阅</RouterLink><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/orders', { user_id: String(user.id) })">查看订单</RouterLink><UiButton variant="ghost" size="sm" type="button" @click="openEdit(user)"><UiIcon name="edit" />账户与权限</UiButton></RowActions></td>
            </tr>
          </tbody>
      </DataTable>
      <EmptyState v-else icon="users" title="没有匹配用户" description="调整搜索条件，或创建一个新的平台账户。"><template #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建用户</UiButton></template></EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(detailID)" :title="selectedUserDetail?.email || '用户详情'" eyebrow="User account" :description="selectedUserDetail ? `账户 #${selectedUserDetail.id}` : '正在加载账户业务概览'" :return-focus-selector="detailID ? `[data-row-action-trigger='user-${detailID}']` : ''" @close="closeDetail">
      <PageAlert v-if="detailError" tone="danger" title="用户详情加载失败">{{ detailError }}</PageAlert>
      <div v-if="detailLoading" class="detail-loading" role="status">正在加载用户详情…</div>
      <main v-else-if="selectedUserDetail" class="business-detail stack">
        <section class="detail-status-strip" aria-label="用户状态">
          <StatusBadge :tone="statusTone(selectedUserDetail.status)">{{ statusName(selectedUserDetail.status) }}</StatusBadge>
          <StatusBadge :tone="permissionTone(selectedUserDetail.is_admin)">{{ permissionName(selectedUserDetail.is_admin) }}</StatusBadge>
          <StatusBadge :tone="selectedUserDetail.email_verified_at ? 'success' : 'warning'">{{ selectedUserDetail.email_verified_at ? '邮箱已验证' : '邮箱未验证' }}</StatusBadge>
        </section>
        <section class="detail-metrics" aria-label="关联业务数量">
          <div><strong>{{ selectedUserDetail.active_subscription_count }}</strong><span>有效订阅</span></div>
          <div><strong>{{ selectedUserDetail.total_subscription_count }}</strong><span>全部订阅</span></div>
          <div><strong>{{ selectedUserDetail.pending_order_count }}</strong><span>待处理订单</span></div>
          <div><strong>{{ selectedUserDetail.total_order_count }}</strong><span>全部订单</span></div>
        </section>
        <section class="detail-facts" aria-label="账户信息">
          <div><span>账户名称</span><strong>{{ selectedUserDetail.account_name || '未设置' }}</strong></div>
          <div><span>账户 ID</span><strong class="mono">#{{ selectedUserDetail.id }}</strong></div>
          <div><span>最近登录</span><TimeBadge :value="selectedUserDetail.last_login_at" /></div>
          <div><span>创建时间</span><TimeBadge :value="selectedUserDetail.created_at" /></div>
          <div><span>更新时间</span><TimeBadge :value="selectedUserDetail.updated_at" /></div>
          <div><span>邮箱验证</span><TimeBadge :value="selectedUserDetail.email_verified_at" /></div>
        </section>
        <div class="detail-action-row">
          <RouterLink class="button button-secondary button-sm" :to="adminContextLink('/admin/subscriptions', { user_id: String(selectedUserDetail.id) })">查看订阅</RouterLink>
          <RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/orders', { user_id: String(selectedUserDetail.id) })">查看订单</RouterLink>
          <UiButton variant="ghost" size="sm" type="button" @click="openEdit(selectedUserDetail)"><UiIcon name="edit" />账户与权限</UiButton>
        </div>
      </main>
    </DetailDrawer>

    <ModalDialog :open="createOpen" :dirty="createState.dirty.value" title="创建平台用户" description="邮箱是唯一账户标识，创建后即可登录。" size="lg" :busy="saving" @close="closeCreate">
      <form id="create-user-form" ref="createFormElement" class="form-grid" novalidate @submit.prevent="create">
        <PageAlert v-if="createErrors.formError.value" class="field-full" tone="danger" title="无法创建用户">{{ createErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="create-user-email" :error="createErrors.fields.email" required full><UiInput v-model.trim="form.email" v-bind="controlAttrs" type="email" autocomplete="email" placeholder="user@example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="初始密码" name="create-user-password" hint="长度为 12–72 个 UTF-8 字节。" :error="createErrors.fields.password" required full><UiInput v-model="form.password" v-bind="controlAttrs" type="password" minlength="12" maxlength="72" autocomplete="new-password" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="账户状态" name="create-user-status" :error="createErrors.fields.status"><UiSelect v-model="form.status" v-bind="controlAttrs" :options="userStatusOptions" /></FormField>
        <label class="check-field"><UiCheckbox v-model="form.isAdmin" /><span><strong>授予管理员权限</strong><br /><small class="field-hint">用户仍可使用个人中心，并额外获得管理后台访问权。</small></span></label>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="create-user-form" type="submit" :loading="saving">创建用户</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" :dirty="editState.dirty.value" title="管理用户账户" :description="selectedUser ? selectedUser.email : ''" size="lg" :busy="saving" @close="closeEdit">
      <form id="edit-user-form" ref="editFormElement" class="stack" novalidate @submit.prevent="saveUser">
        <PageAlert v-if="editErrors.formError.value" tone="danger" title="无法保存账户">{{ editErrors.formError.value }}</PageAlert>
        <div class="form-grid">
          <FormField v-slot="{ controlAttrs }" label="邮箱身份" full><UiInput :value="selectedUser?.email" v-bind="controlAttrs" disabled /></FormField>
          <FormField v-slot="{ controlAttrs }" label="账户状态" name="edit-user-status" :error="editErrors.fields.status"><UiSelect v-model="editDraft.status" v-bind="controlAttrs" :options="userStatusOptions" /></FormField>
          <label class="check-field"><UiCheckbox v-model="editDraft.is_admin" /><span><strong>授予管理员权限</strong><br /><small class="field-hint">关闭后仅移除管理能力，不改变用户身份和个人数据。</small></span></label>
        </div>
        <div class="password-section"><div><strong>重置密码</strong><p>如不需要修改密码，请保持为空。</p></div><FormField v-slot="{ controlAttrs }" label="新密码" name="edit-user-password" hint="长度为 12–72 个 UTF-8 字节；留空则不修改。" :error="editErrors.fields.password"><UiInput v-model="editDraft.password" v-bind="controlAttrs" type="password" minlength="12" maxlength="72" autocomplete="new-password" placeholder="留空则不修改" /></FormField></div>
        <PageAlert v-if="selectedUser?.id === currentUserId" tone="warning" title="当前登录用户">请谨慎变更管理员权限或账户状态。</PageAlert>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="edit-user-form" type="submit" :loading="saving">保存账户</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createAdminUser, fetchAdminUserDetail, fetchUsersPage, updateAdminUser, type AdminUserDetail, type AdminUserListItem } from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import SortableHeader from '../components/SortableHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { nextSortDirection, resolveSortDirection, resolveSortField } from '../composables/tableState'
import { useAppStore } from '../stores/app'
import { formatNumber } from '../utils/format'
import { collectFieldErrors, isEmail, isOneOf, isUtf8LengthInRange } from '../utils/validation'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

type UserItem = AdminUserListItem
const app = useAppStore()
const route = useRoute()
const router = useRouter()
const search = ref(String(route.query.q || ''))
const statusFilter = ref(String(route.query.status || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
type UserSortField = 'id' | 'email' | 'created_at'
const userSortFields = new Set<UserSortField>(['id', 'email', 'created_at'])
const sortField = ref(resolveSortField(route.query.sort, userSortFields, 'created_at'))
const sortDirection = ref<'asc' | 'desc'>(resolveSortDirection(route.query.direction, 'desc'))
const message = ref('')
const saving = ref(false)
const createOpen = ref(false)
const editOpen = ref(false)
const createFormElement = ref<HTMLElement | null>(null)
const editFormElement = ref<HTMLElement | null>(null)
const createErrors = useFormErrors()
const editErrors = useFormErrors()
const selectedUser = ref<UserItem | null>(null)
const selectedUserDetail = ref<AdminUserDetail | null>(null)
const detailID = ref(0)
const detailLoading = ref(false)
const detailError = ref('')
let detailController: AbortController | null = null
const currentUserId = computed(() => app.user.id)
const form = reactive({ email: '', password: '', isAdmin: false, status: 'active' })
const editDraft = reactive({ is_admin: false, status: 'active', password: '' })
const createState = useDirtyForm(() => form)
const editState = useDirtyForm(() => editDraft)
useUnsavedChangesGuard(
  () => (createOpen.value && createState.dirty.value) || (editOpen.value && editState.dirty.value),
  async () => {
    if (createOpen.value && !await createState.confirmDiscard({
      title: '放弃新用户草稿？',
      message: '离开用户管理后，尚未创建的账户信息将丢失。',
      confirmText: '离开页面',
    })) return false
    if (editOpen.value && !await editState.confirmDiscard({
      title: '放弃账户修改？',
      message: '离开用户管理后，尚未保存的账户修改将丢失。',
      confirmText: '离开页面',
    })) return false
    return true
  },
)
const validUserStatuses = ['active', 'suspended', 'deactivated'] as const
watch(() => form.email, () => createErrors.clear('email'))
watch(() => form.password, () => createErrors.clear('password'))
watch(() => form.status, () => createErrors.clear('status'))
watch(() => editDraft.status, () => editErrors.clear('status'))
watch(() => editDraft.password, () => editErrors.clear('password'))
const filterStatusOptions = [
  { label: '全部状态', value: '' },
  { label: '正常', value: 'active' },
  { label: '已暂停', value: 'suspended' },
  { label: '已停用', value: 'deactivated' },
]
const userStatusOptions = filterStatusOptions.slice(1)
const { items: users, total, loading, refreshing, error, load: refresh } = useRemoteTable<UserItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchUsersPage({
    q: search.value || undefined,
    status: statusFilter.value || undefined,
    sort: sortField.value,
    direction: sortDirection.value,
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '用户列表加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

function permissionName(isAdmin: boolean) { return isAdmin ? '管理员权限' : '无额外权限' }
function permissionTone(isAdmin: boolean): 'info' | 'neutral' { return isAdmin ? 'info' : 'neutral' }
function statusName(status: string) { return ({ active: '正常', suspended: '已暂停', deactivated: '已停用' } as Record<string, string>)[status] || `未知状态（${status || '空'}）` }
function statusTone(status: string): 'success' | 'warning' | 'danger' { return status === 'active' ? 'success' : status === 'suspended' ? 'warning' : 'danger' }

function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(search.value ? { q: search.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(sortField.value !== 'created_at' ? { sort: sortField.value } : {}), ...(sortDirection.value !== 'desc' ? { direction: sortDirection.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(detailID.value ? { user: String(detailID.value) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await refresh() }
async function clearFilters() { search.value = ''; statusFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await refresh() }
async function setSort(field: string) { const next = resolveSortField(field, userSortFields, 'created_at'); sortDirection.value = nextSortDirection(sortField.value, next, sortDirection.value, next === 'email' ? 'asc' : 'desc'); sortField.value = next; offset.value = 0; await syncURL(); await refresh() }
async function openDetail(id: number) { await router.push({ query: { ...route.query, user: String(id) } }) }
async function closeDetail() { detailController?.abort(); detailID.value = 0; selectedUserDetail.value = null; detailError.value = ''; const { user: _user, ...query } = route.query; await router.push({ query }) }
async function syncDetailFromRoute() {
  const id = Number(route.query.user)
  if (!Number.isInteger(id) || id <= 0) {
    detailController?.abort(); detailID.value = 0; selectedUserDetail.value = null; detailError.value = ''; detailLoading.value = false
    return
  }
  if (detailID.value === id && (selectedUserDetail.value?.id === id || detailLoading.value)) return
  detailController?.abort()
  detailController = new AbortController()
  detailID.value = id; selectedUserDetail.value = null; detailError.value = ''; detailLoading.value = true
  try { selectedUserDetail.value = await fetchAdminUserDetail(id, { signal: detailController.signal }) }
  catch (cause: any) { if (cause?.name !== 'CanceledError' && cause?.name !== 'AbortError') detailError.value = cause?.response?.data?.message || '用户详情加载失败。' }
  finally { if (detailID.value === id) detailLoading.value = false }
}

async function create() {
  form.email = form.email.trim().toLowerCase()
  const valid = await createErrors.applyValidation(collectFieldErrors({
    email: !isEmail(form.email) && '请输入不超过 128 个 UTF-8 字节的有效邮箱。',
    password: !isUtf8LengthInRange(form.password, 12, 72) && '密码必须为 12–72 个 UTF-8 字节。',
    status: !isOneOf(form.status, validUserStatuses) && '请选择有效的账户状态。',
  }), createFormElement, '请更正标记字段后再创建用户。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    await createAdminUser({ email: form.email, password: form.password, is_admin: form.isAdmin, status: form.status })
    Object.assign(form, { email: '', password: '', isAdmin: false, status: 'active' })
    createOpen.value = false; message.value = '用户已创建。'; await refresh()
  } catch (e: any) { await createErrors.applyApiError(e, '用户创建失败，请检查表单内容。', createFormElement, { email: 'email', password: 'password', status: 'status' }) }
  finally { saving.value = false }
}

function openCreate() { Object.assign(form, { email: '', password: '', isAdmin: false, status: 'active' }); createState.markClean(); createErrors.clear(); createOpen.value = true }
function closeCreate() { if (!saving.value) { createErrors.clear(); createOpen.value = false } }
function openEdit(user: UserItem) { selectedUser.value = user; editErrors.clear(); Object.assign(editDraft, { is_admin: user.is_admin, status: user.status, password: '' }); editState.markClean(); editOpen.value = true }
function closeEdit() { if (!saving.value) { editErrors.clear(); editOpen.value = false } }
async function saveUser() {
  if (!selectedUser.value) return
  const valid = await editErrors.applyValidation(collectFieldErrors({
    status: !isOneOf(editDraft.status, validUserStatuses) && '请选择有效的账户状态。',
    password: Boolean(editDraft.password) && !isUtf8LengthInRange(editDraft.password, 12, 72) && '密码必须为 12–72 个 UTF-8 字节；留空则不修改。',
  }), editFormElement, '请更正标记字段后再保存账户。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    await updateAdminUser(selectedUser.value.id, { is_admin: editDraft.is_admin, status: editDraft.status, password: editDraft.password || undefined })
    editDraft.password = ''; editOpen.value = false; message.value = `${selectedUser.value.email} 已更新。`; await refresh()
    if (detailID.value === selectedUser.value.id) { selectedUserDetail.value = null; await syncDetailFromRoute() }
    if (selectedUser.value.id === app.user.id) await app.loadMe()
  } catch (e: any) { await editErrors.applyApiError(e, '账户保存失败，请检查表单内容。', editFormElement, { status: 'status', password: 'password' }) }
  finally { saving.value = false }
}
watch(() => route.fullPath, async () => {
  const nextSearch = String(route.query.q || '')
  const nextStatus = String(route.query.status || '')
  const nextLimitValue = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(nextLimitValue) ? nextLimitValue : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  const nextSortField = resolveSortField(route.query.sort, userSortFields, 'created_at')
  const nextSortDirection = resolveSortDirection(route.query.direction, 'desc')
  if (nextSearch !== search.value || nextStatus !== statusFilter.value || nextLimit !== limit.value || nextOffset !== offset.value || nextSortField !== sortField.value || nextSortDirection !== sortDirection.value) {
    search.value = nextSearch; statusFilter.value = nextStatus; limit.value = nextLimit; offset.value = nextOffset; sortField.value = nextSortField; sortDirection.value = nextSortDirection; await refresh()
  }
  await syncDetailFromRoute()
})
onMounted(async () => { await refresh(); await syncDetailFromRoute() })
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.user-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 16px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface); }.user-summary > div { display: flex; align-items: center; gap: 11px; padding: 16px 18px; }.user-summary > div + div { border-left: 1px solid var(--line); }.summary-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.summary-icon.success { color: var(--success); background: var(--success-soft); }.summary-icon.admin { color: var(--admin-accent); background: var(--code-soft); }.summary-icon.warning { color: var(--warning); background: var(--warning-soft); }.user-summary > div > div { display: grid; }.user-summary strong { font-size: 20px; }.user-summary span:not(.summary-icon) { color: var(--muted); font-size: 10px; }.user-cell { display: flex; align-items: center; gap: 10px; }.user-avatar { width: 34px; height: 34px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 50%; color: var(--info); background: var(--info-soft); font-size: 12px; font-weight: 750; }.password-section { display: grid; grid-template-columns: minmax(180px, .7fr) minmax(240px, 1.3fr); gap: 18px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.password-section strong { font-size: 13px; }.password-section p { margin: 4px 0 0; color: var(--muted); font-size: 11px; }
@media (max-width: 900px) { .user-summary { grid-template-columns: repeat(2, 1fr); }.user-summary > div:nth-child(3) { border-left: 0; border-top: 1px solid var(--line); }.user-summary > div:nth-child(4) { border-top: 1px solid var(--line); } }
@media (max-width: 560px) { .user-summary { grid-template-columns: 1fr; }.user-summary > div + div { border-left: 0; border-top: 1px solid var(--line); }.password-section { grid-template-columns: 1fr; } }
@media (max-width: 480px) { .data-table .table-primary-column { width: 147px; min-width: 147px; max-width: 147px; }.user-cell .cell-title { min-width: 0; overflow: hidden; }.user-cell .cell-title strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
.user-summary{border-inline:0;border-radius:0}
</style>
