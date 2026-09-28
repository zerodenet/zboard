import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Menus from './Menus.vue'
import { fetchMenus, saveMenus, type MenuSnapshot } from '../api/menus'
import { NAVIGATION_CHANGED } from '../stores/navigation'

vi.mock('../api/menus', () => ({ fetchMenus: vi.fn(), saveMenus: vi.fn(), fetchNavigation: vi.fn() }))
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
    expect(view.text()).toContain('菜单已保存')
    expect(changed).toHaveBeenCalledOnce()
    window.removeEventListener(NAVIGATION_CHANGED, changed)
  })
  it('preserves the draft on revision conflict and supports discarding it', async () => {
    const view = await setup()
    await view.get('[data-node-id="group"] button').trigger('click')
    await view.get('input[aria-label="group 名称"]').setValue('我的资源')
    vi.mocked(saveMenus).mockRejectedValue({ response: { status: 409 } })
    await view.findAll('button').find(button => button.text() === '保存菜单')!.trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('其他操作修改')
    expect(view.get<HTMLInputElement>('input[aria-label="group 名称"]').element.value).toBe('我的资源')
    await view.findAll('button').find(button => button.text() === '撤销修改')!.trigger('click')
    expect(view.get<HTMLInputElement>('input[aria-label="group 名称"]').element.value).toBe('资源')
  })
})
