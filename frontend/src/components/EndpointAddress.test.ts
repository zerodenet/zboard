import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import EndpointAddress from './EndpointAddress.vue'

let clipped = true
beforeEach(() => {
  clipped = true
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function (this: HTMLElement) { return (this as HTMLElement).classList.contains('endpoint-address-host') && clipped ? 300 : 100 })
})
afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

describe('EndpointAddress', () => {
  it.each([
    ['long-hostname.ap-northeast.example.com', 'long-hostname.ap-northeast.example.com:21388'],
    ['2001:db8::1', '[2001:db8::1]:21388'],
    ['[2001:db8::1]', '[2001:db8::1]:21388'],
  ])('reveals and copies the complete address for %s', async (address, expected) => {
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    const wrapper = mount(EndpointAddress, { attachTo: document.body, props: { address, port: 21388 }, global: { plugins: [] } })
    await flushPromises()
    expect(wrapper.get('.endpoint-address-trigger').attributes('title')).toBeUndefined()
    expect(wrapper.get('.endpoint-address-port').text()).toBe(':21388')
    await wrapper.get('.endpoint-address-trigger').trigger('click')
    await flushPromises()
    expect(document.querySelector('.endpoint-address-detail code')?.textContent).toBe(expected)
    const scope = wrapper.get('.endpoint-address').element.getAttributeNames().find(name => name.startsWith('data-v-'))!
    expect(scope).toBeTruthy()
    expect(document.querySelector('.endpoint-address-detail')?.hasAttribute(scope)).toBe(true)
    ;(wrapper.get('[aria-label^="复制地址"]').element as HTMLButtonElement).click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(expected)
    expect(document.querySelector('[role="status"]')?.textContent).toBe('地址已复制')
    wrapper.unmount()
  })

  it('keeps an address without a port intact and offers manual copy when clipboard access fails', async () => {
    clipped = false
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('Denied'))
    const wrapper = mount(EndpointAddress, { attachTo: document.body, props: { address: '2001:db8::1' }, global: { plugins: [] } })
    await flushPromises()
    expect(wrapper.find('.endpoint-address-trigger').exists()).toBe(false)
    expect(wrapper.find('.endpoint-address-port').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="status"]')?.textContent).toContain('手动复制')
    expect(document.querySelector('.endpoint-address-detail')).toBeNull()
    expect(wrapper.get('.endpoint-address-host').text()).toBe('2001:db8::1')
    wrapper.unmount()
  })
})
