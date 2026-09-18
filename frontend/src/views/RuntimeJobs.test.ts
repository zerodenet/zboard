import { mount, flushPromises } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, expect, it, vi } from 'vitest'
import RuntimeJobs from './RuntimeJobs.vue'
const api = vi.hoisted(() => ({ jobs: vi.fn(), queue: vi.fn() }))
vi.mock('../api/runtimeJobs', () => ({ fetchRuntimeJobs: api.jobs, fetchRuntimeQueue: api.queue }))
vi.mock('../api/runtimeHistory', () => ({ fetchRunHistory: vi.fn().mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25 }), resolveRun: vi.fn() }))
let wrapper: ReturnType<typeof mount> | undefined
const snapshot = { as_of: '2026-09-12T01:00:00Z', started_at: '2026-09-12T00:00:00Z', jobs: [{ id: 'expiry', name: '订阅到期处理', interval_seconds: 20, kind: 'periodic', state: 'idle', last_result: 'failed', last_error: '数据库暂不可用', running: 0, runs: 3, failures: 1, missed_runs: 2, timezone: 'Asia/Shanghai', misfire_policy: 'fire_once' }], queues: [{ id: 'admin_tasks', name: '运营任务', pending: 7, running: 2, delayed: 1, stale: 0, drafts: 3, failed: 4 }, { id: 'node_publish', name: '节点配置发布', pending: 0, running: 0, delayed: 0, stale: 0, drafts: 0, failed: 0 }], runtime: { event_spool: null }, plugin_host: null, admin_task_concurrency: 2, admin_item_concurrency: 8 }
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks(); vi.useRealTimers() })
async function render(data: unknown = snapshot) {
 api.jobs.mockResolvedValue(data); api.queue.mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25 })
 wrapper = mount(RuntimeJobs, { global: { plugins: [PrimeVue], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } } }); await flushPromises(); return wrapper
}
it('shows periods, actual failure and unavailable services without inventing healthy states', async () => {
 const w = await render(); expect(w.text()).toContain('每 20 秒'); expect(w.text()).toContain('Asia/Shanghai · 错过合并一次'); expect(w.text()).toContain('跳过 2'); expect(w.text()).toContain('数据库暂不可用'); expect(w.text()).toContain('插件宿主不可用'); expect(w.text()).toContain('此实例未启用事件队列')
 expect(api.queue).toHaveBeenCalledWith('admin_tasks', 0, expect.any(AbortSignal))
})
it('retains the last snapshot on refresh failure and cancels requests when leaving', async () => {
 const w = await render(); api.jobs.mockRejectedValue(new Error('offline'))
 await w.get('button[aria-label="刷新后台任务"]').trigger('click'); await flushPromises()
 expect(w.text()).toContain('运行状态未更新'); expect(w.text()).toContain('订阅到期处理')
 expect(w.text()).toContain('当前继续显示上次成功读取的快照'); expect(w.text()).toContain('重新读取')
 const signal = api.jobs.mock.calls.at(-1)![0] as AbortSignal; w.unmount(); wrapper = undefined; expect(signal.aborted).toBe(true)
})

it('keeps usable sections visible when one runtime partition fails', async () => {
 const w = await render({ ...snapshot, jobs: [], execution_queue: undefined, issues: [{ section: 'execution', message: '任务执行状态读取失败' }] })
 expect(w.text()).toContain('部分运行状态暂不可用')
 expect(w.text()).toContain('任务执行状态读取失败')
 expect(w.text()).toContain('任务执行状态暂不可用')
 expect(w.text()).toContain('运营任务')
 expect(w.text()).not.toContain('此实例尚无已注册的后台执行器')
 expect(api.queue).toHaveBeenCalledWith('admin_tasks', 0, expect.any(AbortSignal))
})
it('clears previous queue details on queue selection and bounds polling after unmount', async () => {
 vi.useFakeTimers(); const w = await render(); await w.findAll('button.queue-card')[1].trigger('click'); await flushPromises()
 expect(api.queue).toHaveBeenLastCalledWith('node_publish', 0, expect.any(AbortSignal))
 const calls = api.jobs.mock.calls.length; w.unmount(); wrapper = undefined; await vi.advanceTimersByTimeAsync(30000); expect(api.jobs).toHaveBeenCalledTimes(calls)
})

it('lists registered plugin tasks with ownership, schedule and queue state', async () => {
 const w = await render({ ...snapshot, plugin_host: { state: 'active', renewal_failures: 0 }, plugin_task_concurrency: 2, plugin_tasks: [{ id: 'plugin:example.sync:refresh', plugin_id: 'example.sync', plugin_name: '目录同步', task_id: 'refresh', name: '更新目录', kind: 'plugin', interval_seconds: 60, timeout_seconds: 20, state: 'queued', running: 0, runs: 3, failures: 1, last_result: 'failed', last_error: '插件任务执行超时' }] })
 expect(w.text()).toContain('更新目录'); expect(w.text()).toContain('到期等待'); expect(w.text()).toContain('超时 20 秒'); expect(w.text()).toContain('插件任务执行超时')
 expect(w.get('a[href="/admin/plugins/example.sync"]').text()).toContain('目录同步')
})
it('distinguishes an empty registration catalog from an unavailable host', async () => {
 const w = await render({ ...snapshot, plugin_host: { state: 'active', renewal_failures: 0 }, plugin_tasks: [] })
 expect(w.text()).toContain('尚无插件注册任务'); expect(w.text()).not.toContain('插件宿主不可用')
})

it('shows exclusive maintenance admission and on-demand migration cadence', async () => {
 const w = await render({ ...snapshot, execution_concurrency: 4, execution_queue: { pending: 2, running: 0, delayed: 0, unknown: 0, maintenance_reserved: true }, jobs: [{ id: 'database_migration', name: '数据库迁移', interval_seconds: 0, kind: 'on_demand', state: 'queued', last_result: 'never', running: 0, runs: 0, failures: 0 }] })
 expect(w.text()).toContain('维护任务已预约独占执行')
 expect(w.text()).toContain('普通任务暂不领取')
 expect(w.text()).toContain('数据库迁移')
 expect(w.text()).toContain('按需执行')
})

it('shows registration backlog as events without node links or invented leases', async () => {
 const w = await render({ ...snapshot, queues: [...snapshot.queues, { id: 'registration_messages', name: '注册消息', pending: 1, running: 0, delayed: 0, stale: 0, drafts: 0, failed: 0, oldest_at: '2026-09-13T00:00:00Z' }] })
 api.queue.mockResolvedValue({ items: [{ id: 42, kind: 'registration_event', state: 'pending', created_at: '2026-09-13T00:00:00Z' }], total: 1, offset: 0, limit: 25 })
 await w.findAll('button.queue-card')[2].trigger('click'); await flushPromises()
 expect(api.queue).toHaveBeenLastCalledWith('registration_messages', 0, expect.any(AbortSignal))
 const details = w.get('section[aria-label="队列明细"]')
 expect(details.text()).toContain('注册消息队列'); expect(details.text()).toContain('账户 #42 · 注册事件')
 expect(details.text()).toContain('事件时间'); expect(details.text()).not.toContain('租约到期')
 expect(details.find('a[href="/admin/nodes?node=42"]').exists()).toBe(false)
})
