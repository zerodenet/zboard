import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchNodeGroupsPage, type NodeGroupSummary } from '../api/client'
import NodeGroupLookup from './NodeGroupLookup.vue'
vi.mock('../api/client', () => ({ fetchNodeGroupsPage: vi.fn() }))
const node = (id: number, name = `节点 ${id}`) => ({ id, name, code: `group-${id}`, is_enabled: true } as NodeGroupSummary)
function page(items: NodeGroupSummary[], total = items.length, offset = 0) {
  return { items, total, offset, limit: 25, page: { total, offset, limit: 25, next_cursor: null, previous_cursor: null }, aggregates: {}, facets: {} }
}
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => vi.mocked(fetchNodeGroupsPage).mockReset())
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
async function setup(modelValue = 0) {
  const wrapper = mount(NodeGroupLookup, { attachTo: document.body, props: { modelValue } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
async function open(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[aria-label="展开建议"]').trigger('click')
  await flushPromises()
}
const options = () => [...document.querySelectorAll('.ui-autocomplete-option')].map(item => item.textContent)
describe('NodeGroupLookup default browsing', () => {
  it('lists all nodes on opening without restricting choices to the selected node', async () => {
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(7)]))
    const wrapper = await setup(7)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(2), node(7)]))
    await open(wrapper)
    expect(options()).toHaveLength(3)
    expect(fetchNodeGroupsPage).toHaveBeenLastCalledWith({ offset: 0, limit: 25, q: undefined, enabled: true }, expect.anything())
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('节点 7')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    document.querySelectorAll<HTMLElement>('.ui-autocomplete-option')[1]!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([2])
    expect(wrapper.emitted('select')?.at(-1)).toEqual([node(2)])
  })
  it('loads the default list before any search and restores it when the search is emptied', async () => {
    const wrapper = await setup()
    expect(fetchNodeGroupsPage).not.toHaveBeenCalled()
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(2)]))
    await open(wrapper)
    expect(options()).toHaveLength(2)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(2, '日本节点')]))
    await wrapper.get('input').setValue('日本')
    await flushPromises()
    expect(fetchNodeGroupsPage).toHaveBeenLastCalledWith(expect.objectContaining({ q: '日本', offset: 0 }), expect.anything())
    expect(options()).toHaveLength(1)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(2)]))
    await wrapper.get('input').setValue('')
    await flushPromises()
    expect(fetchNodeGroupsPage).toHaveBeenLastCalledWith(expect.objectContaining({ q: undefined, offset: 0 }), expect.anything())
    expect(options()).toHaveLength(2)
  })
  it('clearing a selection restores the default list', async () => {
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(7)]))
    const wrapper = await setup(7)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(7)]))
    await open(wrapper)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(7)]))
    await wrapper.get('[aria-label="清除选择"]').trigger('click')
    await wrapper.setProps({ modelValue: 0 })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
    expect(fetchNodeGroupsPage).toHaveBeenLastCalledWith(expect.objectContaining({ q: undefined, offset: 0 }), expect.anything())
    expect(options()).toHaveLength(2)
  })
  it('makes nodes beyond the first bounded page available without requiring a keyword', async () => {
    const wrapper = await setup()
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page(Array.from({ length: 25 }, (_, i) => node(i + 1)), 30))
    await open(wrapper)
    expect(options()).toHaveLength(25)
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page(Array.from({ length: 5 }, (_, i) => node(i + 26)), 30, 25))
    document.querySelector<HTMLButtonElement>('.node-lookup-footer button')!.click()
    await flushPromises()
    expect(fetchNodeGroupsPage).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 25, limit: 25, q: undefined }), expect.anything())
    expect(options()).toHaveLength(30)
    expect(document.querySelector('.node-lookup-footer')?.textContent).toContain('30 / 30')
    expect(document.querySelector('.node-lookup-footer button')).toBeNull()
  })
  it('does not cancel an ID lookup or replace the default list when its selection arrives late', async () => {
    let finish!: (value: ReturnType<typeof page>) => void
    vi.mocked(fetchNodeGroupsPage).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await setup(7)
    const signal = vi.mocked(fetchNodeGroupsPage).mock.calls[0]?.[1]?.signal
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(7)]))
    await open(wrapper)
    expect(signal?.aborted).toBe(false)
    finish(page([node(7)]))
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('节点 7')
    expect(options()).toHaveLength(2)
  })
  it('rejects stale search results after the user clears the search', async () => {
    const wrapper = await setup()
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(2)]))
    await open(wrapper)
    let finish!: (value: ReturnType<typeof page>) => void
    vi.mocked(fetchNodeGroupsPage).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('input').setValue('日本')
    vi.mocked(fetchNodeGroupsPage).mockResolvedValueOnce(page([node(1), node(2)]))
    await wrapper.get('input').setValue('')
    await flushPromises()
    finish(page([node(2)]))
    await flushPromises()
    expect(options()).toHaveLength(2)
  })
})
