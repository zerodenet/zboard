import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Users from './Users.vue'
import Providers from './Providers.vue'
import Protocols from './Protocols.vue'
import FormField from '../components/FormField.vue'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const mocks = vi.hoisted(() => ({ user: vi.fn(), users: vi.fn(), createUser: vi.fn(), updateUser: vi.fn(), accounts: vi.fn(), createProvider: vi.fn(), updateProvider: vi.fn(), endpoint: vi.fn(), endpoints: vi.fn(), createEndpoint: vi.fn(), updateEndpoint: vi.fn(), navigation: vi.fn(), app: { token: 'verified', user: { id: 1 }, reauthRequired: false, sessionEpoch: 0 } }))
vi.mock('../stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('../api/menus', () => ({ fetchNavigation: mocks.navigation }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn(), trackedTaskSummaries: {} }))
vi.mock('../api/client', async importOriginal => ({ ...await importOriginal<typeof import('../api/client')>(), fetchAdminUserDetail: mocks.user, fetchUsersPage: mocks.users, createAdminUser: mocks.createUser, updateAdminUser: mocks.updateUser, fetchProviderAccounts: mocks.accounts, createProviderAccount: mocks.createProvider, updateProviderAccount: mocks.updateProvider, fetchProviderDefinitions: vi.fn(async () => [{ key: 'cloudflare', name: 'Cloudflare', capabilities: ['dns.records'] }]), fetchProtocolEndpoint: mocks.endpoint, fetchProtocolEndpointsPage: mocks.endpoints, createProtocolEndpoint: mocks.createEndpoint, updateProtocolEndpoint: mocks.updateEndpoint, fetchNodesPage: vi.fn(async () => ({ items: [{ id: 4, name: 'Edge', address: 'edge.test' }], total: 1 })), fetchManagedCertificatesPage: vi.fn(async () => ({ items: [], total: 0 })), getVersion: vi.fn(async () => ({ protocol_capabilities: {} })) }))
const at = '2026-10-27T00:00:00Z'
const user = { id: 2, email: 'owner@example.test', status: 'active', is_admin: false, updated_at: at, created_at: at, active_subscription_count: 0, total_subscription_count: 0, pending_order_count: 0, total_order_count: 0 }
const provider = { id: 3, name: 'DNS', provider_key: 'cloudflare', revision: 7, status: 'active', capabilities: [], usage_count: 0, credential_prefix: 'not-a-token', created_at: at, updated_at: at }
const endpoint = { id: 5, node_id: 4, name: 'Edge protocol', protocol: 'vless', address: 'edge.test', listen_address: '0.0.0.0', port: 443, public_port: 443, parent_protocol_id: null, managed_certificate_id: null, multiplier_milli: 1000, sort_order: 0, managed_principal_ready: true, is_active: true, kernel_supported: true, config: '{"type":"vless","users":[],"private_key":"SERVER_SECRET"}', client_config: '{"type":"vless","server":"edge.test","port":443}', egress_config: '', optional_config: '{}', tags: '[]', node_group_memberships: [], usage: { active_flows: 0, active_users: 0, active_credentials: 0, used_bytes_today: 0, used_bytes_total: 0 }, created_at: at, updated_at: at }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const checkbox = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { type: 'checkbox', checked: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked) }) } })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
  Object.assign(mocks.app, { token: 'verified', reauthRequired: false, sessionEpoch: 0 }); mocks.app.user.id = 1
  mocks.navigation.mockResolvedValue({ revision: 1, page_available: true, nodes: [] })
  mocks.user.mockResolvedValue(structuredClone(user)); mocks.users.mockResolvedValue({ items: [structuredClone(user)], total: 1 })
  mocks.accounts.mockResolvedValue([structuredClone(provider)]); mocks.endpoints.mockResolvedValue({ items: [structuredClone(endpoint)], total: 1 }); mocks.endpoint.mockResolvedValue(structuredClone(endpoint))
  mocks.updateProvider.mockResolvedValue(provider)
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open(path: string, component: typeof Users | typeof Providers | typeof Protocols) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/users', component: Users }, { path: '/admin/providers', component: Providers }, { path: '/admin/protocols', component: Protocols }] })
  await router.push(path); await router.isReady()
  view = shallowMount(component, { global: { plugins: [router], renderStubDefaultSlot: true, components: { UiInput: input, UiTextarea: input, UiSelect: input, UiCheckbox: checkbox, UiButton: defineComponent({ template: '<button><slot /></button>' }), FormField }, stubs: { UiInput: false, UiTextarea: false, UiSelect: false, UiCheckbox: false, UiButton: defineComponent({ template: '<button><slot /></button>' }), FormField: false, ModalDialog: modal, DetailDrawer: modal, DataTable: { template: '<div><slot /></div>' }, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /><slot name="actions" /></aside>' }, NodeGroupMembershipEditor: true, NodeLookup: true, NetworkEntries: true } } })
  await flushPromises()
}
async function click(label: string) { await view!.findAll('button').find(button => button.text().includes(label))!.trigger('click'); await flushPromises() }
function capture(path: string) { expect(captureReauthenticationDrafts(1, path)).toEqual(expect.objectContaining({ ok: true, count: 1 })); view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true) }
function noWrites() { for (const write of [mocks.createUser, mocks.updateUser, mocks.createProvider, mocks.updateProvider, mocks.createEndpoint, mocks.updateEndpoint]) expect(write).not.toHaveBeenCalled() }
it('restores ordinary create-user fields without a password and never creates automatically', async () => {
  const path = '/admin/users'; await open(path, Users); await click('创建用户')
  await view!.get('#create-user-email').setValue('draft@example.test'); capture(path); await open(path, Users)
  expect((view!.get('#create-user-email').element as HTMLInputElement).value).toBe('draft@example.test')
  expect((view!.get('#create-user-password').element as HTMLInputElement).value).toBe(''); noWrites()
})
it.each(['user-password', 'provider-token', 'protocol-json'] as const)('rejects whole capture for unsaved %s without leaving or partially dropping ordinary work', async kind => {
  const path = kind === 'user-password' ? '/admin/users' : kind === 'provider-token' ? '/admin/providers' : '/admin/protocols?kind=listener'
  await open(path, kind === 'user-password' ? Users : kind === 'provider-token' ? Providers : Protocols)
  if (kind === 'user-password') { await click('创建用户'); await view!.get('#create-user-email').setValue('draft@example.test'); await view!.get('#create-user-password').setValue('USER_SECRET') }
  else if (kind === 'provider-token') { await click('添加供应商账户'); await view!.get('#provider-token').setValue('TOKEN_SECRET') }
  else { await view!.get('[aria-label="编辑协议服务 Edge protocol"]').trigger('click'); await flushPromises(); (view!.vm as any).$.setupState.form.config = '{"type":"vless","private_key":"NEW_SECRET"}' }
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  expect(sessionStorage.length).toBe(0); expect(view!.find('form').exists()).toBe(true); noWrites()
})
it('restores a provider name with the original CAS version and sends only after explicit submit', async () => {
  const path = '/admin/providers'; await open(path, Providers); await click('编辑'); await view!.get('#provider-name').setValue('Draft DNS'); capture(path); await open(path, Providers)
  noWrites(); await view!.get('#provider-account-form').trigger('submit'); await flushPromises()
  expect(mocks.updateProvider).toHaveBeenCalledWith(3, { name: 'Draft DNS', api_token: undefined, expected_revision: 7 })
})
it.each(['user', 'provider', 'protocol'] as const)('retains %s draft on fresh conflict and rejects programmatic submission', async kind => {
  const path = kind === 'user' ? '/admin/users' : kind === 'provider' ? '/admin/providers' : '/admin/protocols?kind=listener'
  await open(path, kind === 'user' ? Users : kind === 'provider' ? Providers : Protocols)
  if (kind === 'user') { await click('账户与权限'); await view!.get('#edit-user-status').setValue('suspended') }
  else if (kind === 'provider') { await click('编辑'); await view!.get('#provider-name').setValue('Draft DNS') }
  else { await view!.get('[aria-label="编辑协议服务 Edge protocol"]').trigger('click'); await flushPromises(); await view!.get('#protocol-name').setValue('Draft protocol') }
  capture(path)
  if (kind === 'user') mocks.user.mockResolvedValue({ ...user, updated_at: '2026-10-27T01:00:00Z' })
  else if (kind === 'provider') mocks.accounts.mockResolvedValue([{ ...provider, revision: 8 }])
  else mocks.endpoint.mockResolvedValue({ ...endpoint, updated_at: '2026-10-27T01:00:00Z' })
  await open(path, kind === 'user' ? Users : kind === 'provider' ? Providers : Protocols)
  expect(view!.text()).toContain('不能直接覆盖服务器的新内容')
  await view!.get(kind === 'user' ? '#edit-user-form' : kind === 'provider' ? '#provider-account-form' : '#protocol-form').trigger('submit'); await flushPromises(); noWrites()
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
})
it('restores an endpoint name using fresh unchanged server config without persisting its secret', async () => {
  const path = '/admin/protocols?kind=listener'; await open(path, Protocols)
  await view!.get('[aria-label="编辑协议服务 Edge protocol"]').trigger('click'); await flushPromises(); await view!.get('#protocol-name').setValue('Draft protocol')
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  const saved = Array.from({ length: sessionStorage.length }, (_, n) => sessionStorage.getItem(sessionStorage.key(n)!)).join('')
  expect(saved).not.toContain('SERVER_SECRET'); expect(saved).not.toContain('config')
  view!.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, true); await open(path, Protocols)
  expect((view!.get('#protocol-name').element as HTMLInputElement).value).toBe('Draft protocol')
  expect((view!.vm as any).$.setupState.form.config).toContain('SERVER_SECRET'); noWrites()
})
it('does not apply a late user draft detail after a new local editor opens', async () => {
  const path = '/admin/users'; await open(path, Users); await click('账户与权限'); await view!.get('#edit-user-status').setValue('suspended'); capture(path)
  let resolve!: (value: typeof user) => void
  mocks.user.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  await open(path, Users); await click('创建用户'); await view!.get('#create-user-email').setValue('new-local@example.test')
  resolve(user); await flushPromises()
  expect((view!.get('#create-user-email').element as HTMLInputElement).value).toBe('new-local@example.test')
  expect(view!.find('#edit-user-form').exists()).toBe(false); expect(sessionStorage.length).toBeGreaterThan(0); noWrites()
})
it('refuses resource-permission restoration', async () => {
  const path = '/admin/users'; await open(path, Users); await click('账户与权限'); await view!.get('#edit-user-status').setValue('suspended'); capture(path)
  mocks.user.mockRejectedValue({ response: { status: 403 } }); await open(path, Users)
  expect(view!.find('#edit-user-form').exists()).toBe(false); expect(sessionStorage.length).toBe(0); noWrites()
})
