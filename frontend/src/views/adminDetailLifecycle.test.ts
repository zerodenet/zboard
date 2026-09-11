import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Orders from './Orders.vue'
import OrderSubscriptionDialog from '../components/OrderSubscriptionDialog.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
vi.mock('../api/subscriptionAccess', () => ({ fetchOrderSubscriptionAccess: vi.fn() }))
import Users from './Users.vue'
import Subscriptions from './Subscriptions.vue'

const mocks = vi.hoisted(() => ({ detail: vi.fn(), events: vi.fn(), list: vi.fn(), pay: vi.fn() }))
vi.mock('../api/client', () => ({
  fetchAdminOrderDetail: mocks.detail, fetchAdminUserDetail: mocks.detail,
  fetchAdminSubscriptionDetail: mocks.detail, fetchAdminOrderPaymentEvents: mocks.events,
  fetchOrdersPage: mocks.list, fetchUsersPage: mocks.list, fetchSubscriptionsPage: mocks.list,
  cancelOrder: vi.fn(), markOrderPaid: mocks.pay, createAdminUser: vi.fn(), updateAdminUser: vi.fn(),
}))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 99 } }) }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function detail(id: number) {
  return { id, email: `user-${id}@example.com`, plan_name: `订阅 #${id}`, status: 'active', trade_no: `trade-${id}`, user_id: id }
}
const drawer = defineComponent({
  props: ['open', 'title'], emits: ['close'],
  template: '<aside v-if="open" data-detail><h2>{{ title }}</h2><button @click="$emit(\'close\')">Close</button><slot /></aside>',
})
const hiddenModal = defineComponent({ template: '<div />' })
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  mocks.list.mockResolvedValue({ items: [], total: 0 })
  mocks.events.mockResolvedValue({ items: [], total: 0 })
  mocks.detail.mockImplementation(async id => detail(id))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

const cases = [
  { name: 'orders', component: Orders, queryKey: 'order', title: (id: number) => `订单 #${id}` },
  { name: 'users', component: Users, queryKey: 'user', title: (id: number) => `user-${id}@example.com` },
  { name: 'subscriptions', component: Subscriptions, queryKey: 'subscription', title: (id: number) => `订阅 #${id}` },
]
async function render(testCase: typeof cases[number]) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: testCase.component }] })
  await router.push({ path: '/', query: { [testCase.queryKey]: '1' } })
  await router.isReady()
  wrapper = shallowMount(defineComponent({ components: { RouterView }, template: '<RouterView />' }), { global: {
    plugins: [router], renderStubDefaultSlot: true,
    stubs: {
      RouterView: false, Orders: false, Users: false, Subscriptions: false,
      DetailDrawer: drawer, ModalDialog: hiddenModal, ConfirmDialog: hiddenModal,
      UiButton: true, TimeBadge: true, PageRefreshButton: true,
      FormField: true, UiCheckbox: true, UiInput: true, UiSelect: true,
    },
  } })
  await flushPromises()
  return { router, wrapper }
}

describe.each(cases)('$name detail lifecycle', testCase => {
  it('keeps the selected entity when an older transport response arrives late', async () => {
    const old = deferred<ReturnType<typeof detail>>()
    mocks.detail.mockReturnValueOnce(old.promise)
    const { router, wrapper } = await render(testCase)
    await router.push({ query: { [testCase.queryKey]: '2' } })
    await flushPromises()
    expect(wrapper.get('[data-detail] h2').text()).toBe(testCase.title(2))
    old.resolve(detail(1))
    await flushPromises()
    expect(wrapper.get('[data-detail] h2').text()).toBe(testCase.title(2))
  })

  it('does not show an old entity failure on the currently selected entity', async () => {
    const old = deferred<ReturnType<typeof detail>>()
    mocks.detail.mockReturnValueOnce(old.promise)
    const { router, wrapper } = await render(testCase)
    await router.push({ query: { [testCase.queryKey]: '2' } })
    await flushPromises()
    old.reject({ response: { data: { message: 'old entity failed' } } })
    await flushPromises()
    expect(wrapper.get('[data-detail]').text()).not.toContain('old entity failed')
  })

  it('keeps a reopened entity loading until its own request completes', async () => {
    const old = deferred<ReturnType<typeof detail>>()
    const current = deferred<ReturnType<typeof detail>>()
    mocks.detail.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const { router, wrapper } = await render(testCase)
    await wrapper.get('[data-detail] button').trigger('click')
    await flushPromises()
    await router.push({ query: { [testCase.queryKey]: '1' } })
    await flushPromises()
    old.resolve(detail(1))
    await flushPromises()
    expect(wrapper.find('[data-detail] [role="status"]').exists()).toBe(true)
    expect(wrapper.find('.business-detail').exists()).toBe(false)
    current.resolve(detail(1))
    await flushPromises()
    expect(wrapper.find('.business-detail').exists()).toBe(true)
  })

  it('aborts a detail request when the page unmounts', async () => {
    const pending = deferred<ReturnType<typeof detail>>()
    mocks.detail.mockReturnValueOnce(pending.promise)
    await render(testCase)
    const signal = mocks.detail.mock.calls[0][1].signal as AbortSignal
    wrapper!.unmount(); wrapper = undefined
    expect(signal.aborted).toBe(true)
    pending.resolve(detail(1))
    await flushPromises()
  })
})

it('shows order details while the independent payment event list is still loading', async () => {
  const events = deferred<{ items: never[]; total: number }>()
  mocks.events.mockReturnValueOnce(events.promise)
  const { wrapper } = await render(cases[0])
  expect(wrapper.find('.business-detail').exists()).toBe(true)
  expect(wrapper.get('.payment-events').text()).toContain('正在加载支付事件')
  events.resolve({ items: [], total: 0 })
  await flushPromises()
})

it('aborts order event reads when the drawer closes', async () => {
  const events = deferred<{ items: never[]; total: number }>()
  mocks.events.mockReturnValueOnce(events.promise)
  const { wrapper } = await render(cases[0])
  const signal = mocks.events.mock.calls[0][2].signal as AbortSignal
  await wrapper.get('[data-detail] button').trigger('click')
  await flushPromises()
  expect(signal.aborted).toBe(true)
  events.resolve({ items: [], total: 0 })
  await flushPromises()
})

it('applies event pagination from the URL while order details are still pending', async () => {
  const pending = deferred<ReturnType<typeof detail>>()
  mocks.detail.mockReturnValueOnce(pending.promise)
  mocks.events.mockResolvedValue({ items: [], total: 50 })
  const { router } = await render(cases[0])
  await router.push({ query: { order: '1', event_page: '2' } })
  await flushPromises()
  expect(mocks.events).toHaveBeenLastCalledWith(1, { offset: 25, limit: 25 }, expect.anything())
  expect(mocks.detail).toHaveBeenCalledOnce()
  pending.resolve(detail(1))
  await flushPromises()
})

it('opens subscription delivery for the order whose payment was confirmed', async () => {
  mocks.list.mockResolvedValue({ items: [{ ...detail(27), status: 'pending', subscription_id: 0 }], total: 1 })
  mocks.pay.mockResolvedValue({ id: 27, status: 'paid', subscription_id: 301 })
  const { wrapper } = await render(cases[0])
  const pay = wrapper.findAllComponents({ name: 'UiButton' }).find(b => b.text() === '确认收款')
  expect(pay).toBeDefined()
  await pay!.trigger('click')
  wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
  await flushPromises()
  expect(mocks.pay).toHaveBeenCalledWith(27)
  expect(wrapper.findComponent(OrderSubscriptionDialog).props('orderId')).toBe(27)
})
