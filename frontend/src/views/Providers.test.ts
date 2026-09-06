import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Providers from './Providers.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), remove: vi.fn(), confirm: vi.fn(), verify: vi.fn(),
}))
vi.mock('../api/client', () => ({
  fetchProviderAccounts: mocks.list,
  fetchProviderDefinitions: vi.fn(async () => []),
  createProviderAccount: vi.fn(), verifyProviderAccount: mocks.verify,
  deleteProviderAccount: mocks.remove,
}))
vi.mock('../utils/feedback', () => ({ confirmAction: mocks.confirm, notify: vi.fn() }))

const account = { id: 7, name: 'Fixture provider', provider_key: 'cloudflare', capabilities: [], usage_count: 0, status: 'active' }
function render() {
  return mount(Providers, { global: { plugins: [PrimeVue], stubs: { teleport: true } } })
}
async function openActions(wrapper: ReturnType<typeof render>) {
  await wrapper.get('[data-row-action-trigger="provider-7"]').trigger('click')
}
describe('Provider integration deletion', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue([account])
    mocks.confirm.mockResolvedValue(true)
    mocks.remove.mockResolvedValue({ id: 7, deleted: true })
  })
  it('blocks deletion while DNS or certificate usage exists', async () => {
    mocks.list.mockResolvedValue([{ ...account, usage_count: 1 }])
    const wrapper = render()
    await flushPromises()
    await openActions(wrapper)
    const button = wrapper.findAll('button').find(item => item.text() === '删除')!
    expect(button.attributes('disabled')).toBeDefined()
    expect(mocks.remove).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('confirms and removes only the selected unused integration', async () => {
    const wrapper = render()
    await flushPromises()
    mocks.list.mockResolvedValue([])
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '删除')!.trigger('click')
    await flushPromises()
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('Cloudflare 账户及共享 Token') }))
    expect(mocks.remove).toHaveBeenCalledWith(7)
    expect(wrapper.text()).not.toContain('Fixture provider')
    wrapper.unmount()
  })
  it('retains the row and explains a backend reference conflict', async () => {
    mocks.remove.mockRejectedValue({ response: { data: { message: '仍有证书引用' } } })
    const wrapper = render()
    await flushPromises()
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '删除')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('仍有证书引用')
    expect(wrapper.text()).toContain('Fixture provider')
    wrapper.unmount()
  })
  it('keeps verification failure visible after refreshing the account status', async () => {
    mocks.verify.mockRejectedValueOnce({ response: { data: { message: 'Token 权限不足' } } })
    const wrapper = render()
    await flushPromises()
    mocks.list.mockResolvedValue([{ ...account, status: 'invalid' }])
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '重新验证')!.trigger('click')
    await flushPromises()
    expect(mocks.verify).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('Token 权限不足')
    expect(wrapper.text()).toContain('验证失败')
    wrapper.unmount()
  })
})
