import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WorkbenchEntityFilter from './WorkbenchEntityFilter.vue'
import UiAutocomplete from './UiAutocomplete.vue'
import { fetchNodesPage, fetchUsersPage, fetchSubscriptionsPage, fetchProtocolEndpointsPage } from '../api/client'
import { emptyEntityReferenceResponse, fetchAdminEntityReferences } from '../api/readModels'

vi.mock('../api/client', () => ({ fetchNodesPage: vi.fn(), fetchUsersPage: vi.fn(), fetchSubscriptionsPage: vi.fn(), fetchProtocolEndpointsPage: vi.fn() }))
vi.mock('../api/readModels', async importOriginal => ({ ...await importOriginal<typeof import('../api/readModels')>(), fetchAdminEntityReferences: vi.fn() }))
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.splice(0).forEach(x => x.unmount()); vi.resetAllMocks() })
beforeEach(() => vi.mocked(fetchAdminEntityReferences).mockResolvedValue(emptyEntityReferenceResponse()))
async function open(kind: 'user' | 'node' | 'subscription' | 'protocol_endpoint', id = '') {
  const wrapper = mount(WorkbenchEntityFilter, { props: { kind, modelValue: id, label: '对象' }, global: { stubs: { WorkbenchFilterChip: { template: '<div><slot :close="() => {}" /></div>' } } } })
  wrappers.push(wrapper); await flushPromises(); return wrapper
}
describe('name-based workbench entity filter', () => {
  it('cancels an unselected search without removing the applied filter', async () => {
    const refs = emptyEntityReferenceResponse()
    refs.targets['user:7'] = { id: 7, kind: 'user', display_name: 'selected@example.test' }
    vi.mocked(fetchAdminEntityReferences).mockResolvedValue(refs)
    vi.mocked(fetchUsersPage).mockResolvedValue({ items: [], total: 0 } as any)
    const wrapper = await open('user', '7')
    await wrapper.get('input[role="combobox"]').setValue('unfinished search')
    await wrapper.get('input[role="combobox"]').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('selected@example.test')
    await wrapper.get('button[aria-label="清除选择"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['']])
    expect(wrapper.emitted('apply')).toHaveLength(1)
  })
  it('keeps the typed keyword when typing opens the real suggestion list', async () => {
    vi.mocked(fetchUsersPage).mockImplementation(async params => ({
      items: params?.q === 'Tokyo'
        ? [{ id: 7, email: 'Tokyo@example.test' }]
        : [{ id: 8, email: 'unrelated@example.test' }],
      total: 1,
    } as any))
    const wrapper = await open('user')
    await wrapper.get('input[role="combobox"]').setValue('Tokyo')
    await flushPromises()
    expect(fetchUsersPage).toHaveBeenLastCalledWith({ q: 'Tokyo', offset: 0, limit: 20 }, { signal: expect.any(AbortSignal) })
    expect(wrapper.getComponent(UiAutocomplete).props('suggestions')?.map(item => item.name)).toEqual(['Tokyo@example.test'])
  })
  it('does not search the selected display label when reopening suggestions', async () => {
    const refs = emptyEntityReferenceResponse()
    refs.targets['user:7'] = { id: 7, kind: 'user', display_name: 'selected@example.test' }
    vi.mocked(fetchAdminEntityReferences).mockResolvedValue(refs)
    vi.mocked(fetchUsersPage).mockResolvedValue({ items: [], total: 0 } as any)
    const wrapper = await open('user', '7')
    wrapper.getComponent(UiAutocomplete).vm.$emit('open')
    await flushPromises()
    expect(fetchUsersPage).toHaveBeenLastCalledWith({ q: undefined, offset: 0, limit: 20 }, { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it.each([
    ['user', fetchUsersPage, { id: 7, email: 'member@example.test' }],
    ['node', fetchNodesPage, { id: 7, name: '东京服务器' }],
    ['subscription', fetchSubscriptionsPage, { id: 7, user_email: 'member@example.test', plan_name: 'Monthly' }],
    ['protocol_endpoint', fetchProtocolEndpointsPage, { id: 7, name: '日本 SS' }],
  ] as const)('searches %s by keyword with a bounded page and applies only the selected ID', async (kind, fetcher, item) => {
    vi.mocked(fetcher).mockResolvedValue({ items: [item], total: 30 } as any)
    const wrapper = await open(kind)
    const lookup = wrapper.getComponent(UiAutocomplete)
    lookup.vm.$emit('complete', { query: ' Tokyo ' }); await flushPromises()
    expect(fetcher).toHaveBeenCalledWith({ q: 'Tokyo', offset: 0, limit: 20 }, { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('apply')).toBeUndefined()
    const choice = lookup.props('suggestions')![0]
    expect(choice.name).not.toBe('#7')
    lookup.vm.$emit('item-select', { value: choice })
    expect(wrapper.emitted('update:modelValue')).toEqual([['7']])
    expect(wrapper.emitted('apply')).toHaveLength(1)
  })
  it('retains invalid saved IDs without sending an unbounded hydration request', async () => {
    const wrapper = await open('user', 'not-an-id')
    expect(fetchAdminEntityReferences).not.toHaveBeenCalled()
    expect(wrapper.getComponent(UiAutocomplete).props('modelValue')?.name).toBe('#not-an-id')
  })
  it('rejects a delayed label from a previous URL selection', async () => {
    let complete!: (value: any) => void
    vi.mocked(fetchAdminEntityReferences).mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = await open('user', '1')
    await wrapper.setProps({ modelValue: '2' }); await flushPromises()
    const response = emptyEntityReferenceResponse(); response.targets['user:1'] = { id: 1, kind: 'user', display_name: 'old@example.test' }
    complete(response); await flushPromises()
    expect(wrapper.getComponent(UiAutocomplete).props('modelValue')?.name).toBe('#2')
  })
})
