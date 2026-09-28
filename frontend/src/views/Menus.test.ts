import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Menus from './Menus.vue'
import { fetchMenus, saveMenus, type MenuSnapshot } from '../api/menus'
import { NAVIGATION_CHANGED } from '../stores/navigation'
import { notify } from '../utils/feedback'
import UiSelect from '../components/UiSelect.vue'

vi.mock('../api/menus', () => ({ fetchMenus: vi.fn(), saveMenus: vi.fn(), fetchNavigation: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn().mockResolvedValue(false) }))
const snapshot: MenuSnapshot = { revision: 7, nodes: [
  { id: 'group', surface: 'admin', parent_id: '', label: '资源', icon: 'nodes', path: '', position: 0, hidden: false, owner: 'core', plugin_id: '', page_id: '', condition: '' },
  { id: 'plugin:entry', surface: 'admin', parent_id: 'group', label: '插件服务', icon: 'plans', path: '/admin/extensions/example/home', position: 10, hidden: false, owner: 'plugin', plugin_id: 'example', page_id: 'home', condition: '' },
] }
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.resetAllMocks() })
async function setup() {
  vi.mocked(fetchMenus).mockResolvedValue(structuredClone(snapshot))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/menus', component: Menus }] })
  await router.push('/admin/menus')
  wrapper = mount(Menus, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}
describe('menu editor', () => {
  it('edits persisted plugin presentation and refreshes shared navigation after saving', async () => {
    const view = await setup()
    await view.get('[data-node-id="plugin:entry"] button').trigger('click')
    expect(view.get('input[aria-label="plugin:entry 路径"]').attributes('disabled')).toBeDefined()
    await view.get('input[aria-label="plugin:entry 名称"]').setValue('接入服务')
    await view.get('[aria-label="plugin:entry 隐藏"]').trigger('click')
    const changed = vi.fn()
    window.addEventListener(NAVIGATION_CHANGED, changed)
    vi.mocked(saveMenus).mockImplementation(async (_surface, value) => ({ ...JSON.parse(JSON.stringify(value)), revision: 8 }))
    await view.findAll('button').find(button => button.text() === '保存菜单')!.trigger('click')
    await flushPromises()
    expect(saveMenus).toHaveBeenCalledWith('admin', expect.objectContaining({ revision: 7, nodes: expect.arrayContaining([expect.objectContaining({ id: 'plugin:entry', label: '接入服务', hidden: true, owner: 'plugin' })]) }))
    expect(notify).toHaveBeenCalledWith('操作已完成', '菜单已保存', 'success')
    expect(view.get('[aria-label="菜单范围"]').attributes('disabled')).toBeUndefined()
    expect(changed).toHaveBeenCalledOnce()
    window.removeEventListener(NAVIGATION_CHANGED, changed)
  })
  it('preserves the draft on revision conflict and supports discarding it', async () => {
    const view = await setup()
    await view.get('[data-node-id="group"] button').trigger('click')
    await view.get('input[aria-label="group 名称"]').setValue('我的资源')
    expect(view.get('[aria-label="菜单范围"]').attributes('disabled')).toBeDefined()
    vi.mocked(saveMenus).mockRejectedValue({ response: { status: 409 } })
    await view.findAll('button').find(button => button.text() === '保存菜单')!.trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('其他操作修改')
    expect(view.get<HTMLInputElement>('input[aria-label="group 名称"]').element.value).toBe('我的资源')
    await view.findAll('button').find(button => button.text() === '撤销修改')!.trigger('click')
    expect(view.get<HTMLInputElement>('input[aria-label="group 名称"]').element.value).toBe('资源')
    expect(view.get('[aria-label="菜单范围"]').attributes('disabled')).toBeUndefined()
  })
  it('loads the selected menu scope without retaining the previous rows', async () => {
    const view = await setup()
    const account = { revision: 3, nodes: [{ ...snapshot.nodes[0], surface: 'account' as const, label: '用户菜单' }] }
    vi.mocked(fetchMenus).mockResolvedValue(account)
    view.findComponent(UiSelect).vm.$emit('update:modelValue', 'account')
    await flushPromises()
    expect(fetchMenus).toHaveBeenLastCalledWith('account', expect.any(AbortSignal))
    expect(view.text()).toContain('用户菜单')
    expect(view.text()).not.toContain('插件服务')
  })
  it('retains the last snapshot on refresh failure and can retry', async () => {
    const view = await setup()
    vi.mocked(fetchMenus).mockRejectedValueOnce(new Error('offline'))
    await view.get('button[aria-label="刷新菜单"]').trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('菜单加载失败')
    expect(view.text()).toContain('插件服务')
    await view.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.text()).toContain('插件服务')
  })
})
