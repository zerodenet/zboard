import { defineComponent, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DetailDrawer from './DetailDrawer.vue'
import TaskTray from './TaskTray.vue'
import { activeTaskTrayHost } from '../composables/taskTrayHost'
import { clearTrackedTasks, trackAdminTask, trackedTaskIDs } from '../utils/taskTracker'
import { fetchAdminTask, type AdminTask } from '../api/client'
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchAdminTask: vi.fn() }))
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(clearTrackedTasks)
afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  clearTrackedTasks()
  await flushPromises()
  document.body.innerHTML = ''
})
function completedTask(id: number) {
  return { id, type: 'node_reconcile', status: 2, current: 1, total: 1, errors: '' } as AdminTask
}
async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/admin/nodes', component: {} }, { path: '/admin/tasks', component: {} },
  ] })
  await router.push('/admin/nodes?detail=7')
  const open = ref(false), nested = ref(false)
  const harness = defineComponent({
    components: { TaskTray, DetailDrawer },
    setup: () => ({ open, nested }),
    template: `<div><button id="background">背景操作</button><div class="page-task-host"><TaskTray /></div>
      <DetailDrawer :open="open" title="节点详情" @close="open = false"><button id="drawer-action">检测内核</button>
        <DetailDrawer :open="nested" title="第二个详情" @close="nested = false"><button>嵌套操作</button></DetailDrawer>
      </DetailDrawer></div>`,
  })
  const wrapper = mount(harness, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  return { wrapper, router, open, nested }
}
describe('TaskTray with a modal detail drawer', () => {
  it('keeps close and detail actions inside the dialog interaction and focus boundary', async () => {
    trackAdminTask(completedTask(85))
    const { router, open } = await setup()
    const original = document.querySelector('.task-tray')
    open.value = true
    await flushPromises()
    const drawer = document.querySelector<HTMLElement>('.detail-drawer')!
    const tray = drawer.querySelector<HTMLElement>('.task-tray')!
    expect(tray).toBe(original)
    expect(document.querySelectorAll('.task-tray')).toHaveLength(1)
    expect(document.body.style.pointerEvents).toBe('none')
    expect(drawer.style.pointerEvents).toBe('auto')
    expect(tray.closest('[aria-hidden="true"]')).toBeNull()
    const close = tray.querySelector<HTMLButtonElement>('.task-tray-actions button')!
    close.focus()
    await nextTick()
    expect(document.activeElement).toBe(close)
    close.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(drawer.querySelector('[aria-label="关闭详情"]'))
    close.click()
    await nextTick()
    expect(trackedTaskIDs.value).toEqual([])
    expect(open.value).toBe(true)
    trackAdminTask(completedTask(86))
    await nextTick()
    const details = document.querySelector<HTMLAnchorElement>('.task-tray-actions a')!
    details.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
    details.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/tasks')
    expect(router.currentRoute.value.query).toEqual({ task: '86', return_to: '/admin/nodes?detail=7' })
    expect(open.value).toBe(true)
  })
  it('moves the existing tray between nested drawers and restores its page position after closure', async () => {
    trackAdminTask(completedTask(85))
    const { wrapper, open, nested } = await setup()
    const tray = document.querySelector('.task-tray')!
    open.value = true
    await flushPromises()
    const outer = tray.closest('.detail-drawer')
    nested.value = true
    await flushPromises()
    expect(tray.closest('.detail-drawer')).not.toBe(outer)
    nested.value = false
    await flushPromises()
    expect(tray.closest('.detail-drawer')).toBe(outer)
    open.value = false
    await flushPromises()
    expect(wrapper.get('.page-task-host .task-tray').element).toBe(tray)
    expect(activeTaskTrayHost.value).toBeUndefined()
    expect(document.body.style.pointerEvents).toBe('')
  })
  it('keeps a single in-flight task read when moving into and out of the drawer', async () => {
    let finish!: (task: AdminTask) => void
    vi.mocked(fetchAdminTask).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    trackAdminTask({ ...completedTask(85), status: 1 })
    const { open } = await setup()
    open.value = true
    await flushPromises()
    open.value = false
    await flushPromises()
    expect(fetchAdminTask).toHaveBeenCalledTimes(1)
    finish(completedTask(85))
    await flushPromises()
  })
})
