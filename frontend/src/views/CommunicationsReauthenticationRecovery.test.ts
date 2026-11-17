import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Announcements from './Announcements.vue'
import Menus from './Menus.vue'
import Tasks from './Tasks.vue'
import FormField from '../components/FormField.vue'
import { createAnnouncement, deleteAnnouncement, fetchAdminAnnouncements, updateAnnouncement, createAdminTask, runAdminTask, reviewAdminMailDelivery, fetchAdminTasksPage, fetchAdminTaskSummary, fetchEmailTemplates } from '../api/client'
import { fetchMenus, fetchNavigation, saveMenus } from '../api/menus'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/menus', () => ({ fetchMenus: vi.fn(), saveMenus: vi.fn(), fetchNavigation: vi.fn() }))
vi.mock('../api/client', () => ({ createAnnouncement: vi.fn(), deleteAnnouncement: vi.fn(), fetchAdminAnnouncements: vi.fn(), updateAnnouncement: vi.fn(), createAdminTask: vi.fn(), runAdminTask: vi.fn(), reviewAdminMailDelivery: vi.fn(), fetchAdminTasksPage: vi.fn(), fetchAdminTaskSummary: vi.fn(), fetchEmailTemplates: vi.fn(), fetchAdminTask: vi.fn(), fetchAdminTaskItems: vi.fn(), fetchSubscriptionsPage: vi.fn(), fetchUsersPage: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn() }))
const announcement = { id: 1, revision: 3, title: 'Server title', content: 'Server content', severity: 'info', audience: 'user', status: 'draft', popup_enabled: false, dismissible: true, starts_at: null, ends_at: null, created_at: '', updated_at: '', created_by: 1 }
const menu = { revision: 7, nodes: [{ id: 'core:tasks', surface: 'admin', parent_id: '', label: 'Tasks', icon: 'tasks', path: '/admin/tasks', position: 1, hidden: false, owner: 'core', plugin_id: '', page_id: '', condition: 'fresh-authority' }] }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const button = defineComponent({ template: '<button><slot /></button>' })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined
let captured = ''
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts(); captured = ''
  state.app = { token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 1, nodes: [], page_available: true })
  vi.mocked(fetchAdminAnnouncements).mockResolvedValue({ items: [structuredClone(announcement)], total: 1, offset: 0, limit: 50 } as any)
  vi.mocked(fetchMenus).mockImplementation(async () => structuredClone(menu) as any)
  vi.mocked(fetchAdminTasksPage).mockResolvedValue({ items: [], total: 0 } as any)
  vi.mocked(fetchAdminTaskSummary).mockResolvedValue({ total: 0, pending: 0, running: 0 } as any)
  vi.mocked(fetchEmailTemplates).mockResolvedValue([])
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(kind: 'announcements' | 'menus' | 'tasks') {
  const path = `/admin/${kind}`, component = { announcements: Announcements, menus: Menus, tasks: Tasks }[kind]
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component }] })
  await router.push(path)
  view = shallowMount(component as any, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiButton: button }, stubs: { UiInput: false, UiTextarea: false, UiButton: false, FormField, ModalDialog: modal, DetailDrawer: modal, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /><slot name="actions" /></aside>' } } } })
  await flushPromises()
  return path
}
async function roundTrip(kind: 'announcements' | 'menus' | 'tasks', path: string, actor = 1, admin = true) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  captured = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('')
  view!.unmount(); view = undefined
  state.app.user.id = actor; state.app.isAdmin = admin
  confirmReauthenticationDraftIdentity(actor, admin)
  await open(kind)
}
function noTaskWrite() { expect(createAdminTask).not.toHaveBeenCalled(); expect(runAdminTask).not.toHaveBeenCalled(); expect(reviewAdminMailDelivery).not.toHaveBeenCalled() }
it('recovers announcement content, audience and dates with original revision, but no publication choice', async () => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.edit(structuredClone(announcement)); vm.form.title = 'Local title'; vm.form.content = 'Local body'; vm.form.audience = 'guest'; vm.form.starts_at = '2026-10-28T12:00'; vm.form.status = 'published'
  await roundTrip('announcements', path)
  const restored = view!.vm as any
  expect(restored.form).toMatchObject({ title: 'Local title', content: 'Local body', audience: 'guest', starts_at: '2026-10-28T12:00', revision: 3, status: 'draft' })
  expect(captured).not.toContain('"status"'); expect(updateAnnouncement).not.toHaveBeenCalled()
  await restored.save()
  expect(updateAnnouncement).toHaveBeenCalledWith(1, expect.objectContaining({ title: 'Local title', expected_revision: 3, status: 'draft' }))
})
it('keeps original announcement concurrency evidence through recapture and forbids stale save/delete', async () => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.edit(structuredClone(announcement)); vm.form.content = 'Keep old draft'
  vi.mocked(fetchAdminAnnouncements).mockResolvedValue({ items: [{ ...announcement, revision: 4, content: 'Other operator' }], total: 1 } as any)
  await roundTrip('announcements', path); await roundTrip('announcements', path)
  const restored = view!.vm as any
  expect(restored.form.content).toBe('Keep old draft'); expect(restored.form.revision).toBe(3)
  await restored.save(); await restored.remove()
  expect(updateAnnouncement).not.toHaveBeenCalled(); expect(deleteAnnouncement).not.toHaveBeenCalled()
})
it('keeps new announcement ordinary text as an unpublished draft without creating it', async () => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.newAnnouncement(); vm.form.title = 'New local'; vm.form.status = 'published'
  await roundTrip('announcements', path)
  expect((view!.vm as any).form).toMatchObject({ id: 0, revision: 0, title: 'New local', status: 'draft' })
  expect(createAnnouncement).not.toHaveBeenCalled()
})
it('finds a formerly visible announcement beyond the first fresh page without changing its original revision', async () => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.edit(structuredClone(announcement)); vm.form.title = 'Local title'
  vi.mocked(fetchAdminAnnouncements).mockImplementation(async (offset = 0) => ({ items: offset === 50 ? [structuredClone(announcement)] : Array.from({ length: 50 }, (_, index) => ({ ...announcement, id: index + 10 })), total: 51 } as any))
  await roundTrip('announcements', path)
  expect(fetchAdminAnnouncements).toHaveBeenCalledWith(50, 50)
  expect((view!.vm as any).announcementRecoveryConflict).toBe(false)
})
it.each(['missing', 'archived'] as const)('preserves the %s announcement draft but blocks submit', async (kind) => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.edit(structuredClone(announcement)); vm.form.title = 'Local title'
  vi.mocked(fetchAdminAnnouncements).mockResolvedValue({ items: kind === 'missing' ? [] : [{ ...announcement, status: 'archived' }], total: kind === 'missing' ? 0 : 1 } as any)
  await roundTrip('announcements', path)
  expect((view!.vm as any).form.title).toBe('Local title'); await (view!.vm as any).save()
  expect(updateAnnouncement).not.toHaveBeenCalled()
})
it('does not consume an announcement draft when fresh resource reads fail', async () => {
  const path = await open('announcements'), vm = view!.vm as any
  vm.edit(structuredClone(announcement)); vm.form.title = 'Local title'
  vi.mocked(fetchAdminAnnouncements).mockRejectedValue(new Error('offline'))
  await roundTrip('announcements', path)
  expect((view!.vm as any).editing).toBe(false); expect(updateAnnouncement).not.toHaveBeenCalled()
  expect(sessionStorage.length).toBeGreaterThan(0)
})
it('restores menu presentation and original tree revision, obtaining immutable authority freshly', async () => {
  const path = await open('menus'), vm = view!.vm as any
  vm.snapshot.nodes[0].label = 'Local menu'; vm.snapshot.nodes[0].hidden = true; vm.snapshot.nodes[0].condition = 'DO-NOT-STORE'
  await roundTrip('menus', path)
  const restored = view!.vm as any
  expect(restored.snapshot).toMatchObject({ revision: 7, nodes: [{ label: 'Local menu', hidden: true, owner: 'core', condition: 'fresh-authority' }] })
  expect(captured).not.toContain('DO-NOT-STORE'); expect(saveMenus).not.toHaveBeenCalled()
  vi.mocked(saveMenus).mockResolvedValue({ ...menu, revision: 8 } as any)
  await restored.save()
  expect(saveMenus).toHaveBeenCalledWith('admin', expect.objectContaining({ revision: 7 }))
})
it('retains conflicting menu trees across repeated reauthentication and only discards to fresh server state', async () => {
  const path = await open('menus'), vm = view!.vm as any
  vm.snapshot.nodes[0].label = 'Old unsaved tree'
  vi.mocked(fetchMenus).mockResolvedValue({ revision: 8, nodes: [{ ...menu.nodes[0], label: 'Current server tree' }] } as any)
  await roundTrip('menus', path); await roundTrip('menus', path)
  const restored = view!.vm as any
  expect(restored.snapshot.nodes[0].label).toBe('Old unsaved tree'); await restored.save()
  expect(saveMenus).not.toHaveBeenCalled()
  restored.discard()
  expect(restored.snapshot.nodes[0].label).toBe('Current server tree'); expect(restored.snapshot.revision).toBe(8)
})
it('restores the selected menu surface without a watcher replacing its recovered tree', async () => {
  const path = await open('menus'), vm = view!.vm as any
  const account = { revision: 2, nodes: [{ ...menu.nodes[0], surface: 'account', path: '/account', label: 'Account menu' }] }
  vi.mocked(fetchMenus).mockImplementation(async (surface) => structuredClone(surface === 'account' ? account : menu) as any)
  vm.surface = 'account'; await flushPromises(); vm.snapshot.nodes[0].label = 'Local account menu'
  await roundTrip('menus', path)
  expect((view!.vm as any).surface).toBe('account'); expect((view!.vm as any).snapshot.nodes[0].label).toBe('Local account menu')
})
it('restores ordinary unsent email and scope selections, resetting immediate execution', async () => {
  const path = await open('tasks'), vm = view!.vm as any
  await vm.openCreate(); vm.form.type = 'email'; vm.form.auto_run = true; vm.scopeMode = 'users'; vm.selectedScopeIDs = [9, 2]; vm.email.subject = 'Local subject'; vm.email.body = 'Local body'
  vm.deliveryReviewReason = 'DO-NOT-STORE'; vm.deliveryReviewOutcome = 'accepted'
  await roundTrip('tasks', path)
  const restored = view!.vm as any
  expect(restored.createOpen).toBe(true); expect(restored.form).toMatchObject({ type: 'email', auto_run: false })
  expect(restored.email).toMatchObject({ subject: 'Local subject', body: 'Local body' }); expect(restored.scopeMode).toBe('users'); expect(restored.selectedScopeIDs).toEqual([9, 2])
  expect(captured).not.toContain('auto_run'); expect(captured).not.toContain('DO-NOT-STORE'); noTaskWrite()
  vi.mocked(createAdminTask).mockResolvedValue({ id: 6 } as any)
  await restored.createTask()
  expect(createAdminTask).toHaveBeenCalledWith({ type: 'email', scope: { user_ids: [9, 2] }, content: { subject: 'Local subject', body: 'Local body' }, max_attempts: 3, auto_run: false })
  expect(runAdminTask).not.toHaveBeenCalled()
})
it('recovers unsubmitted quota content without task results or delivery-review authority', async () => {
  const path = await open('tasks'), vm = view!.vm as any
  await vm.openCreate(); vm.quota.delta_mb = -12; vm.quota.reason = 'Unsubmitted quota reason'; vm.scopeMode = 'subscriptions'; vm.selectedScopeIDs = [4, 8]; vm.form.auto_run = true
  await roundTrip('tasks', path)
  expect((view!.vm as any).quota).toEqual({ delta_mb: -12, reason: 'Unsubmitted quota reason' }); expect((view!.vm as any).selectedScopeIDs).toEqual([4, 8]); expect((view!.vm as any).form.auto_run).toBe(false)
  noTaskWrite()
})
it.each(['announcements', 'menus', 'tasks'] as const)('does not restore %s before fresh page permission is approved', async (kind) => {
  const path = await open(kind), vm = view!.vm as any
  if (kind === 'announcements') { vm.newAnnouncement(); vm.form.title = 'Hidden local' }
  else if (kind === 'menus') vm.snapshot.nodes[0].label = 'Hidden local'
  else { await vm.openCreate(); vm.quota.reason = 'Hidden local' }
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 2, nodes: [], page_available: false })
  await roundTrip(kind, path)
  expect(view!.text()).not.toContain('Hidden local'); expect(view!.text()).toContain('当前页面权限已变化')
  expect(saveMenus).not.toHaveBeenCalled(); expect(updateAnnouncement).not.toHaveBeenCalled(); noTaskWrite()
})
it.each([{ actor: 2, admin: true }, { actor: 1, admin: false }])('isolates recovered ordinary content after identity/role change: %j', async ({ actor, admin }) => {
  const path = await open('tasks'), vm = view!.vm as any
  await vm.openCreate(); vm.quota.reason = 'Old admin draft'
  await roundTrip('tasks', path, actor, admin)
  expect((view!.vm as any).createOpen).toBe(false); expect(sessionStorage.length).toBe(0); noTaskWrite()
})
it('keeps a menu draft pending on a failed fresh tree read and restores only after an explicit retry', async () => {
  const path = await open('menus'), vm = view!.vm as any
  vm.snapshot.nodes[0].label = 'Keep local tree'
  vi.mocked(fetchMenus).mockRejectedValue(new Error('unavailable'))
  await roundTrip('menus', path)
  expect(sessionStorage.length).toBeGreaterThan(0); expect(saveMenus).not.toHaveBeenCalled()
  vi.mocked(fetchMenus).mockImplementation(async () => structuredClone(menu) as any)
  await (view!.vm as any).load(); await flushPromises()
  await (view!.vm as any).menuDraftRecovery.restore(); await flushPromises()
  expect((view!.vm as any).snapshot.nodes[0].label).toBe('Keep local tree'); expect(saveMenus).not.toHaveBeenCalled()
})
it('keeps new custom menu presentation without persisting or restoring fabricated page authority', async () => {
  const path = await open('menus'), vm = view!.vm as any
  vm.snapshot.nodes.push({ ...menu.nodes[0], id: 'custom:new-draft', parent_id: '', owner: 'custom', label: 'Local entry', path: '/admin/tasks', condition: 'DO-NOT-STORE', plugin_id: 'DO-NOT-STORE', page_id: 'DO-NOT-STORE' })
  await roundTrip('menus', path)
  expect((view!.vm as any).snapshot.nodes[1]).toMatchObject({ id: 'custom:new-draft', owner: 'custom', label: 'Local entry', condition: '', plugin_id: '', page_id: '' })
  expect(captured).not.toContain('DO-NOT-STORE'); expect(saveMenus).not.toHaveBeenCalled()
})
