import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Providers from './Providers.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), remove: vi.fn(), confirm: vi.fn(), verify: vi.fn(), update: vi.fn(), create: vi.fn(),
}))
vi.mock('../api/client', () => ({
  fetchProviderAccounts: mocks.list,
  fetchProviderDefinitions: vi.fn(async () => []),
  createProviderAccount: mocks.create, updateProviderAccount: mocks.update, verifyProviderAccount: mocks.verify,
  deleteProviderAccount: mocks.remove,
}))
vi.mock('../utils/feedback', () => ({ confirmAction: mocks.confirm, notify: vi.fn() }))

const account = { id: 7, name: 'Fixture provider', provider_key: 'cloudflare', capabilities: [], usage_count: 0, status: 'active', revision: 3 }
function render() {
  return mount(Providers, { global: { plugins: [PrimeVue], stubs: { teleport: true, RouterLink: { template: '<a><slot /></a>' } } } })
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
  it('allows deletion with references and explains automatic local cleanup', async () => {
    mocks.list.mockResolvedValue([{ ...account, usage_count: 1 }])
    const wrapper = render()
    await flushPromises()
    await openActions(wrapper)
    const button = wrapper.findAll('button').find(item => item.text() === '删除')!
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click'); await flushPromises()
    expect(mocks.remove).toHaveBeenCalledWith(7)
    wrapper.unmount()
  })
  it('confirms and removes only the selected unused integration', async () => {
    const wrapper = render()
    await flushPromises()
    mocks.list.mockResolvedValue([])
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '删除')!.trigger('click')
    await flushPromises()
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('Cloudflare 账户、Token') }))
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


describe('Provider account editing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue([account])
    mocks.update.mockResolvedValue(account)
  })
  it('allows renaming an account in use without resubmitting its token', async () => {
    mocks.list.mockResolvedValue([{ ...account, usage_count: 2 }])
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('删除时清理 DNS 管理记录')
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '编辑')!.trigger('click')
    await flushPromises()
    const inputs = wrapper.findAll('input')
    expect(inputs.find(item => item.attributes('type') === 'password')!.element.value).toBe('')
    await inputs.find(item => item.element.value === account.name)!.setValue('Updated provider')
    await wrapper.findAll('button').find(item => item.text() === '保存修改')!.trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, { name: 'Updated provider', api_token: undefined, expected_revision: 3 })
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps token validation errors inside the editor so they can be corrected', async () => {
    mocks.update.mockRejectedValue({ response: { data: { message: '新 Token 验证失败，原凭据已保留。', error: { version: 1, code: 'validation_failed', fields: { api_token: '请检查 Token 权限。' } } } } })
    const wrapper = render()
    await flushPromises()
    await openActions(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '编辑')!.trigger('click')
    await flushPromises()
    await wrapper.get('input[type="password"]').setValue('replacement-cloudflare-token')
    await wrapper.findAll('button').find(item => item.text() === '保存并验证')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('原凭据已保留')
    expect(wrapper.text()).toContain('请检查 Token 权限。')
    expect((wrapper.get('input[type="password"]').element as HTMLInputElement).value).toBe('replacement-cloudflare-token')
    wrapper.unmount()
  })
})
