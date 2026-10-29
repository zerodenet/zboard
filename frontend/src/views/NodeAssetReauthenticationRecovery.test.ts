import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Nodes from './Nodes.vue'
import NodeGroups from './NodeGroups.vue'
import FormField from '../components/FormField.vue'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity, REAUTH_DRAFT_MANIFEST } from '../utils/reauthenticationDrafts'
const mocks = vi.hoisted(() => ({ node: vi.fn(), nodes: vi.fn(), group: vi.fn(), groups: vi.fn(), create: vi.fn(), update: vi.fn(), createGroup: vi.fn(), updateGroup: vi.fn(), ssh: vi.fn(), install: vi.fn(), bbr: vi.fn(), navigation: vi.fn(), app: { token: 'verified', user: { id: 1 }, reauthRequired: false, sessionEpoch: 0 } }))
vi.mock('../stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: mocks.navigation }))
vi.mock('../api/client', async importOriginal => ({ ...await importOriginal<typeof import('../api/client')>(), fetchNode: mocks.node, fetchNodesPage: mocks.nodes, fetchNodeGroupDetail: mocks.group, fetchNodeGroupsPage: mocks.groups, createNode: mocks.create, updateNode: mocks.update, createNodeGroup: mocks.createGroup, updateNodeGroup: mocks.updateGroup, updateNodeSSH: mocks.ssh, reconcileNodeKernel: mocks.install }))
vi.mock('../api/nodeSystemActions', () => ({ enableNodeBBR: mocks.bbr, fetchNodeSystemActions: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn(), updateTrackedTask: vi.fn() }))
const node = { id: 1, name: 'Original asset', region: 'HK', address: 'edge.example.test', remark: 'Original note', lifecycle_status: 'active', is_enabled: true, connector_online: false, ssh_configured: false, ssh_verified_at: '', enabled_protocol_count: 0, created_at: '', updated_at: '' }
const group = { id: 1, name: 'Original group', code: 'edge', description: '', is_enabled: true, revision: 3, protocol_endpoint_ids: [1, 3], network_entry_ids: [7], updated_at: '', protocol_endpoint_count: 2, network_entry_count: 1 }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const checkbox = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { type: 'checkbox', checked: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked) }) } })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
const table = defineComponent({ props: ['rows'], template: '<div><slot /><div v-for="row in rows" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>' })
let view: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
  mocks.app.token = 'verified'; mocks.app.user.id = 1; mocks.app.sessionEpoch = 0; mocks.app.reauthRequired = false
  mocks.navigation.mockResolvedValue({ revision: 1, page_available: true, nodes: [] })
  mocks.node.mockResolvedValue(structuredClone(node)); mocks.nodes.mockResolvedValue({ items: [structuredClone(node)], total: 1 })
  mocks.group.mockResolvedValue(structuredClone(group)); mocks.groups.mockResolvedValue({ items: [structuredClone(group)], total: 1 })
  mocks.update.mockResolvedValue(node); mocks.updateGroup.mockResolvedValue(group)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(path: string, component: typeof Nodes | typeof NodeGroups) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/nodes', component: Nodes }, { path: '/admin/node-groups', component: NodeGroups }] })
  await router.push(path); await router.isReady()
  view = shallowMount(component, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiCheckbox: checkbox, UiButton: defineComponent({ template: '<button><slot /></button>' }), FormField },
    stubs: { UiInput: false, UiTextarea: false, UiCheckbox: false, UiButton: false, FormField: false, ModalDialog: modal, DetailDrawer: modal, DataTable: table, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /><slot name="actions" /></aside>' }, TableText: true, TimeBadge: true, PageRefreshButton: true, EndpointMultiLookup: true, NetworkEntryMultiLookup: true },
  } })
  await flushPromises()
  return router
}
async function click(label: string) { await view!.findAll('button').find(button => button.text().includes(label))!.trigger('click'); await flushPromises() }
function saveDraft(path: string) {
  expect(captureReauthenticationDrafts(1, path)).toEqual(expect.objectContaining({ ok: true, count: 1 }))
  view!.unmount(); view = undefined
}
function noWrites() { for (const mock of [mocks.create, mocks.update, mocks.createGroup, mocks.updateGroup, mocks.ssh, mocks.install, mocks.bbr]) expect(mock).not.toHaveBeenCalled() }
it('restores ordinary node creation, drops BBR execution choices and never invokes a write', async () => {
  const path = '/admin/nodes'
  await open(path, Nodes); await click('登记 VPS')
  await view!.get('#create-node-name').setValue('Unsaved asset')
  await view!.get('#create-node-region').setValue('JP')
  await view!.get('#node-create-form input[type="checkbox"]').setValue(true)
  saveDraft(path); confirmReauthenticationDraftIdentity(1, true); await open(path, Nodes)
  expect((view!.get('#create-node-name').element as HTMLInputElement).value).toBe('Unsaved asset')
  expect((view!.get('#create-node-region').element as HTMLInputElement).value).toBe('JP')
  expect((view!.get('#node-create-form input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
  expect(view!.text()).toContain('SSH 密码、私钥和提权凭据需重新输入')
  noWrites()
})
it('keeps an edited asset visible but blocks programmatic submit if fresh ordinary fields changed', async () => {
  const path = '/admin/nodes?node=1'
  await open(path, Nodes); await click('编辑资产'); await view!.get('#edit-node-name').setValue('Old unsaved name')
  saveDraft(path); mocks.node.mockResolvedValue({ ...node, name: 'Changed elsewhere' }); confirmReauthenticationDraftIdentity(1, true); await open(path, Nodes)
  expect((view!.get('#edit-node-name').element as HTMLInputElement).value).toBe('Old unsaved name')
  expect(view!.text()).toContain('不能直接覆盖服务器的新内容')
  await view!.get('#node-edit-form').trigger('submit'); await flushPromises(); noWrites()
})
it('restores a group with original version and members, then submits only on an explicit action', async () => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Unsaved group')
  saveDraft(path); confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  expect((view!.get('#node-group-name').element as HTMLInputElement).value).toBe('Unsaved group'); noWrites()
  await view!.get('#node-group-form').trigger('submit'); await flushPromises()
  expect(mocks.updateGroup).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Unsaved group', expected_revision: 3, protocol_endpoint_ids: [1, 3], network_entry_ids: [7] }))
})
it.each(['revision', 'membership'] as const)('blocks group overwrite after a fresh %s change without upgrading the old draft', async change => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Old draft')
  saveDraft(path); mocks.group.mockResolvedValue(change === 'revision' ? { ...group, revision: 4 } : { ...group, network_entry_ids: [8] })
  confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  expect((view!.get('#node-group-name').element as HTMLInputElement).value).toBe('Old draft')
  expect(view!.text()).toContain('当前草稿基于旧版本')
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  const payload = JSON.parse(sessionStorage.getItem('zboard.reauth-draft.v1.admin.node-groups.editor')!)
  expect(payload.data.revision).toBe(3); expect(payload.data.originalEntryIDs).toEqual([7])
  await view!.get('#node-group-form').trigger('submit'); await flushPromises(); noWrites()
})
it.each(['different-actor', 'permission-loss'] as const)('never recovers admin fields for %s', async reason => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Private old draft'); saveDraft(path)
  if (reason === 'different-actor') mocks.app.user.id = 2
  else mocks.navigation.mockResolvedValue({ page_available: false })
  confirmReauthenticationDraftIdentity(mocks.app.user.id, true); await open(path, NodeGroups)
  expect(view!.find('#node-group-form').exists()).toBe(false)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull(); noWrites()
})
it('retains stored fields when fresh group loading fails so retry does not require retyping', async () => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Retryable draft'); saveDraft(path)
  mocks.group.mockRejectedValueOnce(new Error('offline')); confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  expect(view!.find('#node-group-form').exists()).toBe(false)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).not.toBeNull()
  await click('重新检查草稿恢复')
  expect((view!.get('#node-group-name').element as HTMLInputElement).value).toBe('Retryable draft')
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull(); noWrites()
})
it('does not apply a late fresh resource response after the actor changes', async () => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Old actor draft'); saveDraft(path)
  let resolve!: (value: typeof group) => void
  mocks.group.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  mocks.app.user.id = 2; mocks.app.sessionEpoch++
  resolve(group); await flushPromises()
  expect(view!.find('#node-group-form').exists()).toBe(false); noWrites()
})

it('does not persist an open SSH credential form or reopen it after full-page authentication', async () => {
  const path = '/admin/nodes?node=1'
  await open(path, Nodes); await click('SSH 连接')
  await view!.get('#node-ssh-password').setValue('never-store-this-password')
  expect(captureReauthenticationDrafts(1, path)).toEqual(expect.objectContaining({ ok: true, count: 0 }))
  const values = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('')
  expect(values).not.toContain('never-store-this-password')
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true); await open(path, Nodes)
  expect(view!.find('#ssh-form').exists()).toBe(false); noWrites()
})
it('requires fresh target authorization, clearing an old draft on resource permission denial', async () => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Restricted resource draft'); saveDraft(path)
  mocks.group.mockRejectedValueOnce({ response: { status: 403 } })
  confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  expect(view!.find('#node-group-form').exists()).toBe(false)
  expect(sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)).toBeNull(); noWrites()
})

it('restores an unchanged asset baseline and waits for explicit submission', async () => {
  const path = '/admin/nodes?node=1'
  await open(path, Nodes); await click('编辑资产'); await view!.get('#edit-node-name').setValue('Operator draft')
  saveDraft(path); confirmReauthenticationDraftIdentity(1, true); await open(path, Nodes); noWrites()
  await view!.get('#node-edit-form').trigger('submit'); await flushPromises()
  expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Operator draft', region: 'HK', address: 'edge.example.test', lifecycle_status: 'active', is_enabled: true }))
  expect(mocks.ssh).not.toHaveBeenCalled(); expect(mocks.install).not.toHaveBeenCalled(); expect(mocks.bbr).not.toHaveBeenCalled()
})
it('does not replace a new local editor after a late resource response', async () => {
  const path = '/admin/node-groups'
  await open(path, NodeGroups); await click('编辑'); await view!.get('#node-group-name').setValue('Old draft'); saveDraft(path)
  let resolve!: (value: typeof group) => void
  mocks.group.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  confirmReauthenticationDraftIdentity(1, true); await open(path, NodeGroups)
  await click('创建权限组'); await view!.get('#node-group-name').setValue('New local work')
  resolve(group); await flushPromises()
  expect((view!.get('#node-group-name').element as HTMLInputElement).value).toBe('New local work'); noWrites()
})
