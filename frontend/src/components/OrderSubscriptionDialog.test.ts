import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
import OrderSubscriptionDialog from './OrderSubscriptionDialog.vue'

const api = vi.hoisted(() => ({ access: vi.fn(), write: vi.fn() }))
vi.mock('../api/subscriptionAccess', () => ({ fetchOrderSubscriptionAccess: api.access }))
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot/><slot name="footer"/></section>' })
const button = defineComponent({ template: '<button><slot/></button>' })
let wrapper: ReturnType<typeof mount> | undefined
beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('navigator', { clipboard: { writeText: api.write } })
  api.access.mockImplementation(async id => ({ configured: true, subscription_id: id + 100, subscription_url: `/api/v1/client/subscription/token-${id}` }))
})
afterEach(() => { wrapper?.unmount(); vi.unstubAllGlobals() })
async function render(orderId = 7) {
  wrapper = mount(OrderSubscriptionDialog, { props: { orderId }, global: { plugins: [PrimeVue], stubs: { ModalDialog: modal, UiButton: button, PageAlert: { template: '<div><slot/><slot name="actions"/></div>' } } } })
  await flushPromises()
  return wrapper
}

it('loads only on explicit selection and copies that order’s scoped link', async () => {
  const w = await render(0)
  expect(api.access).not.toHaveBeenCalled()
  await w.setProps({ orderId: 7 }); await flushPromises()
  expect(api.access).toHaveBeenCalledWith(7, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(api.write).not.toHaveBeenCalled()
  await w.findAll('button').find(b => b.text() === '复制订阅地址')!.trigger('click'); await flushPromises()
  expect(api.write).toHaveBeenCalledWith(`${window.location.origin}/api/v1/client/subscription/token-7`)
  expect(w.text()).toContain('已复制')
})

it('discards a late response after switching orders and clears credentials on close', async () => {
  let resolve!: (value: unknown) => void
  api.access.mockReturnValueOnce(new Promise(done => { resolve = done }))
  const w = await render(7)
  await w.setProps({ orderId: 8 }); await flushPromises()
  resolve({ configured: true, subscription_id: 107, subscription_url: '/api/v1/client/subscription/old-secret' }); await flushPromises()
  expect((w.get('input').element as HTMLInputElement).value).toContain('token-8')
  await w.setProps({ orderId: 0 }); await flushPromises()
  expect(w.find('input').exists()).toBe(false)
})

it('retains a selectable address when clipboard permission is denied', async () => {
  api.write.mockRejectedValue(new Error('denied'))
  const w = await render()
  await w.findAll('button').find(b => b.text() === '复制订阅地址')!.trigger('click'); await flushPromises()
  expect(w.text()).toContain('手动复制')
  expect(w.get('input').attributes('readonly')).toBeDefined()
})

it('shows unavailable state without a copy control', async () => {
  api.access.mockResolvedValue({ configured: false, notice: '订阅已撤销' })
  const w = await render()
  expect(w.text()).toContain('订阅已撤销')
  expect(w.find('input').exists()).toBe(false)
  expect(w.findAll('button').some(b => b.text() === '复制订阅地址')).toBe(false)
})

it('copies the selected address on HTTP when the clipboard API is unavailable', async () => {
  vi.stubGlobal('navigator', {})
  const copy = vi.fn(() => true)
  const previous = Object.getOwnPropertyDescriptor(document, 'execCommand')
  Object.defineProperty(document, 'execCommand', { configurable: true, value: copy })
  try {
    const w = await render()
    await w.findAll('button').find(b => b.text() === '复制订阅地址')!.trigger('click'); await flushPromises()
    expect(copy).toHaveBeenCalledWith('copy')
    const input = w.get('input').element as HTMLInputElement
    expect(input.selectionEnd).toBe(input.value.length)
    expect(w.text()).toContain('已复制')
  } finally {
    if (previous) Object.defineProperty(document, 'execCommand', previous)
    else Reflect.deleteProperty(document, 'execCommand')
  }
})
