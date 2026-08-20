import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent, h, nextTick } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { describe, expect, it } from 'vitest'
import type { SystemConfig } from '../api/client'
import { useAppStore } from '../stores/app'
import LegalPolicy from './LegalPolicy.vue'

function config(config_key: string, value: unknown): SystemConfig {
  return {
    id: 1,
    config_key,
    name: config_key,
    value,
    value_type: 'string',
    description: '',
    is_public: true,
    is_secret: false,
    configured: true,
    revision: 1,
    updated_at: '2026-08-20T00:00:00Z',
  }
}

describe('LegalPolicy', () => {
  it('projects the active policy route from public site configuration', async () => {
    const pinia = createPinia()
    const app = useAppStore(pinia)
    app.installation = { installed: true, site_name: 'Example Network', version: 'test' }
    app.publicConfigs = [
      config('site_terms_content', '# Terms\n\nWelcome to {{site_name}}.'),
      config('site_privacy_content', 'https://example.com/privacy'),
      config('site_refund_content', '# Refunds\n\nContact {{support_email}}.'),
      config('site_support_email', 'support@example.com'),
    ]

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/terms', component: LegalPolicy, meta: { title: '服务条款', policyType: 'terms' } },
        { path: '/privacy', component: LegalPolicy, meta: { title: '隐私政策', policyType: 'privacy' } },
        { path: '/refund', component: LegalPolicy, meta: { title: '退款政策', policyType: 'refund' } },
      ],
    })
    await router.push('/terms')
    await router.isReady()
    const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), {
      global: { plugins: [pinia, router] },
    })

    expect(wrapper.get('h1').text()).toBe('服务条款')
    expect(wrapper.text()).toContain('Welcome to Example Network.')

    await router.push('/privacy')
    await nextTick()
    expect(wrapper.get('h1').text()).toBe('隐私政策')
    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/privacy')

    await router.push('/refund')
    await nextTick()
    expect(wrapper.get('h1').text()).toBe('退款政策')
    expect(wrapper.text()).toContain('Contact support@example.com.')
  })
})
