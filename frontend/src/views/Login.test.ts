import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import Login from './Login.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import { login, fetchSystemStatus } from '../api/client'
import { useAppStore } from '../stores/app'
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), login: vi.fn(), fetchSystemStatus: vi.fn() }))
it('explains expiry and returns to the original page after signing in', async () => {
  localStorage.clear()
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/login', component: Login },
    { path: '/admin/nodes', component: {} },
    { path: '/', component: {} },
  ] })
  await router.push({ path: '/login', query: { redirect: '/admin/nodes?page=3#details', reason: 'session-expired' } })
  vi.mocked(login).mockResolvedValueOnce({ auth: { token: 'new-login' }, user: { id: 1, email: 'admin@example.test', is_admin: true } })
  vi.mocked(fetchSystemStatus).mockRejectedValueOnce(new Error('offline'))
  const wrapper = mount(Login, { global: { plugins: [pinia, router], components: { UiButton, UiInput }, stubs: { PluginSlot: true, AuthLegalLinks: true } } })
  try {
    expect(wrapper.text()).toContain('登录已过期')
    await wrapper.get('input[type="email"]').setValue('admin@example.test')
    await wrapper.get('input[type="password"]').setValue('valid-password-123')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/admin/nodes?page=3#details')
    expect(useAppStore().isAuthenticated).toBe(true)
  } finally { wrapper.unmount(); localStorage.clear() }
})
