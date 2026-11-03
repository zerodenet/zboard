import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import SystemLoadPanel from './SystemLoadPanel.vue'
const api = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('../api/systemMetrics', async importOriginal => ({ ...await importOriginal<typeof import('../api/systemMetrics')>(), fetchSystemMetrics: api.fetch }))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })
const current = { at: '2026-11-03T10:00:00Z', cpu_percent: 0, cpu_count: 4, load_1: .68, memory: { used: 2e9, total: 8e9 }, disk: { used: 95e9, total: 100e9 }, process_cpu_cores: .26, process_rss: 65 * 1024 ** 2, heap_bytes: 4e7, goroutines: 42, rx_bytes_per_second: 100, tx_bytes_per_second: 0, issues: [] }
const points = [0, 1].map(i => ({ at: `2026-11-03T09:${58 + i}:00Z`, cpu_percent: i * 20, memory_percent: 25, disk_percent: 95, rx_bytes_per_second: 100, tx_bytes_per_second: 0 }))
async function render(sample: typeof current | null = current) { api.fetch.mockResolvedValue({ current: sample, points: sample ? points : [], range_minutes: 60, sample_seconds: 5, resolution_seconds: 60, scope: 'instance' }); wrapper = mount(SystemLoadPanel); await flushPromises(); return wrapper }
it('separates known zero, missing samples, host capacity and process usage', async () => {
 const w = await render(); expect(w.text()).toContain('0%'); expect(w.text()).toContain('0.26 核'); expect(w.text()).toContain('65.0 MiB'); expect(w.text()).toContain('磁盘空间不足'); expect(w.get('[aria-label="CPU"]').attributes('aria-valuenow')).toBe('0')
 w.unmount(); const empty = await render(null); expect(empty.text()).toContain('等待采样'); expect(empty.get('[aria-label="CPU未取得"]').attributes('aria-valuenow')).toBeUndefined(); expect(empty.text()).not.toContain('磁盘空间不足')
})
it('supports keyboard point inspection and separate network units', async () => {
 const w = await render(); const chart = w.get('[aria-label^="负载趋势图"]'); await chart.trigger('focus'); await chart.trigger('keydown', { key: 'Home' }); expect(w.get('.chart-inspection').text()).toContain('0%'); await chart.trigger('keydown', { key: 'ArrowRight' }); expect(w.get('.chart-inspection').text()).toContain('20%')
 expect(w.get('.chart-inspection').text()).toContain('100 B/s'); expect(w.get('.chart-inspection').text()).toContain('20%'); expect(w.findAll('[data-series]')).toHaveLength(5); expect(w.findAll('.active-point')).toHaveLength(5)
})
it('requests bounded ranges and retains prior data on refresh error', async () => {
 const w = await render(); await w.get('[value="720"]').trigger('click'); await flushPromises(); expect(api.fetch).toHaveBeenLastCalledWith(720, expect.any(AbortSignal))
 api.fetch.mockRejectedValueOnce({ response: { status: 503, data: { message: '采集接口不可用' } } }); await (w.vm as unknown as { refresh: () => Promise<void> }).refresh(); await flushPromises(); expect(w.text()).toContain('采集接口不可用'); expect(w.text()).toContain('65.0 MiB')
})

it('does not draw a line across missing samples or long collection gaps', async () => {
 api.fetch.mockResolvedValue({ current, range_minutes: 60, points: [points[0], { ...points[1], cpu_percent: null }, { ...points[1], at: '2026-11-03T10:00:00Z', cpu_percent: 30 }] })
 wrapper = mount(SystemLoadPanel); await flushPromises()
 const cpuPath = wrapper.get('[data-series="cpu_percent"]').attributes('d') || ''
 expect(cpuPath.match(/M/g)).toHaveLength(2)
 expect(cpuPath).not.toContain('L')
})

it('keeps the inspected timestamp stable while newer samples arrive', async () => {
 const w = await render(), chart = w.get('[aria-label^="负载趋势图"]')
 await chart.trigger('focus'); await chart.trigger('keydown', { key: 'Home' })
 api.fetch.mockResolvedValueOnce({ current, points: [...points, { ...points[1], at: '2026-11-03T10:00:00Z', cpu_percent: 80 }], range_minutes: 60 })
 await (w.vm as unknown as { refresh: () => Promise<void> }).refresh(); await flushPromises()
 expect(w.get('.chart-inspection').text()).toContain('09:58')
 expect(w.get('.chart-inspection').text()).not.toContain('80%')
})

it('shows the first collected point before a line can form', async () => {
 api.fetch.mockResolvedValueOnce({ current, points: [points[0]], range_minutes: 60 })
 wrapper = mount(SystemLoadPanel); await flushPromises()
 expect(wrapper.findAll('.active-point')).toHaveLength(5)
 expect(wrapper.find('.chart-inspection').exists()).toBe(false)
})

it('shows persistence failure while keeping live resources and cached curves available', async () => {
 api.fetch.mockResolvedValueOnce({ current, points, range_minutes: 60, history: { persistent: true, state: 'error', last_saved_at: '2026-11-03T09:58:00Z', pending_minutes: 2, error: '监控历史保存失败，正在自动重试；当前显示缓存数据。' } })
 wrapper = mount(SystemLoadPanel); await flushPromises()
 expect(wrapper.get('.page-alert').text()).toContain('保存失败')
 expect(wrapper.text()).toContain('65.0 MiB')
 expect(wrapper.findAll('[data-series]')).toHaveLength(5)
 expect(wrapper.text()).toContain('历史保存至')
})
