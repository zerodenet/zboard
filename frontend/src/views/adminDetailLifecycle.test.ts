import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Orders from './Orders.vue'
import Nodes from './Nodes.vue'
import NodeCounterObservation from '../components/NodeCounterObservation.vue'
import OrderSubscriptionDialog from '../components/OrderSubscriptionDialog.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
vi.mock('../api/subscriptionAccess', () => ({ fetchOrderSubscriptionAccess: vi.fn() }))
import Users from './Users.vue'
import Subscriptions from './Subscriptions.vue'

const mocks = vi.hoisted(() => ({ detail: vi.fn(), events: vi.fn(), list: vi.fn(), pay: vi.fn(), kernel: vi.fn(), bbr: vi.fn(), releases: vi.fn(), load: vi.fn(), task: vi.fn(), protocols: vi.fn(), rotate: vi.fn() }))
vi.mock('../api/client', () => ({
  fetchAdminOrderDetail: mocks.detail, fetchAdminUserDetail: mocks.detail,
  fetchAdminSubscriptionDetail: mocks.detail, fetchAdminOrderPaymentEvents: mocks.events,
  rotateNodeConnectorCredential: mocks.rotate, fetchProtocolEndpointsPage: mocks.protocols, fetchAdminTask: mocks.task, fetchNode: mocks.detail, fetchNodesPage: mocks.list, fetchNodeKernel: mocks.kernel, fetchNodeLoad: mocks.load, fetchZeroReleases: mocks.releases,
  fetchOrdersPage: mocks.list, fetchUsersPage: mocks.list, fetchSubscriptionsPage: mocks.list,
  cancelOrder: vi.fn(), markOrderPaid: mocks.pay, createAdminUser: vi.fn(), updateAdminUser: vi.fn(),
}))
vi.mock('../api/nodeSystemActions', () => ({ fetchNodeSystemActions: mocks.bbr, enableNodeBBR: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 99 } }) }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function detail(id: number) {
  return { id, name: `node-${id}`, lifecycle_status: 'active', email: `user-${id}@example.com`, plan_name: `订阅 #${id}`, status: 'active', trade_no: `trade-${id}`, user_id: id }
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
  mocks.releases.mockResolvedValue([])
  mocks.protocols.mockResolvedValue({ items: [], total: 0 })
  mocks.kernel.mockResolvedValue({ state: { status: 'healthy' }, operations: [] })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })

const cases = [
  { name: 'orders', component: Orders, queryKey: 'order', title: (id: number) => `订单 #${id}` },
  { name: 'users', component: Users, queryKey: 'user', title: (id: number) => `user-${id}@example.com` },
  { name: 'subscriptions', component: Subscriptions, queryKey: 'subscription', title: (id: number) => `订阅 #${id}` },
  { name: 'nodes', component: Nodes, queryKey: 'node', title: (id: number) => `node-${id}` },
]
async function render(testCase: typeof cases[number]) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: testCase.component }] })
  await router.push({ path: '/', query: { [testCase.queryKey]: '1' } })
  await router.isReady()
  wrapper = shallowMount(defineComponent({ components: { RouterView }, template: '<RouterView />' }), { global: {
    plugins: [router], renderStubDefaultSlot: true,
    stubs: {
      RouterView: false, Orders: false, Users: false, Subscriptions: false, Nodes: false, RemoteReadStatus: false,
      DetailDrawer: drawer, ModalDialog: hiddenModal, ConfirmDialog: hiddenModal,
      NodeRetirementDialog: hiddenModal, NodeRuntimeDiagnosticsModal: hiddenModal, UiButton: true, TimeBadge: true, PageRefreshButton: true,
      FormField: true, UiCheckbox: true, UiInput: true, UiTextarea: true, UiSelect: true,
    },
  } })
  await flushPromises()
  return { router, wrapper }
}

it('passes only the selected node counter evidence to its detail view', async () => {
  const observation = { known: true, latest_source_known: true, stats_source_known: true, source: 'buffered_stats', counter_received_at: '2026-10-01T10:20:30Z' }
  mocks.detail.mockResolvedValueOnce({ ...detail(1), observation })
  const { router, wrapper } = await render(cases.find(testCase => testCase.name === 'nodes')!)
  expect(wrapper.getComponent(NodeCounterObservation).props('observation')).toEqual(observation)
  await router.push({ query: { node: '2' } })
  await flushPromises()
  expect(wrapper.get('[data-detail] h2').text()).toBe('node-2')
  expect(wrapper.getComponent(NodeCounterObservation).props('observation')).toBeUndefined()
  expect(mocks.detail.mock.calls.map(call => call[0])).toEqual([1, 2])
})

describe.each(cases)('$name detail lifecycle', testCase => {
  it('opens linked details while the filtered list is still pending', async () => {
    mocks.list.mockReturnValue(new Promise(() => {}))
    const { router, wrapper } = await render(testCase)
    expect(wrapper.get('[data-detail] h2').text()).toBe(testCase.title(1))
    await router.push({ query: { [testCase.queryKey]: '2', q: 'another filter' } })
    await flushPromises()
    expect(wrapper.get('[data-detail] h2').text()).toBe(testCase.title(2))
  })

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
    expect(wrapper.find('.business-detail, .node-detail').exists()).toBe(false)
    current.resolve(detail(1))
    await flushPromises()
    expect(wrapper.find('.business-detail, .node-detail').exists()).toBe(true)
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
  expect(wrapper.find('.business-detail, .node-detail').exists()).toBe(true)
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

it('aborts node detail reads on close and does not reopen from a late reply', async () => {
 const pending = deferred<ReturnType<typeof detail>>()
 mocks.detail.mockReturnValueOnce(pending.promise)
 const { wrapper } = await render(cases[3])
 const signal = mocks.detail.mock.calls[0][1].signal as AbortSignal
 await wrapper.get('[data-detail] button').trigger('click'); await flushPromises()
 expect(signal.aborted).toBe(true)
 pending.resolve(detail(1)); await flushPromises()
 expect(wrapper.find('[data-detail]').exists()).toBe(false)
 expect(mocks.load).not.toHaveBeenCalled()
})
it('keeps node assets usable when kernel state fails and never samples the host automatically', async () => {
 mocks.kernel.mockRejectedValue({ response: { status: 503, data: { message: 'kernel read unavailable' } } })
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'kernel' } }); await flushPromises()
 expect(wrapper.get('[data-detail] h2').text()).toBe('node-1')
 expect(wrapper.text()).toContain('kernel read unavailable')
 expect(mocks.kernel).toHaveBeenCalledWith(1, { signal: expect.any(AbortSignal) })
 expect(mocks.load).not.toHaveBeenCalled()
})

it('cancels the old BBR read when leaving the kernel tab and ignores its late failure', async () => {
 mocks.detail.mockImplementation(async id => ({ ...detail(id), ssh_verified_at: '2026-10-16T00:00:00Z' }))
 const old = deferred<never>()
 mocks.bbr.mockReturnValueOnce(old.promise)
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'kernel' } }); await flushPromises()
 const signal = mocks.bbr.mock.calls[0][1].signal as AbortSignal
 await router.push({ query: { node: '1', tab: 'overview' } }); await flushPromises()
 expect(signal.aborted).toBe(true)
 old.reject({ response: { data: { message: 'old BBR failure' } } }); await flushPromises()
 expect(wrapper.text()).not.toContain('old BBR failure')
})
it('keeps only one progress read in flight and cancels it on drawer close', async () => {
 vi.useFakeTimers()
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'kernel' } }); await flushPromises()
 const source = wrapper.findAllComponents({ name: 'UiTabs' }).find(tabs => tabs.attributes('label') === '内核来源')!
 source.vm.$emit('update:modelValue', 'offline'); await flushPromises()
 wrapper.findComponent({ name: 'NodeKernelUpload' }).vm.$emit('accepted', { id: 42, status: 0 }, 1, 'test-version')
 mocks.task.mockReturnValue(new Promise(() => {}))
 await vi.advanceTimersByTimeAsync(6_000)
 expect(mocks.task).toHaveBeenCalledTimes(1)
 const signal = mocks.task.mock.calls[0][2].signal as AbortSignal
 await wrapper.get('[data-detail] button').trigger('click'); await flushPromises()
 expect(signal.aborted).toBe(true)
 await vi.advanceTimersByTimeAsync(15_000)
 expect(mocks.task).toHaveBeenCalledTimes(1)
})

it('keeps node protocol pages scoped and aborts a previous node read', async () => {
 const old = deferred<{ items: { id: number; name: string }[]; total: number }>()
 mocks.protocols.mockReturnValueOnce(old.promise)
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'protocols' } }); await flushPromises()
 const signal = mocks.protocols.mock.calls[0][1].signal as AbortSignal
 await router.push({ query: { node: '2', tab: 'protocols' } }); await flushPromises()
 expect(signal.aborted).toBe(true)
 old.resolve({ items: [{ id: 99, name: 'old node protocol' }], total: 1 }); await flushPromises()
 expect(wrapper.text()).not.toContain('old node protocol')
 expect(wrapper.get('[data-detail] h2').text()).toBe('node-2')
 expect(mocks.protocols).toHaveBeenLastCalledWith({ nodeId: 2, offset: 0, limit: 25 }, { signal: expect.any(AbortSignal) })
})
it('shows protocol read failures without claiming the node has no protocols', async () => {
 mocks.protocols.mockRejectedValue(new Error('offline'))
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'protocols' } }); await flushPromises()
 expect(wrapper.text()).toContain('节点协议服务加载失败')
 expect(wrapper.find('.node-protocols empty-state-stub').exists()).toBe(false)
})
it('keeps the successful credential patch visible when refreshing the node list stalls', async () => {
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'credentials' } }); await flushPromises()
 mocks.list.mockReturnValue(new Promise(() => {}))
 mocks.rotate.mockResolvedValue({ node_id: 1, api_key: 'one-time-test', api_key_prefix: 'new-prefix' })
 const create = wrapper.findAllComponents({ name: 'UiButton' }).find(button => button.text() === '手动生成')!
 await create.trigger('click'); await flushPromises()
 expect(wrapper.text()).toContain('new-prefix')
 expect(mocks.rotate).toHaveBeenCalledWith(1)
 await router.push({ query: { node: '2', tab: 'credentials' } }); await flushPromises()
 expect(wrapper.get('[data-detail] h2').text()).toBe('node-2')
 expect(wrapper.text()).not.toContain('new-prefix')
})

it('shares an in-flight version read between repeated kernel source selections', async () => {
 const versions = deferred<never[]>()
 mocks.releases.mockReturnValueOnce(versions.promise)
 const { router, wrapper } = await render(cases[3])
 await router.push({ query: { node: '1', tab: 'kernel' } }); await flushPromises()
 const source = wrapper.findAllComponents({ name: 'UiTabs' }).find(tabs => tabs.attributes('label') === '内核来源')!
 source.vm.$emit('update:modelValue', 'offline'); await flushPromises()
 source.vm.$emit('update:modelValue', 'online'); await flushPromises()
 expect(mocks.releases).toHaveBeenCalledTimes(1)
 versions.resolve([]); await flushPromises()
 expect(wrapper.text()).not.toContain('Zero 已发布版本查询失败')
})
