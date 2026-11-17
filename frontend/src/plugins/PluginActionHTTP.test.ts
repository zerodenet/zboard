import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PluginFrame from './PluginFrame.vue'
import { api, clearAuthToken, getAuthToken, setAuthToken } from '../api/client'
import { isAuthSessionExpired } from '../utils/authSession'
import type { PluginSession } from '../api/plugins'

// The Go acceptance test starts a signed native plugin and real HTTP handlers.
// No action response, session, SDK call or HTTP result is mocked here.
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'acceptance' }) }))
const origin = process.env.ZBOARD_ACTION_ACCEPTANCE_URL
const acceptance = origin ? describe : describe.skip
acceptance('signed native plugin through HTTP and iframe bridge', () => {
  let wrapper: VueWrapper | undefined, session: PluginSession, sequence = 0
  const actions: string[] = []
  const previousAdapter = api.defaults.adapter, previousBase = api.defaults.baseURL
  beforeEach(() => {
    // Match the document origin to the actual fixture, as in a deployed UI.
    ;(window as any).happyDOM.setURL(origin!)
    actions.length = 0
    setAuthToken(process.env.ZBOARD_ACTION_ACCEPTANCE_TOKEN!)
    api.defaults.baseURL = `${origin}/api/v1`
    api.defaults.adapter = (async config => {
      const body = typeof config.data === 'string' ? JSON.parse(config.data) : config.data
      if (body?.type === 'page.call') actions.push(body.action)
      const response = await fetch(`${config.baseURL}${config.url}`, { method: config.method?.toUpperCase(), headers: config.headers.toJSON() as Record<string, string>, body: config.data, signal: config.signal as AbortSignal })
      const data = await response.json()
      if (config.url?.endsWith('/session') && response.status === 200 && config.method === 'post') session = data.data
      const value = { status: response.status, statusText: response.statusText, config, headers: Object.fromEntries(response.headers.entries()), data }
      if (!response.ok) throw new AxiosError('request rejected', AxiosError.ERR_BAD_REQUEST, config, undefined, value)
      return value
    }) satisfies AxiosAdapter
  })
  afterEach(async () => {
    wrapper?.unmount(); wrapper = undefined
    await flushPromises()
    api.defaults.adapter = previousAdapter; api.defaults.baseURL = previousBase
    clearAuthToken(); vi.restoreAllMocks()
  })
  async function open() {
    wrapper = mount(PluginFrame, { attachTo: document.body, props: { pluginId: 'example.action-errors', pageId: 'actions', surface: 'account' } })
    await vi.waitFor(() => expect(wrapper!.find('iframe').exists()).toBe(true))
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    await vi.waitFor(() => expect(frame.contentWindow).not.toBeNull())
    const posted = vi.spyOn(frame.contentWindow!, 'postMessage')
    return { frame, posted }
  }
  function send(frame: HTMLIFrameElement, action: string) {
    const id = `native-http-${++sequence}`
    const event = new MessageEvent('message', { data: { source: 'zboard-plugin-ui', bridge_token: session.bridge_token, request_id: id, type: 'page.call', action, payload: { note: 'preserve input' } } })
    Object.defineProperty(event, 'source', { value: frame.contentWindow })
    window.dispatchEvent(event)
    return id
  }
  it.each([
    ['reject.retry', 'resource_busy', 'Resource is busy. Please try later.', true],
    ['reject.permanent', 'operation_closed', '此操作已关闭，请保留输入并联系管理员。', false],
    ['reject.namespace', 'session_expired', 'Plugin-specific reason, not host authentication.', true],
    ['reject.html', 'public_text', `<img src=x onerror="alert('unsafe')"> is plain text.`, false],
  ])('preserves $0 rejection without global logout or automatic replay', async (action, reason, message, retryable) => {
    const { frame, posted } = await open()
    const token = getAuthToken(), id = send(frame, action as string)
    await vi.waitFor(() => expect(posted).toHaveBeenCalled(), { timeout: 5000 })
    expect(posted.mock.calls[0][0]).toMatchObject({ request_id: id, ok: false, source: 'zboard-plugin-host', source_kind: 'plugin', code: 'plugin_action_rejected', plugin_code: reason, error: message, retryable })
    expect(actions).toEqual([action])
    expect(isAuthSessionExpired()).toBe(false)
    expect(getAuthToken()).toBe(token)
  })
  it('keeps raw internal and malformed errors behind host failure rules', async () => {
    const { frame, posted } = await open()
    for (const action of ['reject.legacy', 'reject.malformed', 'reject.long', 'reject.timeout']) {
      posted.mockClear(); send(frame, action)
      await vi.waitFor(() => expect(posted).toHaveBeenCalled(), { timeout: 5000 })
      const response = posted.mock.calls[0][0] as Record<string, any>
      expect(response.ok).toBe(false)
      expect(response.code).toBe(action === 'reject.timeout' ? 'deadline_exceeded' : 'unavailable')
      expect(response.source_kind).toBeUndefined()
      expect(JSON.stringify(response)).not.toMatch(/internal-secret|private|password/)
    }
  })
  it('preserves existing successful action results and input', async () => {
    const { frame, posted } = await open(); send(frame, 'success.echo')
    await vi.waitFor(() => expect(posted).toHaveBeenCalled(), { timeout: 5000 })
    expect(posted.mock.calls[0][0]).toMatchObject({ ok: true, result: { action: 'success.echo', payload: { note: 'preserve input' } } })
  })
  it('gives host session revocation precedence over a delayed plugin rejection', async () => {
    const { frame, posted } = await open(); send(frame, 'reject.slow')
    await vi.waitFor(() => expect(actions).toContain('reject.slow'))
    await api.delete('/plugin-ui/session', { headers: { 'X-Plugin-Session': session.token } })
    await vi.waitFor(() => expect(posted).toHaveBeenCalled(), { timeout: 5000 })
    expect(posted.mock.calls[0][0]).toMatchObject({ ok: false, code: 'session_expired' })
    expect(JSON.stringify(posted.mock.calls[0][0])).not.toContain('late_result')
    expect(isAuthSessionExpired()).toBe(false)
  })
})
