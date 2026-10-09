import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Protocols from './Protocols.vue'
import FormField from '../components/FormField.vue'
import NodeGroupMembershipEditor from '../components/NodeGroupMembershipEditor.vue'
import NodeGroupLookup from '../components/NodeGroupLookup.vue'
import WorkbenchFilterLookup from '../components/WorkbenchFilterLookup.vue'
import RealityDomainProbe from '../components/RealityDomainProbe.vue'
import { fetchProtocolEndpoint, fetchProtocolEndpointUsageResets, fetchProtocolEndpointsPage, getVersion, resetProtocolEndpointUsage, saveNetworkEntry, updateProtocolEndpointsBatch } from '../api/client'
import { trackedTaskSummaries } from '../utils/taskTracker'

const mocks = vi.hoisted(() => ({ create: vi.fn(), order: vi.fn(), saveOrder: vi.fn() }))
vi.mock('../api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('../api/client')>(),
  getVersion: vi.fn(async () => ({ protocol_capabilities: {} })),
  fetchProtocolEndpointsPage: vi.fn(async () => ({ items: [], total: 0 })),
  fetchProtocolEndpoint: vi.fn(),
  fetchProtocolEndpointUsageResets: vi.fn(async () => []),
  resetProtocolEndpointUsage: vi.fn(),
  fetchProtocolDeployments: vi.fn(async () => ({ items: [], total: 0 })),
  fetchNodesPage: vi.fn(async () => ({ items: [{ id: 1, name: 'Fixture VPS', address: '192.0.2.1', is_enabled: true, kernel_state: { installed_version: '0.0.1-rc.1' } }] })),
  fetchManagedCertificatesPage: vi.fn(async () => ({ items: [] })),
  generateRealityKeyPair: vi.fn(async () => ({ private_key: 'private-fixture', public_key: 'public-fixture', short_id: '0123456789abcdef' })),
  fetchNodeGroupsPage: vi.fn(async () => ({ items: [], total: 0 })),
  createProtocolEndpoint: mocks.create,
  fetchSubscriptionDeliveryOrder: mocks.order,
  updateSubscriptionDeliveryOrder: mocks.saveOrder,
  saveNetworkEntry: vi.fn(),
  updateProtocolEndpointsBatch: vi.fn(),
}))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))

const Button = defineComponent({ template: '<button><slot /></button>' })
const Select = defineComponent({ name: 'UiSelect', props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select />' })
const Checkbox = defineComponent({ name: 'UiCheckbox', props: ['modelValue'], emits: ['update:modelValue'], template: '<input type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', !modelValue)" />' })
const Dialog = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" :requestClose="() => {}" /></div>' })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Protocols }] })
  await router.push('/')
  await router.isReady()
  const wrapper = shallowMount(Protocols, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: {
    FormField, NodeGroupMembershipEditor, NodeGroupLookup, UiButton: Button, ModalDialog: Dialog, UiInput: true, UiSelect: Select, UiTextarea: true, UiCheckbox: Checkbox, PageRefreshButton: true, TimeBadge: true,
    PageHeader: { template: '<header><slot name="actions" /></header>' },
    DataWorkbench: { template: '<div><slot name="filters"/><slot name="actions"/><slot/></div>' },
    WorkbenchFilterBar: { template: '<div><slot/></div>' },
  } } })
  await flushPromises()
  return wrapper
}
async function click(wrapper: Awaited<ReturnType<typeof render>>, text: string) {
  await wrapper.findAll('button').find(button => button.text() === text)!.trigger('click')
  await flushPromises()
}
async function review(wrapper: Awaited<ReturnType<typeof render>>) {
  await click(wrapper, '创建协议服务')
  await click(wrapper, '实际协议监听')
  wrapper.findAllComponents({ name: 'UiInput' })[0].vm.$emit('update:modelValue', 'VLESS fixture')
  await click(wrapper, '下一步')
  await click(wrapper, '下一步')
}
const membership = { node_group_id: 9, name: 'Fixture group', code: 'fixture', description: '', revision: 7, is_enabled: true, sort_order: 0 }

describe('Protocol creation and actionable errors', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [], total: 0 } as any)
    vi.mocked(getVersion).mockResolvedValue({ protocol_capabilities: {} } as any)
    mocks.create.mockResolvedValue({ protocol_endpoint: { id: 10 }, publish_status: 'queued', timing: {} })
  })
  it('allows Mieru on reset release numbers even when an older API advertises a minimum', async () => {
    vi.mocked(getVersion).mockResolvedValue({ protocol_capabilities: { mieru: { supported: true, minimum_zero_version: '0.0.15-rc.4' } } } as any)
    const wrapper = await render()
    await click(wrapper, '创建协议服务')
  await click(wrapper, '实际协议监听')
    wrapper.findAllComponents({ name: 'UiSelect' }).find(item => item.props('modelValue') === 'vless')!.vm.$emit('update:modelValue', 'mieru')
    await flushPromises()
    await click(wrapper, '下一步')
    await click(wrapper, '下一步')
    expect(wrapper.find('page-alert-stub[title="当前内核不支持所选协议"]').exists()).toBe(false)
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ protocol: 'mieru', is_active: true, listen_address: '0.0.0.0' }))
    wrapper.unmount()
  })
  it('copies to an enabled draft and lets the user select groups before saving', async () => {
    const source = { id: 7, node_id: 1, name: 'Source', protocol: 'vless', address: '192.0.2.1', listen_address: '::', port: 443, public_port: 443, is_active: true, kernel_supported: true }
    vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [source], total: 1 } as any)
    vi.mocked(fetchProtocolEndpoint).mockResolvedValue({ ...source, config: '{"type":"vless","users":[]}', client_config: '{"type":"vless"}', node_group_memberships: [{ ...membership, node_group_id: 3 }] } as any)
    const wrapper = await render()
    await wrapper.get('button[aria-label="复制协议服务 Source"]').trigger('click')
    await flushPromises()
    await click(wrapper, '下一步')
    await click(wrapper, '下一步')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.getComponent(NodeGroupMembershipEditor).props('modelValue')).toEqual([])
    expect(wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).props('disabled')).toBe(false)
    const state = wrapper.get<HTMLInputElement>('#protocol-active')
    expect(state.element.checked).toBe(true)
    expect(state.element.closest('details')).toBeNull()
    await state.setValue(false)
    expect(wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).props('disabled')).toBe(true)
    await state.setValue(true)
    expect(wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).props('disabled')).toBe(false)
    wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Source 副本', is_active: true, listen_address: '::', node_group_membership_changes: [{ node_group_id: 9, expected_revision: 7, member: true }] }))
    wrapper.unmount()
  })
  it.each(['::', '[::]'])('saves IPv6 listen address %s independently from the public endpoint', async (listenAddress) => {
    const wrapper = await render()
    await click(wrapper, '创建协议服务')
    await click(wrapper, '实际协议监听')
    wrapper.findAllComponents({ name: 'UiInput' })[0].vm.$emit('update:modelValue', 'IPv6 fixture')
    const field = wrapper.findAllComponents(FormField).find(item => item.props('name') === 'protocol-listen-address')!
    field.getComponent({ name: 'UiInput' }).vm.$emit('update:modelValue', listenAddress)
    await flushPromises()
    await click(wrapper, '下一步')
    await click(wrapper, '下一步')
    expect(wrapper.text()).toContain('[::]:443')
    expect(wrapper.text()).not.toContain('[[::]]')
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ listen_address: listenAddress, address: '192.0.2.1', port: 443, public_port: 443 }))
    wrapper.unmount()
  })
  it('saves the reviewed protocol together with the selected node group', async () => {
    const wrapper = await render()
    await review(wrapper)
    wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'VLESS fixture', node_id: 1, node_group_membership_changes: [{ node_group_id: 9, expected_revision: 7, member: true }] }))
    wrapper.unmount()
  })
  it('saves one optional SOCKS5 egress without changing the protocol inbound', async () => {
    const wrapper = await render()
    await click(wrapper, '创建协议服务')
    await click(wrapper, '实际协议监听')
    wrapper.findAllComponents({ name: 'UiInput' })[0].vm.$emit('update:modelValue', 'VLESS with egress')
    await click(wrapper, '下一步')
    const state = wrapper.vm as any
    state.egress.enabled = true
    state.egress.protocol = 'socks5'
    state.egress.server = 'proxy.example'
    state.egress.port = 1080
    state.egress.username = 'user'
    state.egress.password = 'pass'
    await click(wrapper, '下一步')
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      protocol: 'vless',
      egress_config: JSON.stringify({ type: 'socks5', server: 'proxy.example', port: 1080, username: 'user', password: 'pass' }, null, 2),
    }))
    wrapper.unmount()
  })
  it('opens VPS domain probing from the SNI field and preserves key material on selection', async () => {
    const wrapper = await render()
    await click(wrapper, '创建协议服务')
    await click(wrapper, '实际协议监听')
    await click(wrapper, '下一步')
    const state = wrapper.vm as any
    state.structured.security = 'reality'
    await flushPromises()
    expect(state.structured.reality_server_name).toBe('')
    const keys = { privateKey: state.structured.reality_private_key, publicKey: state.structured.reality_public_key, shortID: state.structured.reality_short_id }
    await click(wrapper, '探测伪装域名')
    const probe = wrapper.getComponent(RealityDomainProbe)
    expect(probe.props('nodeId')).toBe(1)
    probe.vm.$emit('select', 'verified.example')
    await flushPromises()
    expect(state.structured.reality_server_name).toBe('verified.example')
    expect({ privateKey: state.structured.reality_private_key, publicKey: state.structured.reality_public_key, shortID: state.structured.reality_short_id }).toEqual(keys)
    probe.vm.$emit('close')
    await flushPromises()
    expect(wrapper.findComponent(RealityDomainProbe).exists()).toBe(false)
    wrapper.unmount()
  })
  it('shows the specific field failure and keeps the group selection for correction', async () => {
    mocks.create.mockRejectedValue({ response: { data: { message: '协议服务校验失败。', error: { version: 1, code: 'validation_failed', fields: { node_group_membership_changes: '节点组版本信息缺失，请重新选择。' } } } } })
    const wrapper = await render()
    await review(wrapper)
    wrapper.getComponent(NodeGroupMembershipEditor).getComponent(NodeGroupLookup).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('page-alert-stub[tone="danger"]').text()).toContain('节点组版本信息缺失，请重新选择。')
    expect(wrapper.getComponent({ name: 'NodeGroupMembershipEditor' }).props('modelValue')).toEqual([membership])
    wrapper.unmount()
  })
})

it('loads the protocol list without requesting publication-history facets', async () => {
  vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [], total: 7 } as any)
  const wrapper = await render()
  expect(fetchProtocolEndpointsPage).toHaveBeenCalledTimes(1)
  expect(vi.mocked(fetchProtocolEndpointsPage).mock.calls[0][0]).not.toHaveProperty('includeFacets')
  expect(vi.mocked(fetchProtocolEndpointsPage).mock.calls[0][0]).not.toHaveProperty('deploymentStatus')
  wrapper.unmount()
})

it('refreshes live usage without requesting publication-history facets', async () => {
  vi.useFakeTimers()
  vi.mocked(fetchProtocolEndpointsPage)
    .mockResolvedValueOnce({ items: [], total: 7 } as any)
    .mockResolvedValue({ items: [], total: 7 } as any)
  const wrapper = await render()
  try {
    vi.mocked(fetchProtocolEndpointsPage).mockClear()
    await vi.advanceTimersByTimeAsync(15_000)
    await flushPromises()
    expect(fetchProtocolEndpointsPage).toHaveBeenCalledTimes(1)
    expect(vi.mocked(fetchProtocolEndpointsPage).mock.calls[0][0]).not.toHaveProperty('includeFacets')
  } finally {
    wrapper.unmount()
    vi.useRealTimers()
  }
})

it('orders a forwarding entry independently of a direct service with the same numeric ID', async () => {
  const direct = { id: 1, key: 'protocol:1', service_kind: 'listener', name: '落地', node_id: 2, protocol: 'trojan', address: 'direct.example', port: 443, is_active: true, sort_order: 0 }
  const entry = { id: 1, key: 'entry:1', service_kind: 'forward', name: '广州专线', node_id: 6, protocol: 'trojan', address: 'forward.example', port: 443, is_active: true, sort_order: 1 }
  vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [direct, entry], total: 2 } as any)
  mocks.order.mockResolvedValue({ items: [direct, entry], version: 'v1', total: 2 })
  mocks.saveOrder.mockResolvedValue({ items: [entry, direct], version: 'v2', total: 2 })
  const wrapper = await render()
  await wrapper.get('[data-testid="protocol-delivery-order"]').trigger('click')
  await flushPromises()
  expect(wrapper.findAll('[data-order-key]')).toHaveLength(2)
  expect(wrapper.get('[data-order-key="entry:1"]').attributes('draggable')).toBe('true')
  await wrapper.get('[data-order-key="entry:1"]').trigger('dragstart', { dataTransfer: { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: 'move' } })
  await wrapper.get('[data-order-key="protocol:1"]').trigger('drop')
  expect(wrapper.findAll('[data-order-key]')[0].attributes('data-order-key')).toBe('entry:1')
  await click(wrapper, '查看完整顺序')
  expect(wrapper.findAll('.protocol-order-item')).toHaveLength(2)
  expect(wrapper.findAll('.protocol-order-item')[0].text()).toContain('广州专线')
  await click(wrapper, '保存展示顺序')
  await flushPromises()
  expect(mocks.saveOrder).toHaveBeenCalledWith({ ordered_keys: ['entry:1','protocol:1'], expected_version: 'v1' })
  wrapper.unmount()
})

it('uses the complete default table for sorting and restores the prior filtered view on cancel', async () => {
  mocks.order.mockResolvedValue({ items: [], version: 'v1', total: 0 })
  const wrapper = await render()
  const router = (wrapper.vm as any).$router
  await router.push('/?kind=forward&group=9&density=comfortable')
  await flushPromises()
  await wrapper.get('[data-testid="protocol-delivery-order"]').trigger('click')
  await flushPromises()
  expect(fetchProtocolEndpointsPage).toHaveBeenLastCalledWith(expect.objectContaining({ serviceKind: 'all', groupId: undefined, sort: 'sort_order', offset: 0 }), expect.anything())
  expect(router.currentRoute.value.query).toEqual({ density: 'comfortable' })
  await click(wrapper, '取消')
  expect(router.currentRoute.value.query).toEqual({ kind: 'forward', group: '9', density: 'comfortable' })
  wrapper.unmount()
})

it('applies and clears the node group filter on combined services', async () => {
  const wrapper = await render()
  vi.mocked(fetchProtocolEndpointsPage).mockClear()
  const groupFilter = wrapper.findAllComponents(WorkbenchFilterLookup).find(item => item.props('kind') === 'group')!
  groupFilter.vm.$emit('update:modelValue', 9)
  groupFilter.vm.$emit('apply')
  await flushPromises()
  expect(fetchProtocolEndpointsPage).toHaveBeenCalledWith(expect.objectContaining({ serviceKind: 'all', groupId: 9, offset: 0 }), expect.anything())
  const router = (wrapper.vm as any).$router
  router.back()
  await flushPromises()
  expect(fetchProtocolEndpointsPage).toHaveBeenLastCalledWith(expect.objectContaining({ groupId: undefined }), expect.anything())
  groupFilter.vm.$emit('update:modelValue', 9)
  groupFilter.vm.$emit('apply')
  await flushPromises()
  groupFilter.vm.$emit('update:modelValue', 0)
  groupFilter.vm.$emit('apply')
  await flushPromises()
  expect(fetchProtocolEndpointsPage).toHaveBeenLastCalledWith(expect.objectContaining({ serviceKind: 'all', groupId: undefined }), expect.anything())
  wrapper.unmount()
})

it('switches local service tabs while keeping the page mounted', async () => {
  const wrapper = await render()
  const content = wrapper.get('.protocol-listener-content').element
  wrapper.getComponent({ name: 'UiTabs' }).vm.$emit('update:modelValue', 'forward')
  await flushPromises()
  expect(wrapper.get('.protocol-listener-content').element).toBe(content)
  expect(fetchProtocolEndpointsPage).toHaveBeenLastCalledWith(expect.objectContaining({ serviceKind: 'forward' }), expect.anything())
  wrapper.unmount()
})

it('queues a direct protocol toggle and persists a forward toggle with its revision', async () => {
  const direct = { id: 3, service_kind: 'listener', name: '直连', node_id: 1, protocol: 'vless', address: 'direct.example', port: 443, is_active: true, kernel_supported: true }
  const forward = { id: 4, service_kind: 'forward', name: '前置', node_id: 1, protocol: 'vless', address: 'entry.example', port: 1443, is_active: true, forward: { id: 4, deployment_mode: 'managed', network: 'tcp_udp', name: '前置', node_id: 1, endpoint_id: 3, address: 'entry.example', port: 1443, public_port: 443, revision: 7 } }
  vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [direct, forward], total: 2 } as any)
  vi.mocked(updateProtocolEndpointsBatch).mockResolvedValue({ id: 42, status: 0 } as any)
  vi.mocked(saveNetworkEntry).mockResolvedValue({ ...forward.forward, revision: 8, enabled: false } as any)
  const wrapper = await render()
  const switches = wrapper.findAllComponents({ name: 'UiSwitch' })
  expect(switches).toHaveLength(2)
  switches[0].vm.$emit('update:modelValue', false)
  await flushPromises()
  expect(updateProtocolEndpointsBatch).toHaveBeenCalledWith({ protocol_endpoint_ids: [3], is_active: false })
  expect(switches[0].props('disabled')).toBe(true)
  trackedTaskSummaries[42].status = 2
  await flushPromises()
  expect(switches[0].props('disabled')).toBe(false)
  switches[1].vm.$emit('update:modelValue', false)
  await flushPromises()
  expect(saveNetworkEntry).toHaveBeenCalledWith(4, expect.objectContaining({ revision: 7, enabled: false, endpoint_id: 3, parent_protocol_id: 3, node_group_membership_changes: [] }))
  wrapper.unmount()
})

it('shows current load without treating retained publication records as protocol status', async () => {
  vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [
    { id: 3, service_kind: 'listener', name: '直连', node_id: 1, protocol: 'vless', address: 'direct.example', port: 443, is_active: true, kernel_supported: true, publication: { status: 'queued' }, usage: { active_users: 4, active_flows: 26, used_bytes_today: 2048 } },
    { id: 4, service_kind: 'forward', name: '前置', node_id: 1, protocol: 'vless', address: 'entry.example', port: 1443, is_active: true, forward: { id: 4, deployment_mode: 'managed' }, usage: { active_users: 4, active_flows: 26, used_bytes_today: 2048 } },
  ], total: 2 } as any)
  const wrapper = await render()
  expect(wrapper.findAll('.protocol-load').map(item => [
    item.findAll('strong span').map(value => value.text()),
    item.find('.protocol-load-traffic').text(),
  ])).toEqual([[['4', '26'], '2 KB'], [['4', '26'], '2 KB']])
  expect(wrapper.findAll('.protocol-load')[1].attributes('title')).toContain('父协议汇总')
  expect(wrapper.text()).not.toContain('等待节点发布')
  expect(wrapper.text()).not.toContain('等待首次发布')
  expect(wrapper.find('status-count-filters-stub').exists()).toBe(false)
  expect(wrapper.text()).toContain('查看发布任务')
  expect(fetchProtocolEndpointsPage).toHaveBeenCalledWith(expect.not.objectContaining({ includeFacets: true, deploymentStatus: 'never' }), expect.anything())
  wrapper.unmount()
})

it('allows editing Shadowsocks ports and resets only the selected service measurement period', async () => {
  const endpoint = { id: 8, service_kind: 'listener', node_id: 1, name: 'SS 入口', protocol: 'shadowsocks', address: 'edge.example.test', port: 443, public_port: 443, is_active: true, kernel_supported: true, usage: { used_bytes_today: 1024, used_bytes_total: 4096 } }
  vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [endpoint], total: 1 } as any)
  vi.mocked(fetchProtocolEndpoint).mockResolvedValue({ ...endpoint, config: '{"type":"shadowsocks","users":[]}', client_config: '{"type":"shadowsocks"}', node_group_memberships: [] } as any)
  vi.mocked(fetchProtocolEndpointUsageResets).mockResolvedValue([])
  vi.mocked(resetProtocolEndpointUsage).mockResolvedValue({ id: 1, protocol_endpoint_id: 8, actor_user_id: 1, reset_at: '2026-09-30T00:00:00Z', period_used_bytes: 4096, reason: '更换端口' })
  const wrapper = await render()
  await wrapper.get('button[aria-label="编辑协议服务 SS 入口"]').trigger('click')
  await flushPromises()
  const ports = wrapper.findAllComponents({ name: 'PortInput' })
  expect(ports).toHaveLength(2)
  expect(ports.every(port => port.attributes('disabled') === undefined)).toBe(true)
  await click(wrapper, '取消')
  await wrapper.get('button[aria-label="查看协议服务 SS 入口"]').trigger('click')
  await flushPromises()
  await click(wrapper, '重置统计')
  ;(wrapper.vm as any).usageResetReason = '更换端口'
  await flushPromises()
  await click(wrapper, '确认重置')
  expect(resetProtocolEndpointUsage).toHaveBeenCalledWith(8, '更换端口')
  wrapper.unmount()
})
