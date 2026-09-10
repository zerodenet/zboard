import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Protocols from './Protocols.vue'
import FormField from '../components/FormField.vue'
import NodeGroupMembershipEditor from '../components/NodeGroupMembershipEditor.vue'
import NodeGroupLookup from '../components/NodeGroupLookup.vue'
import { fetchProtocolEndpoint, fetchProtocolEndpointsPage, getVersion } from '../api/client'

const mocks = vi.hoisted(() => ({ create: vi.fn(), order: vi.fn(), saveOrder: vi.fn() }))
vi.mock('../api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('../api/client')>(),
  getVersion: vi.fn(async () => ({ protocol_capabilities: {} })),
  fetchProtocolEndpointsPage: vi.fn(async () => ({ items: [], total: 0 })),
  fetchProtocolEndpoint: vi.fn(),
  fetchNodesPage: vi.fn(async () => ({ items: [{ id: 1, name: 'Fixture VPS', address: '192.0.2.1', is_enabled: true, kernel_state: { installed_version: '0.0.1-rc.1' } }] })),
  fetchManagedCertificatesPage: vi.fn(async () => ({ items: [] })),
  fetchNodeGroupsPage: vi.fn(async () => ({ items: [], total: 0 })),
  createProtocolEndpoint: mocks.create,
  fetchSubscriptionDeliveryOrder: mocks.order,
  updateSubscriptionDeliveryOrder: mocks.saveOrder,
}))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn() }))
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
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ protocol: 'mieru', is_active: true }))
    wrapper.unmount()
  })
  it('copies to an enabled draft and lets the user select groups before saving', async () => {
    const source = { id: 7, node_id: 1, name: 'Source', protocol: 'vless', address: '192.0.2.1', port: 443, public_port: 443, is_active: true, kernel_supported: true }
    vi.mocked(fetchProtocolEndpointsPage).mockResolvedValue({ items: [source], total: 1 } as any)
    vi.mocked(fetchProtocolEndpoint).mockResolvedValue({ ...source, config: '{"type":"vless","users":[]}', client_config: '{"type":"vless"}', node_group_memberships: [{ ...membership, node_group_id: 3 }] } as any)
    const wrapper = await render()
    await wrapper.get('button[aria-label="复制协议服务 Source"]').trigger('click')
    await flushPromises()
    await click(wrapper, '下一步')
    await click(wrapper, '下一步')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.getComponent(NodeGroupMembershipEditor).props('modelValue')).toEqual([])
    expect(wrapper.getComponent(NodeGroupLookup).props('disabled')).toBe(false)
    const state = wrapper.get<HTMLInputElement>('#protocol-active')
    expect(state.element.checked).toBe(true)
    expect(state.element.closest('details')).toBeNull()
    await state.setValue(false)
    expect(wrapper.getComponent(NodeGroupLookup).props('disabled')).toBe(true)
    await state.setValue(true)
    expect(wrapper.getComponent(NodeGroupLookup).props('disabled')).toBe(false)
    wrapper.getComponent(NodeGroupLookup).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Source 副本', is_active: true, node_group_membership_changes: [{ node_group_id: 9, expected_revision: 7, member: true }] }))
    wrapper.unmount()
  })
  it('saves the reviewed protocol together with the selected node group', async () => {
    const wrapper = await render()
    await review(wrapper)
    wrapper.getComponent({ name: 'NodeGroupLookup' }).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'VLESS fixture', node_id: 1, node_group_membership_changes: [{ node_group_id: 9, expected_revision: 7, member: true }] }))
    wrapper.unmount()
  })
  it('shows the specific field failure and keeps the group selection for correction', async () => {
    mocks.create.mockRejectedValue({ response: { data: { message: '协议服务校验失败。', error: { version: 1, code: 'validation_failed', fields: { node_group_membership_changes: '节点组版本信息缺失，请重新选择。' } } } } })
    const wrapper = await render()
    await review(wrapper)
    wrapper.getComponent({ name: 'NodeGroupLookup' }).getComponent({ name: 'UiAutocomplete' }).vm.$emit('item-select', { value: { ...membership, id: membership.node_group_id } })
    await flushPromises()
    await wrapper.get('#protocol-form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('page-alert-stub[tone="danger"]').text()).toContain('节点组版本信息缺失，请重新选择。')
    expect(wrapper.getComponent({ name: 'NodeGroupMembershipEditor' }).props('modelValue')).toEqual([membership])
    wrapper.unmount()
  })
})

it('orders a forwarding entry independently of a direct service with the same numeric ID', async () => {
  const direct = { id: 1, key: 'protocol:1', service_kind: 'listener', name: '落地', node_id: 2, protocol: 'trojan', is_active: true, sort_order: 0 }
  const entry = { id: 1, key: 'entry:1', service_kind: 'forward', name: '广州专线', node_id: 6, protocol: 'trojan', is_active: true, sort_order: 1 }
  mocks.order.mockResolvedValue({ items: [direct, entry], version: 'v1', total: 2 })
  mocks.saveOrder.mockResolvedValue({ items: [entry, direct], version: 'v2', total: 2 })
  const wrapper = await render()
  await wrapper.get('[data-testid="protocol-delivery-order"]').trigger('click')
  await flushPromises()
  expect(wrapper.findAll('.protocol-order-item')).toHaveLength(2)
  await wrapper.get('[aria-label="上移 广州专线"]').trigger('click')
  expect(wrapper.findAll('.protocol-order-item')[0].text()).toContain('广州专线')
  await click(wrapper, '保存展示顺序')
  await flushPromises()
  expect(mocks.saveOrder).toHaveBeenCalledWith({ ordered_keys: ['entry:1','protocol:1'], expected_version: 'v1' })
  wrapper.unmount()
})
