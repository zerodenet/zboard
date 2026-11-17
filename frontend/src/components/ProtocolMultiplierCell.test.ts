import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import ProtocolMultiplierCell from './ProtocolMultiplierCell.vue'
import { updateProtocolEndpointMultiplier } from '../api/client'
const app = reactive({ user: { id: 1 }, isAdmin: true })
vi.mock('../stores/app', () => ({ useAppStore: () => app }))
vi.mock('../api/client', () => ({ updateProtocolEndpointMultiplier: vi.fn() }))
let wrapper: VueWrapper | undefined
beforeEach(() => { vi.resetAllMocks(); app.user.id = 1; app.isAdmin = true; vi.mocked(updateProtocolEndpointMultiplier).mockImplementation(async (id, multiplier_milli) => ({ id, multiplier_milli })) })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
async function render(extra = {}) {
  wrapper = mount(ProtocolMultiplierCell, { attachTo: document.body, props: { endpointId: 7, multiplierMilli: 2000, name: '测试协议', ...extra } })
  await wrapper.get('button').trigger('click'); await flushPromises()
  return wrapper
}
function field() { return document.querySelector('input') as HTMLInputElement }
async function edit(value: string) { field().value = value; field().dispatchEvent(new Event('input', { bubbles: true })); await flushPromises() }
async function save() { document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await flushPromises() }
it('focuses the value and saves an exact fractional multiplier through the narrow API', async () => {
  const view = await render(); expect(document.activeElement).toBe(field())
  await edit('0.125'); await save()
  expect(updateProtocolEndpointMultiplier).toHaveBeenCalledOnce(); expect(updateProtocolEndpointMultiplier).toHaveBeenCalledWith(7, 125)
  expect(view.emitted('saved')).toEqual([[{ id: 7, multiplier_milli: 125 }]])
  expect(document.querySelector('form')).toBeNull()
})
it.each(['0', '-1', '100.001', '1.2345', 'invalid', '', '1e2'])('rejects invalid input without rounding or sending a write: %s', async value => {
  await render(); await edit(value); await save()
  expect(updateProtocolEndpointMultiplier).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('0.001–100')
  expect(field().value).toBe(value)
})
it.each(['0.001', '100'])('accepts a boundary value: %s', async value => {
  await render(); await edit(value); await save()
  expect(updateProtocolEndpointMultiplier).toHaveBeenCalledWith(7, Number(value) * 1000)
})
it('cancels or saves an unchanged value without a mutation', async () => {
  await render(); await save(); expect(updateProtocolEndpointMultiplier).not.toHaveBeenCalled()
  await wrapper!.get('button').trigger('click'); await flushPromises(); await edit('3')
  const cancel = [...document.querySelectorAll('button')].find(e => e.textContent === '取消')!
  cancel.click(); await flushPromises(); expect(document.querySelector('form')).toBeNull()
  expect(updateProtocolEndpointMultiplier).not.toHaveBeenCalled()
})
it('keeps the draft and concrete server error on failure, then retries explicitly', async () => {
  vi.mocked(updateProtocolEndpointMultiplier).mockRejectedValueOnce({ response: { data: { message: '协议已变更，请重新核对' } } })
  const view = await render(); await edit('2.5'); await save()
  expect(field().value).toBe('2.5'); expect(document.body.textContent).toContain('协议已变更，请重新核对')
  expect(view.emitted('saved')).toBeUndefined()
  await save(); expect(updateProtocolEndpointMultiplier).toHaveBeenCalledTimes(2)
})
it('prevents duplicate writes while a save is pending', async () => {
  let resolve!: (value: any) => void
  vi.mocked(updateProtocolEndpointMultiplier).mockReturnValue(new Promise(done => { resolve = done }))
  await render(); await edit('3'); await save(); await save()
  expect(updateProtocolEndpointMultiplier).toHaveBeenCalledOnce()
  expect(field().disabled).toBe(true)
  resolve({ id: 7, multiplier_milli: 3000 }); await flushPromises()
})
it.each(['target', 'identity', 'unmount'])('ignores a late response after %s changes', async kind => {
  let resolve!: (value: any) => void
  vi.mocked(updateProtocolEndpointMultiplier).mockReturnValue(new Promise(done => { resolve = done }))
  const view = await render(); await edit('3'); await save()
  if (kind === 'target') await view.setProps({ endpointId: 9 })
  else if (kind === 'identity') app.user.id = 2
  else view.unmount()
  resolve({ id: 7, multiplier_milli: 3000 }); await flushPromises()
  expect(view.emitted('saved')).toBeUndefined()
})
it('does not accept an unconfirmed or mismatched save result', async () => {
  vi.mocked(updateProtocolEndpointMultiplier).mockResolvedValue({ id: 9, multiplier_milli: 3000 })
  const view = await render(); await edit('3'); await save()
  expect(view.emitted('saved')).toBeUndefined(); expect(document.body.textContent).toContain('暂无法确认')
})

it('does not expose a writable editor after administrator access is lost', async () => {
  const view = await render(); await edit('3'); app.isAdmin = false; await flushPromises()
  expect(document.querySelector('form')).toBeNull()
  expect(view.get('button').attributes('disabled')).toBeDefined()
  expect(updateProtocolEndpointMultiplier).not.toHaveBeenCalled()
})
it('does not allow editing an unavailable protocol', async () => {
  const view = await render({ endpointId: 0 })
  expect(view.get('button').attributes('disabled')).toBeDefined()
  expect(view.get('button').attributes('title')).toContain('协议不可用')
  expect(document.querySelector('form')).toBeNull()
  expect(updateProtocolEndpointMultiplier).not.toHaveBeenCalled()
})
