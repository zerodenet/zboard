import { mount, flushPromises } from '@vue/test-utils'
import { computed, defineComponent, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { previewSubscriptionTemplate, type SubscriptionTemplatePreview } from '../api/client'
import { useSubscriptionTemplatePreview } from './useSubscriptionTemplatePreview'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
vi.mock('../api/client', () => ({ previewSubscriptionTemplate: vi.fn() }))
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
const preview = (content: string): SubscriptionTemplatePreview => ({ content, content_type: 'application/json', bytes: content.length, line_count: 1, truncated: false, groups: [] })
describe('live subscription template preview', () => {
  let wrapper: ReturnType<typeof mount> | undefined
  const state = reactive({ enabled: true, identity: 'admin:1', invalid: '', customization: defaultSubscriptionCustomization('clash') })
  let live!: ReturnType<typeof useSubscriptionTemplatePreview>
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); Object.assign(state, { enabled: true, identity: 'admin:1', invalid: '', customization: defaultSubscriptionCustomization('clash') }); vi.mocked(previewSubscriptionTemplate).mockResolvedValue(preview('current')) })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })
  function open() { wrapper = mount(defineComponent({ setup() { live = useSubscriptionTemplatePreview({ enabled: computed(() => state.enabled), identity: computed(() => state.identity), invalid: computed(() => state.invalid), request: computed(() => ({ renderer: 'clash', customization: state.customization, live: true, full_content: true })) }); return () => null } })) }
  async function tick() { await vi.advanceTimersByTimeAsync(450); await flushPromises() }
  it('coalesces edits and sends an immutable request snapshot', async () => {
    open(); state.customization.policy_groups[0].name = 'first'; state.customization.policy_groups[0].name = 'latest'
    await tick()
    expect(previewSubscriptionTemplate).toHaveBeenCalledOnce()
    const request = vi.mocked(previewSubscriptionTemplate).mock.calls[0]![0]
    expect(request.customization!.policy_groups[0].name).toBe('latest')
    state.customization.policy_groups[0].name = 'after request'
    expect(request.customization!.policy_groups[0].name).toBe('latest')
    expect(live.stale.value).toBe(true)
  })
  it('aborts old reads and refuses a late result even if the transport ignores cancellation', async () => {
    const old = deferred<SubscriptionTemplatePreview>()
    vi.mocked(previewSubscriptionTemplate).mockReturnValueOnce(old.promise)
    open(); await tick()
    const signal = vi.mocked(previewSubscriptionTemplate).mock.calls[0]![1]!.signal
    state.customization.policy_groups[0].name = 'new'
    expect(signal!.aborted).toBe(true)
    await tick()
    old.resolve(preview('old')); await flushPromises()
    expect(live.result.value?.content).toBe('current')
    expect(live.stale.value).toBe(false)
  })
  it('retains the last successful output for invalid drafts and recovers without saving', async () => {
    open(); await tick(); state.invalid = 'Raw JSON 尚未完成'
    await tick(); expect(previewSubscriptionTemplate).toHaveBeenCalledOnce()
    expect(live.result.value?.content).toBe('current'); expect(live.stale.value).toBe(true)
    expect(live.error.value).toContain('尚未完成')
    state.invalid = ''; await tick(); expect(previewSubscriptionTemplate).toHaveBeenCalledTimes(2)
  })
  it('clears private results at an identity boundary and blocks reads after a cross-tab credential change', async () => {
    open(); await tick(); state.identity = 'admin:2'
    expect(live.result.value).toBeNull(); await tick()
    window.dispatchEvent(new StorageEvent('storage', { key: 'zboard.auth.token', newValue: 'new-credential' }))
    expect(live.result.value).toBeNull()
    state.customization.policy_groups[0].name = 'must not send'; await tick()
    expect(previewSubscriptionTemplate).toHaveBeenCalledTimes(2)
    expect(live.error.value).toContain('登录状态已变化')
  })
  it('stops on close and retries a failed read without blanking its last good result', async () => {
    open(); await tick()
    vi.mocked(previewSubscriptionTemplate).mockRejectedValueOnce(new Error('offline'))
    await live.refresh(); expect(live.result.value?.content).toBe('current'); expect(live.stale.value).toBe(true)
    await live.refresh(); expect(live.error.value).toBe('')
    state.enabled = false; state.customization.policy_groups[0].name = 'closed'
    await tick(); expect(previewSubscriptionTemplate).toHaveBeenCalledTimes(3); expect(live.result.value).toBeNull()
  })
})
