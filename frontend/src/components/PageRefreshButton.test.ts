import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageRefreshButton from './PageRefreshButton.vue'

describe('PageRefreshButton', () => {
  it('keeps page refresh compact, named and keyboard accessible', async () => {
    const wrapper = mount(PageRefreshButton, {
      props: { label: '刷新节点资产' },
      global: { plugins: [PrimeVue] },
    })

    const button = wrapper.get('button')
    expect(button.attributes('aria-label')).toBe('刷新节点资产')
    expect(button.attributes('title')).toBe('刷新节点资产')
    expect(wrapper.find('.ui-icon').exists()).toBe(true)
    await button.trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })
})
