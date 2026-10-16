import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Dashboard from './Dashboard.vue'
import { fetchDashboardCurrent, fetchDashboardStatistics, fetchDashboardTrend, fetchDashboardTrafficRankings, type DashboardOverview } from '../api/dashboard'
import { fetchProtocolDeployments } from '../api/client'
import UiSection from '../components/UiSection.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import UiSegmentedControl from '../components/UiSegmentedControl.vue'
import DashboardTrafficRanking from '../components/DashboardTrafficRanking.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
vi.mock('../api/dashboard', () => ({ fetchDashboardCurrent: vi.fn(), fetchDashboardStatistics: vi.fn(), fetchDashboardTrend: vi.fn(), fetchDashboardTrafficRankings: vi.fn() }))
vi.mock('../api/client', () => ({ fetchProtocolDeployments: vi.fn() }))
const facts: DashboardOverview = {
 period: {range:'7d', from:'2026-09-22T00:00:00Z',to:'2026-09-28T10:00:00Z',previous_from:'2026-09-15T00:00:00Z',previous_to:'2026-09-21T10:00:00Z',bucket:'day',timezone:'Asia/Shanghai'},
 business:{revenue_cents:1500,previous_revenue_cents:1000,paid_orders:3,previous_paid_orders:4,new_orders:2,renew_orders:1,previous_renew_orders:0,new_users:8,previous_new_users:4,new_subscriptions:2,previous_new_subscriptions:1,active_subscriptions:3,expiring_within_3d:0,currency:'CNY',mixed_currency:false},
 service:{traffic_bytes:2000,previous_traffic_bytes:1000,active_subscriptions:null,active_flows:null,online_nodes:1,enabled_nodes:1},
 subscriptions:{expiring_within_24h:0,expiring_within_3d:0,expiring_within_7d:0,quota_exhausted:0},attention:{nodes_offline:0,deployments_unresolved:0,tickets_pending:0},
 infrastructure:{nodes_total:1,nodes_enabled:1,connector_online:1,ssh_verified:1,traffic_ready:1,protocol_endpoints:1,active_protocol_endpoints:1,published_plans:1,unresolved_deployments:0}, coverage:{principal_flows:false},trend:[],as_of:'2026-09-28T10:00:00Z',
}
function deferred<T>() { let resolve!: (value:T)=>void; const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve} }
let view:VueWrapper|undefined
beforeEach(()=>{
 vi.resetAllMocks()
 vi.mocked(fetchDashboardStatistics).mockResolvedValue({...structuredClone(facts), traffic: facts.service})
 vi.mocked(fetchDashboardCurrent).mockResolvedValue({...structuredClone(facts), active_subscriptions: facts.business.active_subscriptions})
 vi.mocked(fetchDashboardTrend).mockResolvedValue({period:facts.period,trend:[],as_of:facts.as_of,currency:"CNY",mixed_currency:false})
 vi.mocked(fetchDashboardTrafficRankings).mockResolvedValue({period:facts.period,nodes:[{id:1,name:'Node HK',traffic_bytes:1000,previous_traffic_bytes:500}],users:[{id:2,name:'test@example.test',traffic_bytes:2000,previous_traffic_bytes:0}],as_of:facts.as_of})
 vi.mocked(fetchProtocolDeployments).mockResolvedValue({items:[]} as any)
})
afterEach(()=>{view?.unmount();view=undefined})
async function open() {
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:{template:'<div />'}}]});await router.push('/admin');await router.isReady()
 view=mount(Dashboard,{global:{plugins:[router],components:{UiSection,UiMetricStrip,PageRefreshButton}}});await flushPromises();return view
}
describe('operations dashboard',()=>{
 it('shows successful metrics and comparisons while event loading fails',async()=>{
  vi.mocked(fetchProtocolDeployments).mockRejectedValue(new Error('events offline'))
  const wrapper=await open()
  expect(wrapper.text()).toContain('¥15.00');expect(wrapper.text()).toContain('↑ 50.0%');expect(wrapper.text()).toContain('↓ 25.0%')
  expect(wrapper.text()).toContain('Node HK');expect(wrapper.text()).toContain('上期为 0');expect(wrapper.text()).toContain('最近事件加载失败')
  expect(fetchDashboardStatistics).toHaveBeenCalledTimes(1);expect(fetchDashboardTrafficRankings).toHaveBeenCalledTimes(2)
 })
 it('does not wait for pending rankings to render metrics',async()=>{
  vi.mocked(fetchDashboardTrafficRankings).mockReturnValue(deferred<any>().promise)
  const wrapper=await open();expect(wrapper.text()).toContain('¥15.00');expect(wrapper.text()).toContain('正在加载流量排行')
 })
 it('cancels and ignores an older range response after switching',async()=>{
  const old=deferred<any>();vi.mocked(fetchDashboardStatistics).mockReturnValueOnce(old.promise)
  const wrapper=await open();const signal=vi.mocked(fetchDashboardStatistics).mock.calls[0][1]!.signal!
  wrapper.findComponent(UiSegmentedControl).vm.$emit('update:modelValue','today');await flushPromises()
  expect(signal.aborted).toBe(true);expect(fetchDashboardStatistics).toHaveBeenLastCalledWith('today',expect.objectContaining({signal:expect.any(AbortSignal)}))
  old.resolve({...facts,traffic:facts.service,business:{...facts.business,revenue_cents:99999}});await flushPromises()
  expect(wrapper.text()).toContain('¥15.00');expect(wrapper.text()).not.toContain('999.99');expect(wrapper.text()).toContain('对比昨日同期')
 })
 it('changes node periods without refetching business, users or events and ignores stale nodes', async()=>{
  const old=deferred<any>()
  vi.mocked(fetchDashboardTrafficRankings).mockImplementation(async (_range, options)=> options?.dimension === 'nodes' ? old.promise : ({ period:facts.period, nodes:[], users:[{id:2,name:'user-kept',traffic_bytes:10,previous_traffic_bytes:5}],as_of:facts.as_of }))
  const wrapper=await open()
  const nodeSignal=vi.mocked(fetchDashboardTrafficRankings).mock.calls[0][1]!.signal!
  vi.mocked(fetchDashboardTrafficRankings).mockResolvedValue({period:facts.period,nodes:[{id:3,name:'latest-node',traffic_bytes:40,previous_traffic_bytes:20}],users:[],as_of:facts.as_of})
  wrapper.findAllComponents(DashboardTrafficRanking)[0].vm.$emit('update:range','month');await flushPromises()
  expect(nodeSignal.aborted).toBe(true)
  expect(fetchDashboardTrafficRankings).toHaveBeenLastCalledWith('month',expect.objectContaining({dimension:'nodes'}))
  expect(fetchDashboardTrafficRankings).toHaveBeenCalledTimes(3)
  expect(fetchDashboardStatistics).toHaveBeenCalledTimes(1);expect(fetchProtocolDeployments).toHaveBeenCalledTimes(1)
  expect(wrapper.text()).toContain('user-kept');expect(wrapper.text()).toContain('对比上月同期')
  old.resolve({period:facts.period,nodes:[{id:4,name:'stale-node',traffic_bytes:99,previous_traffic_bytes:0}],users:[],as_of:facts.as_of});await flushPromises()
  expect(wrapper.text()).toContain('latest-node');expect(wrapper.text()).not.toContain('stale-node')
 })
 it('switches trend amount to count without fetching again', async()=>{
  vi.mocked(fetchDashboardTrend).mockResolvedValue({...facts,currency:"CNY",mixed_currency:false,trend:[{bucket_start:facts.period.from,revenue_cents:1000,paid_orders:1,new_orders:1,renew_orders:0},{bucket_start:facts.period.to,revenue_cents:500,paid_orders:2,new_orders:1,renew_orders:1}]})
  const wrapper=await open()
  expect(wrapper.findAll('.trend-bar')[0].attributes('style')).toContain('100%')
  wrapper.findAllComponents(UiSegmentedControl)[1].vm.$emit('update:modelValue','orders');await flushPromises()
  expect(wrapper.findAll('.trend-bar')[0].attributes('style')).toContain('50%')
  expect(wrapper.text()).toContain('柱高：订单量');expect(fetchDashboardStatistics).toHaveBeenCalledTimes(1)
 })

})

it('renders current service state before either historical statistics or trends return', async () => {
 vi.mocked(fetchDashboardStatistics).mockReturnValue(new Promise(() => {}))
 vi.mocked(fetchDashboardTrend).mockReturnValue(new Promise(() => {}))
 const wrapper = await open()
 expect(wrapper.text()).toContain('在线节点')
 expect(wrapper.text()).toContain('有效订阅')
 expect(wrapper.text()).toContain('当前没有需要处理的异常')
 expect(wrapper.text()).toContain('经营统计：加载中')
 expect(wrapper.text()).not.toContain('¥0.00')
})
it('keeps service status on statistics failure and retries only the failed section', async () => {
 vi.mocked(fetchDashboardStatistics).mockRejectedValue(new Error('history unavailable'))
 const wrapper = await open()
 expect(wrapper.text()).toContain('经营统计加载失败')
 expect(wrapper.text()).toContain('在线节点')
 expect(fetchDashboardCurrent).toHaveBeenCalledOnce()
 const retry = wrapper.findAll('button').find(b => b.text() === '重试')!
 await retry.trigger('click'); await flushPromises()
 expect(fetchDashboardStatistics).toHaveBeenCalledTimes(2)
 expect(fetchDashboardCurrent).toHaveBeenCalledOnce()
})
it('polls current state while history is stalled and stops on unmount', async () => {
 vi.useFakeTimers()
 try {
  vi.mocked(fetchDashboardStatistics).mockReturnValue(new Promise(() => {}))
  vi.mocked(fetchDashboardTrend).mockReturnValue(new Promise(() => {}))
  await open()
  await vi.advanceTimersByTimeAsync(3_001)
  expect(fetchDashboardCurrent).toHaveBeenCalledTimes(2)
  expect(fetchDashboardStatistics).toHaveBeenCalledOnce()
  view!.unmount(); view=undefined
  await vi.advanceTimersByTimeAsync(30_000)
  expect(fetchDashboardCurrent).toHaveBeenCalledTimes(2)
 } finally { vi.useRealTimers() }
})

it('renders independent trend amounts and currency while business statistics are pending', async () => {
 vi.mocked(fetchDashboardStatistics).mockReturnValue(new Promise(() => {}))
 vi.mocked(fetchDashboardTrend).mockResolvedValue({period:facts.period,as_of:facts.as_of,currency:'USD',mixed_currency:false,trend:[{bucket_start:facts.period.from,revenue_cents:700,paid_orders:2,new_orders:2,renew_orders:0}]})
 const wrapper = await open()
 const chart = wrapper.get('.trend-shell')
 expect(chart.text()).toContain('$7.00')
 expect(chart.text()).toContain('USD')
 expect(wrapper.get('.trend-bar').attributes('style')).toContain('100%')
})
it('does not label mixed trend amounts with a separate statistics currency', async () => {
 vi.mocked(fetchDashboardTrend).mockResolvedValue({period:facts.period,as_of:facts.as_of,currency:'',mixed_currency:true,trend:[{bucket_start:facts.period.from,revenue_cents:700,paid_orders:2,new_orders:2,renew_orders:0}]})
 const wrapper = await open()
 expect(wrapper.get('.trend-shell').text()).toContain('多币种')
 expect(wrapper.get('.trend-shell').text()).toContain('柱高：订单量')
 expect(wrapper.get('.trend-shell').text()).not.toContain('¥7.00')
})
