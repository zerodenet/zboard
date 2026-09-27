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
  return mount(Providers, { attachTo: document.body, global: { plugins: [], stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
}
async function openActions(wrapper: ReturnType<typeof render>) {
  await wrapper.get('[data-row-action-trigger="provider-7"]').trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
}
function menuButton(label: string): HTMLButtonElement {
  const button = [...document.body.querySelectorAll<HTMLButtonElement>('[role="menu"] button')].find(item => item.textContent?.trim() === label)
  expect(button, `menu action ${label}`).toBeDefined()
  return button!
}
function dialogButton(label: string): HTMLButtonElement {
  const button = [...document.body.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(item => item.textContent?.trim() === label)
  expect(button, `dialog action ${label}`).toBeDefined()
  return button!
}
async function fillDialogInput(selector: string, value: string) {
  const input = document.body.querySelector<HTMLInputElement>(selector)
  expect(input, `dialog input ${selector}`).not.toBeNull()
  input!.value = value
  input!.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
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
    const button = menuButton('删除')
    expect(button.disabled).toBe(false)
    button.click(); await flushPromises()
    expect(mocks.remove).toHaveBeenCalledWith(7)
    wrapper.unmount()
  })
  it('confirms and removes only the selected unused integration', async () => {
    const wrapper = render()
    await flushPromises()
    mocks.list.mockResolvedValue([])
    await openActions(wrapper)
    menuButton('删除').click()
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
    menuButton('删除').click()
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
    menuButton('重新验证').click()
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
    expect(wrapper.find('[title="关联的 DNS 解析与证书数量"]').text()).toBe('2')
    await openActions(wrapper)
    menuButton('编辑').click()
    await flushPromises()
    expect(document.body.querySelector<HTMLInputElement>('#provider-token')?.value).toBe('')
    await fillDialogInput('#provider-name', 'Updated provider')
    dialogButton('保存修改').click()
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
    menuButton('编辑').click()
    await flushPromises()
    await fillDialogInput('#provider-token', 'replacement-cloudflare-token')
    dialogButton('保存并验证').click()
    await flushPromises()
    expect(document.body.textContent).toContain('原凭据已保留')
    expect(document.body.textContent).toContain('请检查 Token 权限。')
    expect(document.body.querySelector<HTMLInputElement>('#provider-token')?.value).toBe('replacement-cloudflare-token')
    wrapper.unmount()
  })
})
