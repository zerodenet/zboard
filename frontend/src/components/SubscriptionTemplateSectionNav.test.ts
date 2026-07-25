import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import SubscriptionTemplateSectionNav from './SubscriptionTemplateSectionNav.vue'

describe('SubscriptionTemplateSectionNav', () => {
  it('uses compact route links with one current section', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/admin/subscription-templates', component: { template: '<div />' } },
        { path: '/admin/subscription-templates/rule-sets', component: { template: '<div />' } },
      ],
    })
    await router.push('/admin/subscription-templates/rule-sets')
    await router.isReady()

    const wrapper = mount(SubscriptionTemplateSectionNav, {
      props: { section: 'rule-sets' },
      global: { plugins: [router] },
    })
    const links = wrapper.findAll('.section-link')

    expect(wrapper.get('nav').attributes('aria-label')).toBe('订阅模板功能')
    expect(links.map(link => link.text())).toEqual(['模板', '规则集'])
    expect(links[0].attributes('href')).toBe('/admin/subscription-templates')
    expect(links[0].attributes('aria-current')).toBeUndefined()
    expect(links[1].attributes('href')).toBe('/admin/subscription-templates/rule-sets')
    expect(links[1].attributes('aria-current')).toBe('page')
    expect(links[1].classes()).toContain('is-active')
    expect(wrapper.find('.ui-tabs').exists()).toBe(false)
  })
})
