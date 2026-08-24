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
      config('site_policy_documents', JSON.stringify([
        { slug: 'terms', title: '服务条款', summary: '服务规则', content: '# 服务条款\n\nWelcome to {{site_name}}.', published: true, placements: ['footer', 'purchase'] },
        { slug: 'privacy', title: '隐私政策', summary: '', content: 'https://example.com/privacy', published: true, placements: ['footer'] },
        { slug: 'refund', title: '退款政策', summary: '', content: '# 退款政策\n\nContact {{support_email}}.', published: true, placements: ['purchase'] },
      ])),
      config('site_support_email', 'support@example.com'),
    ]

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/docs/:slug', component: LegalPolicy },
      ],
    })
    await router.push('/docs/terms')
    await router.isReady()
    const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), {
      global: { plugins: [pinia, router] },
    })

    expect(wrapper.get('h1').text()).toBe('服务条款')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.text()).toContain('Welcome to Example Network.')

    await router.push('/docs/privacy')
    await nextTick()
    expect(wrapper.get('h1').text()).toBe('隐私政策')
    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/privacy')

    await router.push('/docs/refund')
    await nextTick()
    expect(wrapper.get('h1').text()).toBe('退款政策')
    expect(wrapper.text()).toContain('Contact support@example.com.')
  })
})
