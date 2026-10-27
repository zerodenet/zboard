import { defineComponent, h, ref } from 'vue'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App.vue'
import { api, clearAuthToken, getAuthToken } from '../api/client'
import { useAppStore } from '../stores/app'
import { useNavigation } from '../stores/navigation'
import NavigationPage from './NavigationPage.vue'
import UiButton from './UiButton.vue'
import { installAuthSessionNavigation } from '../utils/authSessionNavigation'
import { resetAuthSessionExpired } from '../utils/authSession'
import { useUnsavedChangesGuard } from '../composables/useFormState'
import { clearTrackedTasks, trackedTaskIDs } from '../utils/taskTracker'
import IdentityLoginComplete from '../views/IdentityLoginComplete.vue'
import { useReauthenticationDraft } from '../composables/useReauthenticationDraft'
import { clearReauthenticationDrafts, REAUTH_DRAFT_TTL_MS } from '../utils/reauthenticationDrafts'

let wrapper: VueWrapper | undefined
let cleanup: (() => void) | undefined
let expired = false
let identityGate: Promise<void> | undefined
let failVerification = false
let networkFailure = false
let navigationForbidden = false
let passwordSet: boolean | null = true
let externalMethods: object[] = []
let actor = { id: 1, email: 'first@example.test', is_admin: true }
const writes = vi.fn()
const requests = vi.fn()
const adapter: AxiosAdapter = async config => {
  requests(config.url)
  if (config.url === '/auth/me' && identityGate) await identityGate
  if (config.url === '/auth/me' && networkFailure) throw new AxiosError('offline', 'ERR_NETWORK', config)
  if (config.url === '/navigation' && navigationForbidden) {
    throw new AxiosError('forbidden', undefined, config, undefined, { status: 403, data: {}, config, headers: {}, statusText: '' })
  }
  if (config.method === 'post' && config.url === '/payment') writes()
  if ((expired && !['/auth/login', '/auth/me', '/plugin-ui/slots'].includes(config.url || '')) || config.url === '/auth/me' && failVerification) {
    throw new AxiosError('expired', undefined, config, undefined, { status: 401, data: {}, config, headers: {}, statusText: '' })
  }
  const data = config.url === '/auth/oidc/finish' ? { auth: { token: `oauth-${actor.id}` }, user: { ...actor, is_admin: true } }
    : config.url === '/auth/login' ? { auth: { token: `new-${actor.id}` }, user: { ...actor, is_admin: true } }
    : config.url === '/auth/me' ? actor
    : config.url === '/account/identities/security' ? { password_set: passwordSet }
    : config.url === '/plugin-ui/slots' ? externalMethods
    : config.url === '/navigation' ? { revision: 1, page_available: true, nodes: [] }
    : { maintenance: { enabled: false }, announcements: [], announcement_unread_count: 0 }
  return { status: 200, data: { data }, config, headers: {}, statusText: '' }
}

beforeEach(() => {
  localStorage.clear(); sessionStorage.clear(); clearReauthenticationDrafts(); identityGate = undefined; resetAuthSessionExpired(); clearTrackedTasks()
  expired = false; failVerification = false; networkFailure = false; navigationForbidden = false
  passwordSet = true; externalMethods = []
  actor = { id: 1, email: 'first@example.test', is_admin: true }
  writes.mockClear(); requests.mockClear(); api.defaults.adapter = adapter
})
afterEach(() => { wrapper?.unmount(); cleanup?.(); clearAuthToken(); clearReauthenticationDrafts(); resetAuthSessionExpired(); vi.restoreAllMocks() })

async function render() {
  const pinia = createPinia()
  const store = useAppStore(pinia)
  store.setToken('initial'); store.setUser(actor)
  const Editor = defineComponent({ setup() {
    const draft = ref('')
    useReauthenticationDraft({ scope: 'test.editor', permission: 'admin', ready: () => true, read: () => ({ body: draft.value }), sanitize: (v: any) => typeof v?.body === 'string' ? { body: v.body } : undefined, restore: v => { draft.value = v.body } })
    useUnsavedChangesGuard(() => Boolean(draft.value), () => false)
    return () => h('input', { 'data-draft': '', value: draft.value, onInput: (event: Event) => { draft.value = (event.target as HTMLInputElement).value } })
  } })
  const Layout = defineComponent({ setup() {
    const navigation = useNavigation('admin')
    return () => h(NavigationPage, { available: navigation.pageAvailable.value, loading: navigation.pageLoading.value, error: navigation.pageError.value, failure: navigation.failure.value }, { default: () => h(RouterView) })
  } })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/admin', component: Layout, meta: { requiresAuth: true, requiresAdmin: true }, children: [{ path: 'edit', component: Editor }] },
    { path: '/account', component: Editor, meta: { requiresAuth: true } },
    { path: '/', component: { template: '<p>首页</p>' } },
    { path: '/login', component: { template: '<p>现有登录入口</p>' } },
    { path: '/auth/oidc/complete', component: IdentityLoginComplete },
    { path: '/admin/dashboard', component: { template: '<p>新账户首页</p>' } },
  ] })
  await router.push('/admin/edit?page=3#draft')
  cleanup = installAuthSessionNavigation(router, () => store.clear(), () => store.suspendSession())
  wrapper = mount(App, { global: { plugins: [pinia, router], components: { UiButton }, stubs: {
    ModalDialog: { template: '<section role="dialog"><slot /></section>' },
    NavigationProgress: true, FeedbackHost: true, MaintenanceScreen: true, AnnouncementStack: true,
  } } })
  await flushPromises()
  await wrapper.get('[data-draft]').setValue('recoverable draft')
  return { store, router }
}

async function expire() {
  expired = true
  await Promise.allSettled([api.post('/payment'), api.get('/navigation')])
  await flushPromises()
}
async function authenticate() {
  await wrapper!.get('input[type="password"]').setValue('long-password-123')
  await wrapper!.get('form').trigger('submit')
  await flushPromises()
}

describe('in-page session restoration', () => {
  it('keeps one dialog, page position and same-account draft without replaying a failed payment', async () => {
    const { store, router } = await render()
    const editor = wrapper!.get('[data-draft]').element
    await expire()
    expect(wrapper!.findAll('[role="dialog"]')).toHaveLength(1)
    expect(router.currentRoute.value.fullPath).toBe('/admin/edit?page=3#draft')
    expect(wrapper!.get('[data-draft]').element).toBe(editor)
    expect(store.reauthRequired).toBe(true)
    expired = false
    await authenticate()
    expect(store.reauthRequired).toBe(false)
    expect(wrapper!.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper!.get('[data-draft]').element).toBe(editor)
    expect((editor as HTMLInputElement).value).toBe('recoverable draft')
    expect(requests).toHaveBeenCalledWith('/auth/me')
    expect(requests.mock.calls.filter(([path]) => path === '/navigation').length).toBeGreaterThan(1)
    expect(writes).toHaveBeenCalledOnce()
  })

  it('verifies permissions and isolates drafts and tracked tasks when the account changes', async () => {
    const { store, router } = await render()
    const editor = wrapper!.get('[data-draft]').element
    trackedTaskIDs.value = [99]
    await expire()
    actor = { id: 2, email: 'second@example.test', is_admin: false }
    expired = false
    await authenticate()
    expect(store.user.id).toBe(2)
    expect(store.isAdmin).toBe(false)
    expect(router.currentRoute.value.path).toBe('/account')
    expect(wrapper!.get('[data-draft]').element).not.toBe(editor)
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('')
    expect(trackedTaskIDs.value).toEqual([])
  })

  it('retains the recovery dialog and draft when identity verification fails', async () => {
    const { store } = await render()
    await expire()
    failVerification = true
    await authenticate()
    expect(store.reauthRequired).toBe(true)
    expect(store.user.id).toBe(1)
    expect(store.token).toBe('')
    expect(getAuthToken()).toBe('')
    expect(wrapper!.find('[role="dialog"]').exists()).toBe(true)
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('recoverable draft')
    expect((wrapper!.get('input[type="password"]').element as HTMLInputElement).value).toBe('')
  })

  it('discards the old admin page when the same account loses its admin role', async () => {
    const { store, router } = await render()
    const editor = wrapper!.get('[data-draft]').element
    await expire()
    actor = { ...actor, is_admin: false }
    expired = false
    await authenticate()
    expect(store.isAdmin).toBe(false)
    expect(router.currentRoute.value.path).toBe('/account')
    expect(wrapper!.get('[data-draft]').element).not.toBe(editor)
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('')
  })

  it('honors refreshed page permissions even when the account and admin role stay the same', async () => {
    const { store, router } = await render()
    await expire()
    expired = false; navigationForbidden = true
    await authenticate()
    expect(store.reauthRequired).toBe(false)
    expect(store.isAdmin).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe('/admin/edit?page=3#draft')
    expect(wrapper!.find('[data-draft]').exists()).toBe(false)
    expect(wrapper!.text()).toContain('访问权限不足')
  })

  it('keeps the draft and recovery entry on network failure and allows another explicit attempt', async () => {
    const { store } = await render()
    await expire()
    expired = false; networkFailure = true
    await authenticate()
    expect(store.reauthRequired).toBe(true)
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('recoverable draft')
    networkFailure = false
    await authenticate()
    expect(store.reauthRequired).toBe(false)
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('recoverable draft')
  })

  it('explicitly exits recovery, clears the old page and does not replay any action', async () => {
    const { store, router } = await render()
    await expire()
    await wrapper!.findAll('button').find(button => button.text() === '退出并返回首页')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
    expect(store.user.id).toBe(0)
    expect(store.reauthRequired).toBe(false)
    expect(wrapper!.find('[data-draft]').exists()).toBe(false)
    expect(writes).toHaveBeenCalledOnce()
  })

  it('offers actual external login contributions without assuming a password exists', async () => {
    passwordSet = false
    externalMethods = [{ plugin_id: 'oidc', generation: 1, slot: { title: 'OIDC 登录', id: 'login', surface: 'public', slot: 'auth.login.methods', entrypoint: 'ui/login.html' } }]
    const { router, store } = await render()
    expect(store.passwordLoginAvailable).toBe(false)
    await expire()
    expect(wrapper!.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper!.text()).toContain('OIDC 登录')
    expect(wrapper!.text()).toContain('本标签页暂存 10 分钟')
    await wrapper!.findAll('button').find(button => button.text() === '打开第三方登录入口')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query).toEqual({ redirect: '/admin/edit?page=3#draft', reason: 'session-expired' })
    expect(wrapper!.find('[data-draft]').exists()).toBe(false)
    expect(requests.mock.calls.filter(([url]) => url === '/auth/login')).toHaveLength(0)
  })

  it('treats unknown password availability as an explicit choice rather than a working login promise', async () => {
    passwordSet = null
    await render()
    await expire()
    expect(wrapper!.find('input[type="password"]').exists()).toBe(false)
    await wrapper!.findAll('button').find(button => button.text() === '我已设置邮箱密码')!.trigger('click')
    expect(wrapper!.find('input[type="password"]').exists()).toBe(true)
    expect(requests.mock.calls.filter(([url]) => url === '/auth/login')).toHaveLength(0)
  })
})


describe('full-page identity recovery uses verified identity and permission', () => {
  async function leaveForOAuth() {
    passwordSet = false
    externalMethods = [{ plugin_id: 'oidc', generation: 1, slot: { title: 'OIDC 登录', id: 'login', surface: 'public', slot: 'auth.login.methods', entrypoint: 'ui/login.html' } }]
    const current = await render()
    await expire()
    await wrapper!.findAll('button').find(button => button.text() === '打开第三方登录入口')!.trigger('click')
    await flushPromises()
    return current
  }
  it('restores the same actor only after /me and fresh page access; never resends a write', async () => {
    const { router } = await leaveForOAuth()
    expect(wrapper!.find('[data-draft]').exists()).toBe(false)
    let release!: () => void
    identityGate = new Promise<void>(resolve => { release = resolve })
    const reads = vi.spyOn(sessionStorage, 'getItem')
    expired = false
    await router.push('/auth/oidc/complete'); await flushPromises()
    expect(reads.mock.calls.filter(([key]) => key.startsWith('zboard.reauth-draft.v1.'))).toHaveLength(0)
    expect(writes).toHaveBeenCalledOnce()
    release(); await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/edit?page=3#draft')
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value).toBe('recoverable draft')
    expect(reads.mock.calls.some(([key]) => key.startsWith('zboard.reauth-draft.v1.'))).toBe(true)
    expect(writes).toHaveBeenCalledOnce()
  })
  it.each(['different-actor', 'lost-role', 'lost-page', 'expired'] as const)('does not expose the old draft for %s', async failure => {
    const { router } = await leaveForOAuth()
    const reads = vi.spyOn(sessionStorage, 'getItem')
    if (failure === 'different-actor') actor = { id: 2, email: 'other@example.test', is_admin: true }
    if (failure === 'lost-role') actor = { ...actor, is_admin: false }
    if (failure === 'lost-page') navigationForbidden = true
    if (failure === 'expired') vi.spyOn(Date, 'now').mockReturnValue(Date.now() + REAUTH_DRAFT_TTL_MS + 1)
    expired = false
    await router.push('/auth/oidc/complete'); await flushPromises()
    expect(reads.mock.calls.filter(([key]) => key.startsWith('zboard.reauth-draft.v1.'))).toHaveLength(0)
    expect(sessionStorage.getItem('zboard.reauth-drafts.manifest.v1')).toBeNull()
    expect(wrapper!.find('[data-draft]').exists() ? (wrapper!.get('[data-draft]').element as HTMLInputElement).value : '').toBe('')
    expect(writes).toHaveBeenCalledOnce()
  })
  it('keeps the captured draft when the external-login button is clicked twice immediately', async () => {
    passwordSet = false
    externalMethods = [{ plugin_id: 'oidc', generation: 1, slot: { title: 'OIDC 登录', id: 'login', surface: 'public', slot: 'auth.login.methods', entrypoint: 'ui/login.html' } }]
    const { router } = await render(); await expire()
    const button = wrapper!.findAll('button').find(button => button.text() === '打开第三方登录入口')!.element as HTMLButtonElement
    button.click(); button.click(); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(sessionStorage.getItem('zboard.reauth-drafts.manifest.v1')).not.toBeNull()
    expect(writes).toHaveBeenCalledOnce()
  })
  it('keeps the mounted original form and refuses external navigation when storage actually rejects the write', async () => {
    passwordSet = false
    externalMethods = [{ plugin_id: 'oidc', generation: 1, slot: { title: 'OIDC 登录', id: 'login', surface: 'public', slot: 'auth.login.methods', entrypoint: 'ui/login.html' } }]
    const { router, store } = await render(); await expire()
    const original = wrapper!.get('[data-draft]').element
    const storageWrite = vi.spyOn(sessionStorage, 'setItem').mockImplementation(() => { throw new DOMException('quota', 'QuotaExceededError') })
    await wrapper!.findAll('button').find(button => button.text() === '打开第三方登录入口')!.trigger('click'); await flushPromises()
    expect(storageWrite).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.fullPath).toBe('/admin/edit?page=3#draft')
    expect(wrapper!.get('[data-draft]').element).toBe(original)
    expect((original as HTMLInputElement).value).toBe('recoverable draft')
    expect(store.user.id).toBe(1); expect(store.reauthRequired).toBe(true)
    expect(wrapper!.text()).toContain('尚未离开此页')
    expect(sessionStorage.getItem('zboard.reauth-drafts.manifest.v1')).toBeNull()
    expect(writes).toHaveBeenCalledOnce()
  })
  it('keeps an oversize draft in the current page instead of navigating or dropping it', async () => {
    passwordSet = false
    externalMethods = [{ plugin_id: 'oidc', generation: 1, slot: { title: 'OIDC 登录', id: 'login', surface: 'public', slot: 'auth.login.methods', entrypoint: 'ui/login.html' } }]
    const { router } = await render()
    await wrapper!.get('[data-draft]').setValue('x'.repeat(70_000))
    await expire()
    await wrapper!.findAll('button').find(button => button.text() === '打开第三方登录入口')!.trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/edit')
    expect((wrapper!.get('[data-draft]').element as HTMLInputElement).value.length).toBe(70_000)
    expect(wrapper!.text()).toContain('尚未离开此页')
    expect(writes).toHaveBeenCalledOnce()
  })
})
