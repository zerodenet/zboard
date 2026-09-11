import { mount, flushPromises } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { vi, it, expect, beforeEach } from 'vitest'
import AccountSecurity from './AccountSecurity.vue'
const mocks = vi.hoisted(() => ({ status: vi.fn(), setup: vi.fn(), change: vi.fn() }))
vi.mock('../../api/identities', () => ({ fetchIdentityPasswordStatus: mocks.status, setupIdentityPassword: mocks.setup }))
vi.mock('../../api/accountSecurity', () => ({ changeAccountPassword: mocks.change }))
vi.mock('../../plugins/PluginSlot.vue', () => ({ default: { template: '<div />' } }))
beforeEach(() => { vi.clearAllMocks(); mocks.status.mockResolvedValue({ password_set: true }); mocks.change.mockResolvedValue({}); mocks.setup.mockResolvedValue({}) })
const render = () => mount(AccountSecurity, { global: { plugins: [PrimeVue] } })
it('always provides native password change for password accounts', async () => {
 const w = render(); await flushPromises(); expect(w.text()).toContain('修改密码')
 const inputs = w.findAll('input[type=password]'); expect(inputs).toHaveLength(3)
 await inputs[0].setValue('current-password'); await inputs[1].setValue('new-password-123'); await inputs[2].setValue('different-password')
 await w.get('button').trigger('click'); expect(mocks.change).not.toHaveBeenCalled(); expect(w.text()).toContain('两次输入的新密码不一致')
 await inputs[2].setValue('new-password-123'); await w.get('button').trigger('click'); await flushPromises()
 expect(mocks.change).toHaveBeenCalledWith('current-password', 'new-password-123'); expect((inputs[0].element as HTMLInputElement).value).toBe('');w.unmount()
})
it('retains initial password setup for external-only accounts', async () => {
 mocks.status.mockResolvedValue({ password_set: false }); const w = render(); await flushPromises(); expect(w.text()).toContain('设置本地密码'); expect(w.findAll('input[type=password]')).toHaveLength(2);w.unmount()
})
