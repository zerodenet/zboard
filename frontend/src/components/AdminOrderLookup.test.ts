import { shallowMount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AdminOrderLookup from './AdminOrderLookup.vue'
import UiAutocomplete from './UiAutocomplete.vue'
describe('assignment lookup', () => {
  it('ignores stale search results and clears selected identity while typing', async () => {
    let first!: (value: unknown) => void
    const load = vi.fn().mockImplementationOnce(() => new Promise(resolve => { first = resolve })).mockResolvedValueOnce({ items: [{ id: 2, label: 'new' }], total: 1 })
    const wrapper = shallowMount(AdminOrderLookup, { props: { modelValue: { id: 7, label: 'buyer' }, label: '用户', fetchPage: load } })
    const input = wrapper.getComponent(UiAutocomplete)
    input.vm.$emit('update:modelValue', 'new'); expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([null])
    input.vm.$emit('complete', { query: 'old' }); input.vm.$emit('complete', { query: 'new' }); await flushPromises()
    expect(load.mock.calls[0][2].aborted).toBe(true)
    first({ items: [{ id: 1, label: 'old' }], total: 1 }); await flushPromises()
    expect((input.props('suggestions') as any[])[0].id).toBe(2)
    wrapper.unmount()
  })
})

const mounted: ReturnType<typeof mount>[] = []
afterEach(() => { mounted.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
async function setup(fetchPage: any, modelValue: any = null, disabled = false) {
  const wrapper = mount(AdminOrderLookup, { attachTo: document.body, props: { modelValue, label: '商品', fetchPage, disabled } })
  mounted.push(wrapper); await flushPromises(); return wrapper
}
async function open(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[aria-label="展开建议"]').trigger('click'); await flushPromises()
}
const choices = () => [...document.querySelectorAll('.ui-autocomplete-option')].map(item => item.textContent?.trim())
const result = (labels: string[], total = labels.length) => ({ items: labels.map((label, i) => ({ id: i + 1, label })), total })

describe('assignment lookup default browsing', () => {
  it('loads choices on opening without keywords and keeps selected labels out of the query', async () => {
    const load = vi.fn().mockResolvedValue(result(['月付', '季付', '年付']))
    const wrapper = await setup(load, { id: 1, label: '月付' })
    expect(load).not.toHaveBeenCalled()
    await open(wrapper)
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    expect(choices()).toEqual(['月付', '季付', '年付'])
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('月付')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    document.querySelectorAll<HTMLElement>('.ui-autocomplete-option')[1]!.click(); await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ id: 2, label: '季付' }])
    await wrapper.setProps({ modelValue: { id: 2, label: '季付' } })
    await open(wrapper)
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    expect(choices()).toHaveLength(3)
  })
  it('restores the first default page when search or selection is cleared', async () => {
    const load = vi.fn().mockResolvedValue(result(['月付', '年付']))
    const wrapper = await setup(load)
    await open(wrapper)
    expect(load).toHaveBeenCalledWith('', 0, expect.any(AbortSignal))
    load.mockResolvedValueOnce(result(['年付']))
    await wrapper.get('input').setValue('年'); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('年', 0, expect.any(AbortSignal))
    expect(choices()).toEqual(['年付'])
    await wrapper.get('input').setValue(''); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    expect(choices()).toEqual(['月付', '年付'])
    await wrapper.setProps({ modelValue: { id: 2, label: '年付' } })
    await wrapper.get('[aria-label="清除选择"]').trigger('click'); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })
  it('browses subsequent pages without a keyword and resets the page for a search', async () => {
    const load = vi.fn().mockResolvedValueOnce(result(['第一页'], 51)).mockResolvedValueOnce(result(['第二页'], 51)).mockResolvedValueOnce(result(['第三页'], 51)).mockResolvedValueOnce(result(['搜索结果'], 1))
    const wrapper = await setup(load); await open(wrapper)
    const next = () => [...document.querySelectorAll<HTMLButtonElement>('.lookup-pager button')].find(button => button.textContent?.trim() === '下一页')!
    next().click(); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 25, expect.any(AbortSignal))
    expect(choices()).toEqual(['第二页'])
    next().click(); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 50, expect.any(AbortSignal))
    expect(next().disabled).toBe(true)
    await wrapper.get('input').setValue('结果'); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('结果', 0, expect.any(AbortSignal))
    expect(choices()).toEqual(['搜索结果'])
  })
  it('shows loading and failure states and retries the default page', async () => {
    let reject!: (reason: Error) => void
    const load = vi.fn().mockImplementationOnce(() => new Promise((_, fail) => { reject = fail })).mockResolvedValueOnce(result(['已恢复']))
    const wrapper = await setup(load); await open(wrapper)
    expect(document.querySelector('[role="status"]')?.textContent).toBe('正在加载选项…')
    expect(document.querySelector('.ui-autocomplete-empty')?.textContent).not.toBe('没有匹配结果')
    reject(new Error('offline')); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('商品加载失败')
    await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    expect(choices()).toEqual(['已恢复'])
  })
  it('cancels disabled or unmounted lookups and ignores their late responses', async () => {
    let finish!: (value: unknown) => void
    const load = vi.fn().mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValue(result(['新范围']))
    const wrapper = await setup(load, null, true)
    wrapper.getComponent(UiAutocomplete).vm.$emit('open'); await flushPromises()
    expect(load).not.toHaveBeenCalled()
    await wrapper.setProps({ disabled: false }); await open(wrapper)
    const signal = load.mock.calls[0][2]
    await wrapper.setProps({ disabled: true })
    expect(signal.aborted).toBe(true)
    finish(result(['旧范围'])); await flushPromises()
    expect(wrapper.getComponent(UiAutocomplete).props('suggestions')).toEqual([])
    await wrapper.setProps({ disabled: false })
    wrapper.getComponent(UiAutocomplete).vm.$emit('open'); await flushPromises()
    expect(load).toHaveBeenLastCalledWith('', 0, expect.any(AbortSignal))
    const latest = load.mock.calls.at(-1)![2]
    wrapper.unmount(); mounted.splice(mounted.indexOf(wrapper), 1)
    expect(latest.aborted).toBe(true)
  })
})
