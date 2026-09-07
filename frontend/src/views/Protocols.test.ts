import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Protocols from './Protocols.vue'
import FormField from '../components/FormField.vue'
import NodeGroupMembershipEditor from '../components/NodeGroupMembershipEditor.vue'
import NodeGroupLookup from '../components/NodeGroupLookup.vue'

const mocks = vi.hoisted(() => ({ create: vi.fn() }))
vi.mock('../api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('../api/client')>(),
  getVersion: vi.fn(async () => ({ protocol_capabilities: {} })),
  fetchProtocolEndpointsPage: vi.fn(async () => ({ items: [], total: 0 })),
  fetchNodesPage: vi.fn(async () => ({ items: [{ id: 1, name: 'Fixture VPS', address: '192.0.2.1', is_enabled: true }] })),
  fetchManagedCertificatesPage: vi.fn(async () => ({ items: [] })),
  fetchNodeGroupsPage: vi.fn(async () => ({ items: [], total: 0 })),
  createProtocolEndpoint: mocks.create,
}))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))

const Button = defineComponent({ template: '<button><slot /></button>' })
const Dialog = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" :requestClose="() => {}" /></div>' })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Protocols }] })
  await router.push('/')
  await router.isReady()
  const wrapper = shallowMount(Protocols, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: {
    FormField, NodeGroupMembershipEditor, NodeGroupLookup, UiButton: Button, ModalDialog: Dialog, UiInput: true, UiSelect: true, UiTextarea: true, UiCheckbox: true, PageRefreshButton: true, TimeBadge: true,
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
  wrapper.findAllComponents({ name: 'UiInput' })[0].vm.$emit('update:modelValue', 'VLESS fixture')
  await click(wrapper, '下一步')
  await click(wrapper, '下一步')
}
const membership = { node_group_id: 9, name: 'Fixture group', code: 'fixture', description: '', revision: 7, is_enabled: true, sort_order: 0 }

describe('Protocol creation and actionable errors', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.create.mockResolvedValue({ protocol_endpoint: { id: 10 }, publish_status: 'queued', timing: {} })
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
