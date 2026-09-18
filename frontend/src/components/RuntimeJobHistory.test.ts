import { mount, flushPromises } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { expect, it, vi } from 'vitest'
import RuntimeJobHistory from './RuntimeJobHistory.vue'
import UiSelect from './UiSelect.vue'
const api = vi.hoisted(() => ({ history: vi.fn(), attempts: vi.fn(), cancel: vi.fn(), resolve: vi.fn(), reconcile: vi.fn() }))
vi.mock('../api/runtimeHistory', () => ({ fetchRunHistory: api.history, fetchRunAttempts: api.attempts, cancelRun: api.cancel, resolveRun: api.resolve, reconcileDNSRun: api.reconcile }))
it('requires verification evidence and saves the selected outcome before refreshing', async () => {
  api.history.mockResolvedValue({ items: [{ id: 'run-1', owner: 'plugin:example.sync', handler: 'sync', state: 'unknown', created_at: '2026-09-12T00:00:00Z', finished_at: null }], total: 1, offset: 0, limit: 25 })
  api.resolve.mockResolvedValue(undefined)
  const w = mount(RuntimeJobHistory, { props: { asOf: '2026-09-12T01:00:00Z', names: { sync: '同步任务' } }, global: { plugins: [PrimeVue] } })
  await flushPromises()
  expect(w.text()).toContain('结果待核验')
  await w.findAll('button').find(button => button.text() === '记录核验结果')!.trigger('click')
  expect(w.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  w.findAllComponents(UiSelect).at(-1)!.vm.$emit('update:modelValue', 'failed')
  await w.get('textarea').setValue('已核验供应商记录，未执行')
  await w.get('form').trigger('submit'); await flushPromises()
  expect(api.resolve).toHaveBeenCalledWith('run-1', 'failed', '已核验供应商记录，未执行')
  expect(w.emitted('resolved')).toHaveLength(1)
  w.unmount()
})

it('queues DNS inspection without resolving the original unknown outcome', async () => {
  api.history.mockResolvedValue({ items: [{ id: 'dns-1', owner: 'system', handler: 'dns_operation', state: 'unknown', created_at: '2026-09-12T00:00:00Z', finished_at: null }], total: 1, offset: 0, limit: 25 })
  api.resolve.mockClear(); api.reconcile.mockResolvedValue(undefined)
  const w = mount(RuntimeJobHistory, { props: { asOf: 'now', names: {} }, global: { plugins: [PrimeVue] } })
  await flushPromises()
  await w.findAll('button').find(button => button.text() === '核验远端 DNS')!.trigger('click')
  await flushPromises()
  expect(api.reconcile).toHaveBeenCalledWith('dns-1')
  expect(api.resolve).not.toHaveBeenCalled()
  expect(w.text()).toContain('核验任务已入队')
  expect(w.text()).toContain('结果待核验')
  w.unmount()
})

it('explains migration verification without promising automatic cutover or retry', async () => {
  api.history.mockResolvedValue({ items: [{ id: 'migration-1', owner: 'system', handler: 'database_migration', state: 'unknown', created_at: '2026-09-12T00:00:00Z', finished_at: null }], total: 1, offset: 0, limit: 25 })
  const w = mount(RuntimeJobHistory, { props: { asOf: 'now', names: {} }, global: { plugins: [PrimeVue] } })
  await flushPromises()
  expect(w.text()).toContain('数据库迁移')
  await w.findAll('button').find(button => button.text() === '记录核验结果')!.trigger('click')
  expect(w.text()).toContain('目标数据库的迁移结果和数据完整性')
  expect(w.text()).toContain('不会自动关闭维护模式或重新复制数据')
  w.unmount()
})

it('shows persisted attempts and requests durable cancellation for a running job', async () => {
  api.history.mockResolvedValue({ items: [{ id: 'run-active', owner: 'system', handler: 'sync', state: 'running', attempts: 2, max_attempts: 3, created_at: '2026-09-12T00:00:00Z', finished_at: null }], total: 1, offset: 0, limit: 25 })
  api.attempts.mockResolvedValue({ items: [{ attempt_number: 2, worker: 'host-b', state: 'running', started_at: '2026-09-12T00:01:00Z', expires_at: '2026-09-12T00:02:00Z', finished_at: null }], total: 2, offset: 0, limit: 25 })
  api.cancel.mockResolvedValue('cancel_requested')
  const w = mount(RuntimeJobHistory, { props: { asOf: 'now', names: { sync: '同步任务' } }, global: { plugins: [PrimeVue] } })
  await flushPromises()
  await w.findAll('button').find(button => button.text() === '查看尝试')!.trigger('click'); await flushPromises()
  expect(api.attempts).toHaveBeenCalledWith('run-active')
  expect(w.text()).toContain('第 2 次')
  expect(w.text()).toContain('host-b')
  await w.findAll('button').find(button => button.text() === '取消任务')!.trigger('click'); await flushPromises()
  expect(api.cancel).toHaveBeenCalledWith('run-active', '管理员从执行历史请求取消')
  expect(w.emitted('resolved')).toHaveLength(1)
  w.unmount()
})
