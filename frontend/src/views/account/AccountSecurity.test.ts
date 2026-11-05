import { mount, flushPromises } from '@vue/test-utils'
import { vi, it, expect, beforeEach } from 'vitest'
import AccountSecurity from './AccountSecurity.vue'
const mocks = vi.hoisted(() => ({ status: vi.fn(), setup: vi.fn(), change: vi.fn() }))
vi.mock('../../api/identities', () => ({ fetchIdentityPasswordStatus: mocks.status, setupIdentityPassword: mocks.setup }))
vi.mock('../../api/accountSecurity', () => ({ changeAccountPassword: mocks.change }))
vi.mock('../../plugins/PluginSlot.vue', () => ({ default: { template: '<div />' } }))
beforeEach(() => { vi.clearAllMocks(); mocks.status.mockResolvedValue({ password_set: true }); mocks.change.mockResolvedValue({}); mocks.setup.mockResolvedValue({}) })
const render = () => mount(AccountSecurity, { global: { plugins: [] } })
it('always provides native password change for password accounts', async () => {
 const w = render(); await flushPromises(); expect(w.text()).toContain('修改密码')
 const inputs = w.findAll('input[type=password]'); expect(inputs).toHaveLength(3)
 await inputs[0].setValue('current-password'); await inputs[1].setValue('new-password-123'); await inputs[2].setValue('different-password')
 await w.get('button').trigger('click'); expect(mocks.change).not.toHaveBeenCalled(); expect(w.text()).toContain('两次输入的新密码不一致'); expect(inputs[2].attributes('aria-invalid')).toBe('true')
 await inputs[2].setValue('new-password-123'); await w.get('button').trigger('click'); await flushPromises()
 expect(mocks.change).toHaveBeenCalledWith('current-password', 'new-password-123'); expect((inputs[0].element as HTMLInputElement).value).toBe('');w.unmount()
})
it('retains initial password setup for external-only accounts', async () => {
 mocks.status.mockResolvedValue({ password_set: false }); const w = render(); await flushPromises(); expect(w.text()).toContain('设置本地密码'); expect(w.findAll('input[type=password]')).toHaveLength(2);w.unmount()
})

it('keeps the server password error visible after clearing the secret fields', async () => {
 mocks.change.mockRejectedValueOnce({ response: { data: { message: '新密码不可用', error: { version: 1, code: 'validation_failed', fields: { password: '请选择不同的新密码。' } } } } })
 const w = render(); await flushPromises()
 const inputs = w.findAll('input[type=password]')
 await inputs[0].setValue('current-password'); await inputs[1].setValue('new-password-123'); await inputs[2].setValue('new-password-123')
 await w.get('button').trigger('click'); await flushPromises()
 expect((inputs[1].element as HTMLInputElement).value).toBe('')
 expect(w.text()).toContain('请选择不同的新密码。')
 expect(inputs[1].attributes('aria-invalid')).toBe('true')
 await inputs[1].setValue('another-password-123')
 expect(w.text()).not.toContain('请选择不同的新密码。')
 w.unmount()
})
