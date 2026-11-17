import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import Tasks from './Tasks.vue'
import UiButton from '../components/UiButton.vue'
import UiSelect from '../components/UiSelect.vue'
import UiInput from '../components/UiInput.vue'
import UiTextarea from '../components/UiTextarea.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import TimeBadge from '../components/TimeBadge.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
const api = vi.hoisted(() => ({ task: vi.fn(), items: vi.fn(), page: vi.fn(), summary: vi.fn(), templates: vi.fn(), create: vi.fn(), run: vi.fn(), history: vi.fn() }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchAdminTask: api.task, fetchAdminTaskItems: api.items, fetchAdminTasksPage: api.page, fetchAdminTaskSummary: api.summary, fetchEmailTemplates: api.templates, createAdminTask: api.create, runAdminTask: api.run, fetchMailDeliveryHistory: api.history }))
let wrapper: ReturnType<typeof mount> | undefined
const task = { id: 126, type: 'email', scope: '{"user_ids":[4]}', content: '{}', current: 1, total: 1, status: 2, attempts: 1, max_attempts: 3, created_at: '2026-11-02T07:13:42Z', errors: '', succeeded_count: 1 }
const item = { id: 134, task_id: 126, target_type: 'user', target_id: '4', target_name: 'customer@example.test', target_description: '张先生', status: 2, attempts: 1, error: '', delivery_state: 'accepted' }
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })
async function render(items: unknown[] = [item], overrides: Record<string, unknown> = {}) {
 api.task.mockResolvedValue({ ...task, ...overrides }); api.history.mockResolvedValue({ items: [], total: 0 }); api.items.mockResolvedValue({ items, total: items.length, offset: 0, limit: 50 }); api.page.mockResolvedValue({ items: overrides.needs_review_count ? [{ ...task, ...overrides }] : [], total: overrides.needs_review_count ? 1 : 0, offset: 0, limit: 50 }); api.summary.mockResolvedValue({ total: 0 }); api.templates.mockResolvedValue([])
 const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/tasks', component: Tasks }] })
 await router.push('/admin/tasks?task=126'); await router.isReady()
 wrapper = mount(RouterView, { global: { plugins: [createPinia(), router], components: { UiInput, UiTextarea, UiCheckbox, UiButton, UiSelect, TimeBadge, PageRefreshButton }, stubs: { DetailDrawer: { props: ['open', 'title'], template: '<aside v-if="open" role="dialog" :aria-label="title"><slot/></aside>' }, ModalDialog: { props: ['open'], template: '<div v-if="open"><slot/></div>' } } } })
 await flushPromises(); return wrapper
}
it('shows email and the admin note directly, keeping identifiers in a closed disclosure', async () => {
 const w = await render(), cell = w.get('.task-item-table tbody td:first-child')
 expect(cell.text()).toContain('customer@example.test'); expect(cell.text()).toContain('张先生')
 expect(cell.text()).not.toContain('用户 #4'); expect(cell.text()).not.toContain('TaskItem'); expect(cell.text()).not.toContain('#134')
 const technical = w.get('.task-technical-details')
 expect(technical.attributes('open')).toBeUndefined(); expect(technical.text()).toContain('用户 #4'); expect(technical.text()).toContain('子任务 #134')
 expect(api.create).not.toHaveBeenCalled(); expect(api.run).not.toHaveBeenCalled()
})
it('uses readable business names for other task targets without repeating empty metadata', async () => {
 const w = await render([{ ...item, target_type: 'node', target_name: '香港入口', target_description: '' }])
 const cell = w.get('.task-item-table tbody td:first-child')
 expect(cell.text()).toBe('香港入口'); expect(cell.find('.task-target-description').exists()).toBe(false)
})
it('distinguishes a deleted user from a missing label on an older backend', async () => {
 const w = await render([{ ...item, target_name: '用户已删除', target_missing: true, target_description: '' }, { ...item, id: 135, target_name: undefined, target_description: '' }])
 const cells = w.findAll('.task-item-table tbody td:first-child')
 expect(cells[0]!.text()).toBe('用户已删除'); expect(cells[1]!.text()).toBe('用户信息暂不可用')
 expect(cells[1]!.text()).not.toContain('已删除'); expect(cells[0]!.text()).not.toContain('#4')
})

it.each([
 ['accepted', 2, '已提交', 'success'], ['suppressed', 2, '已取消', 'neutral'],
 ['unknown', 3, '待核验', 'warning'], ['not_accepted', 3, '提交失败', 'danger'], ['', 2, '结果未记录', 'neutral'],
])('renders one meaningful status for %s and keeps actions in their own column', async (delivery_state, status, label, tone) => {
 const w = await render([{ ...item, delivery_state, status }])
 const cell = w.get('.task-item-status')
 expect(cell.text()).toBe(label)
 expect(cell.findAll('.status-badge')).toHaveLength(1)
 expect(cell.get('.status-badge').attributes('data-tone')).toBe(tone)
 expect(cell.find('button').exists()).toBe(false)
 expect(w.get('.task-item-table thead').text()).not.toContain('结果说明')
 const row = w.get('.task-item-table tbody tr')
 expect(row.get('.table-action-column').text()).toContain(delivery_state === 'unknown' ? '核验结果' : '发送记录')
 expect(api.create).not.toHaveBeenCalled(); expect(api.run).not.toHaveBeenCalled()
})
it('opens read-only submission history from the action column', async () => {
 const w = await render()
 await w.get('.task-item-table .table-action-column button').trigger('click'); await flushPromises()
 expect(api.history).toHaveBeenCalledWith(126, 134, 0, expect.any(AbortSignal))
 expect(w.text()).toContain('已提交表示邮件服务器已接收。')
 expect(api.run).not.toHaveBeenCalled()
})
it('shows unresolved delivery in the summary and offers no blind retry', async () => {
 const w = await render([{ ...item, status: 3, delivery_state: 'unknown' }], { status: 3, needs_review_count: 1, errors: 'provider timeout' })
 expect(w.get('.task-facts .status-badge').text()).toBe('待核验')
 expect(w.text()).toContain('需要核验发送结果')
 expect(w.get('.task-table tbody .table-action-column').text()).toBe('查看结果')
 expect(w.get('.task-item-table tbody .table-action-column').text()).toContain('核验结果')
 expect(api.run).not.toHaveBeenCalled()
})

it('preserves concrete errors without coloring uncertain delivery as a confirmed failure', async () => {
 const w = await render([{ ...item, status: 3, delivery_state: 'unknown', error: 'SMTP 接收确认超时，请核对渠道日志' }], { status: 3, needs_review_count: 1 })
 expect(w.get('.task-item-table thead').text()).toContain('结果说明')
 expect(w.get('.task-item-table tbody').text()).toContain('SMTP 接收确认超时')
 expect(w.get('.task-item-status .status-badge').attributes('data-tone')).toBe('warning')
 expect(w.get('.task-item-table .output-block').classes()).not.toContain('output-danger')
})
