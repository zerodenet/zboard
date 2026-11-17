import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ResourceDeletionDialog from './ResourceDeletionDialog.vue'
const api = vi.hoisted(() => ({ checkResourceDeletion: vi.fn(), deleteProtocolEndpoint: vi.fn(), deleteNetworkEntry: vi.fn() }))
vi.mock('../api/client', () => api)
const safe = { id: 21, name: '美国 SS', already_deleted: false, blockers: [], effects: ['保留历史账本。'] }
function button(text: string) { return [...document.body.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.includes(text))! }
let wrapper: ReturnType<typeof mount>
beforeEach(() => { vi.resetAllMocks(); api.checkResourceDeletion.mockResolvedValue(safe) })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
function open(kind: 'protocol' | 'entry' = 'protocol') { wrapper = mount(ResourceDeletionDialog, { attachTo: document.body, props: { target: { id: 21, name: '美国 SS' }, kind }, global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } }); return flushPromises() }
describe('resource deletion', () => {
 it('keeps the exact failure and dependency links inside the open dialog, and rechecks before retry', async () => {
  await open()
  api.deleteProtocolEndpoint.mockRejectedValueOnce({ response: { data: { message: '依赖已变化，请先处理。', data: { ...safe, blockers: [{ code: 'node_deleting', message: '服务器删除事务正在执行。', href: '/admin/nodes?deletion=3' }] } } } })
  button('确认删除').click(); await flushPromises()
  const dialog = document.body.querySelector('[role="dialog"]')!
  expect(dialog.textContent).toContain('美国 SS')
  expect(dialog.textContent).toContain('依赖已变化，请先处理。')
  expect(dialog.querySelector('a')?.getAttribute('href')).toBe('/admin/nodes?deletion=3')
  expect(button('确认删除').disabled).toBe(true)
  expect(wrapper.emitted('deleted')).toBeUndefined()
  button('重新检查').click(); await flushPromises(); button('确认删除').click(); await flushPromises()
  expect(api.checkResourceDeletion).toHaveBeenCalledTimes(2)
  expect(api.deleteProtocolEndpoint).toHaveBeenCalledTimes(2)
  expect(wrapper.emitted('deleted')).toHaveLength(1)
 })
 it('shows sold group impact without requiring server or other resources to be removed', async () => {
  api.checkResourceDeletion.mockResolvedValueOnce({ ...safe, warnings: [{ code: 'group_delivery', message: '该权限组将没有可用线路，套餐和订阅保留。', href: '/admin/node-groups?group=3' }] })
  await open(); expect(document.body.textContent).toContain('删除影响')
  expect(button('确认删除').disabled).toBe(false)
  button('确认删除').click(); await flushPromises(); expect(api.deleteProtocolEndpoint).toHaveBeenCalledWith(21)
  expect(wrapper.emitted('deleted')).toHaveLength(1)
 })
 it('fails closed when preflight is unavailable and never sends a deletion request', async () => {
  api.checkResourceDeletion.mockRejectedValueOnce(new Error('network'))
  await open(); expect(button('确认删除').disabled).toBe(true)
  expect(document.body.textContent).toContain('无法检查删除依赖')
  expect(api.deleteProtocolEndpoint).not.toHaveBeenCalled()
 })
 it('does not repeat deletion for an already deleted identity', async () => {
  api.checkResourceDeletion.mockResolvedValueOnce({ ...safe, already_deleted: true })
  await open(); expect(document.body.textContent).toContain('无需再次删除')
  button('关闭并刷新列表').click(); expect(wrapper.emitted('deleted')).toHaveLength(1)
  expect(api.deleteProtocolEndpoint).not.toHaveBeenCalled()
 })
 it('keeps an entry failure beside its action and permits an explicit retry', async () => {
  api.deleteNetworkEntry.mockRejectedValueOnce({ response: { data: { message: '发布结果待核验。' } } })
  await open('entry'); button('确认删除').click(); await flushPromises()
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('发布结果待核验。')
  expect(wrapper.emitted('deleted')).toBeUndefined()
  button('确认删除').click(); await flushPromises(); expect(wrapper.emitted('deleted')).toHaveLength(1)
 })
})
