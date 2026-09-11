import { flushPromises, shallowMount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountSecurity from './AccountSecurity.vue'

const mocks = vi.hoisted(() => ({ providers: vi.fn(), identities: vi.fn(), security: vi.fn() }))
vi.mock('../../api/identities', () => ({
  fetchIdentityProviders: mocks.providers,
  fetchExternalIdentities: mocks.identities,
  fetchIdentityPasswordStatus: mocks.security,
  bindExternalIdentity: vi.fn(),
  setupIdentityPassword: vi.fn(),
  navigateToIdentityProvider: vi.fn(),
  unlinkExternalIdentity: vi.fn(),
}))

describe('account identity details', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.providers.mockResolvedValue([{ id: 'zboard.oauth~github', name: 'GitHub' }])
    mocks.identities.mockResolvedValue([{
      id: 'binding-1', plugin_id: 'zboard.oauth~github', provider_id: 'github', publisher: 'higanbana986',
      issuer: 'https://github.com', subject: 'github-user-42', created_at: '2026-09-10T00:00:00Z',
    }])
    mocks.security.mockResolvedValue({ password_set: true })
  })

  it('shows the current user which third-party account and plugin source are linked', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/account/security', component: AccountSecurity }] })
    await router.push('/account/security')
    await router.isReady()
    const wrapper = shallowMount(AccountSecurity, { global: {
      plugins: [router], renderStubDefaultSlot: true,
      stubs: {
        FormField: true, TransientFeedback: true,
        UiButton: { template: '<button><slot /></button>' },
        UiInput: true,
        TimeBadge: { props: ['value'], template: '<time>{{ value }}</time>' },
      },
    } })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('GitHub')
    expect(text).toContain('github-user-42')
    expect(text).toContain('higanbana986')
    expect(text).toContain('zboard.oauth')
    expect(text).toContain('https://github.com')
    expect(text).toContain('2026-09-10T00:00:00Z')
    const buttonLabels = wrapper.findAll('button').map(button => button.text())
    expect(buttonLabels).toContain('已绑定')
    expect(buttonLabels).not.toContain('绑定账号')
    wrapper.unmount()
  })
})
