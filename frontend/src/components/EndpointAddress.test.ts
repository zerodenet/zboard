import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
import EndpointAddress from './EndpointAddress.vue'

afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

describe('EndpointAddress', () => {
  it.each([
    ['long-hostname.ap-northeast.example.com', 'long-hostname.ap-northeast.example.com:21388'],
    ['2001:db8::1', '[2001:db8::1]:21388'],
    ['[2001:db8::1]', '[2001:db8::1]:21388'],
  ])('reveals and copies the complete address for %s', async (address, expected) => {
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    const wrapper = mount(EndpointAddress, { attachTo: document.body, props: { address, port: 21388 }, global: { plugins: [PrimeVue] } })
    expect(wrapper.get('button').attributes('title')).toBe(expected)
    expect(wrapper.get('.endpoint-address-port').text()).toBe(':21388')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.querySelector('.endpoint-address-detail code')?.textContent).toBe(expected)
    ;(document.querySelector('.endpoint-address-detail button') as HTMLButtonElement).click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(expected)
    expect(document.querySelector('[role="status"]')?.textContent).toBe('地址已复制')
    wrapper.unmount()
  })

  it('keeps an address without a port intact and offers manual copy when clipboard access fails', async () => {
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('Denied'))
    const wrapper = mount(EndpointAddress, { attachTo: document.body, props: { address: '2001:db8::1' }, global: { plugins: [PrimeVue] } })
    expect(wrapper.get('button').attributes('title')).toBe('2001:db8::1')
    expect(wrapper.find('.endpoint-address-port').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    ;(document.querySelector('.endpoint-address-detail button') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.querySelector('[role="status"]')?.textContent).toContain('手动复制')
    expect(document.querySelector('.endpoint-address-detail code')?.textContent).toBe('2001:db8::1')
    wrapper.unmount()
  })
})
