import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, expect, it, vi } from 'vitest'
import NodeGroups from './NodeGroups.vue'
import FormField from '../components/FormField.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiTextarea from '../components/UiTextarea.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('../api/client', () => ({ fetchNodeGroupsPage: mocks.list, fetchNodeGroupDetail: mocks.detail, createNodeGroup: mocks.create, updateNodeGroup: mocks.update }))
vi.mock('../utils/taskTracker', () => ({ trackAdminTask: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))
const group = { id: 1, name: '高级组', code: 'premium', description: '', is_enabled: true, revision: 3, updated_at: '', protocol_endpoint_count: 0, network_entry_count: 1, protocol_endpoint_ids: null, network_entry_ids: [7] }
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue({ items: [group], total: 1 }); mocks.detail.mockResolvedValue(group); mocks.update.mockResolvedValue(group) })

it('edits and saves a front-only group without adding the landing endpoint', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: NodeGroups }] })
  await router.push('/'); await router.isReady()
  const wrapper = mount({ template: '<router-view />' }, { global: {
    plugins: [PrimeVue, router], components: { FormField, UiButton, UiInput, UiCheckbox, UiTextarea },
    stubs: { teleport: true, TimeBadge: true, PageRefreshButton: true, EndpointMultiLookup: true, NetworkEntryMultiLookup: true },
  } })
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text().includes('编辑'))!.trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('前置线路')
  await wrapper.get('#node-group-form').trigger('submit')
  await flushPromises()
  expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ expected_revision: 3, protocol_endpoint_ids: [], network_entry_ids: [7] }))
  wrapper.unmount()
})
