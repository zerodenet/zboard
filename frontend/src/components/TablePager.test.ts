import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TablePager from './TablePager.vue'
import UiSelect from './UiSelect.vue'

describe('TablePager', () => {
  it('uses the compact Stripe range and icon navigation by default', async () => {
    const wrapper = mount(TablePager, {
      props: { total: 126, offset: 50, limit: 50 },
      global: { plugins: [PrimeVue] },
    })

    expect(wrapper.get('nav').attributes('data-variant')).toBe('stripe')
    expect(wrapper.get('.table-pager-summary').text()).toBe('51–100 / 126')
    expect(wrapper.get('.table-pager-page').text()).toBe('2 / 3')
    expect(wrapper.findAll('.table-pager-nav')).toHaveLength(2)
    expect(wrapper.findAll('.table-pager-nav .ui-icon')).toHaveLength(2)
    expect(wrapper.getComponent(UiSelect).props('options')).toEqual([
      { label: '25', value: 25 },
      { label: '50', value: 50 },
      { label: '100', value: 100 },
    ])

    await wrapper.get('.table-pager-next').trigger('click')
    expect(wrapper.emitted('change')).toEqual([[{ offset: 100, limit: 50 }]])
  })
})
