import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Users from './Users.vue'
import Orders from './Orders.vue'
import Subscriptions from './Subscriptions.vue'
import Plugins from './Plugins.vue'
import { fetchUsersPage, fetchOrdersPage, fetchSubscriptionsPage, fetchAdminSubscriptionDetail } from '../api/client'
import { fetchPlugins } from '../api/plugins'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import UiButton from '../components/UiButton.vue'
import FormField from '../components/FormField.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
vi.mock('../api/client',async original=>({...await original<typeof import('../api/client')>(),fetchUsersPage:vi.fn(),fetchOrdersPage:vi.fn(),fetchSubscriptionsPage:vi.fn(),fetchAdminSubscriptionDetail:vi.fn()}))
vi.mock('../api/plugins',async original=>({...await original<typeof import('../api/plugins')>(),fetchPlugins:vi.fn()}))
vi.mock('../stores/app',()=>({useAppStore:()=>({user:{id:1},isAdmin:true,systemTimeZone:'Asia/Shanghai'})}))
const workbench=defineComponent({template:'<div><slot name="filters" /><slot name="actions" /><slot /><slot name="footer" /></div>'})
const drawer=defineComponent({props:['open'],template:'<div v-if="open"><slot /></div>'})
let view:VueWrapper|undefined
beforeEach(()=>{vi.resetAllMocks();for(const fn of [fetchUsersPage,fetchOrdersPage,fetchSubscriptionsPage])vi.mocked(fn).mockResolvedValue({items:[],total:0} as any)})
afterEach(()=>{view?.unmount();view=undefined})
async function open(component:any,url:string){
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:{template:'<div />'}}]});await router.push(url);await router.isReady()
 view=shallowMount(component,{global:{plugins:[router],components:{UiButton,FormField,PageRefreshButton},renderStubDefaultSlot:true,stubs:{DataWorkbench:workbench,DetailDrawer:drawer,WorkbenchFilterBar:false,FormField:false,TimeBadge:defineComponent({props:['value'],template:'<time>{{ value }}</time>'})}}});await flushPromises();return {view,router}
}
it('restores false role and verification filters, resets paging, clears filters and handles route restoration',async()=>{
 const {view,router}=await open(Users,'/admin/users?is_admin=false&email_verified=false&page=2&return_to=/admin')
 expect(fetchUsersPage).toHaveBeenNthCalledWith(1,expect.objectContaining({isAdmin:false,emailVerified:false,offset:50}),expect.anything())
 expect(view.get('button[aria-controls]').attributes('aria-expanded')).toBe('true')
 const role=view.findAllComponents(WorkbenchFilterSelect).find(c=>c.props('label')==='管理权限')!;role.vm.$emit('update:modelValue','true');role.vm.$emit('apply');await flushPromises()
 expect(router.currentRoute.value.query.page).toBeUndefined()
 expect(fetchUsersPage).toHaveBeenLastCalledWith(expect.objectContaining({isAdmin:true,emailVerified:false,offset:0}),expect.anything())
 view.findComponent({name:'WorkbenchFilterBar'}).vm.$emit('clear');await flushPromises();expect(router.currentRoute.value.query).toEqual({return_to:'/admin'})
 expect(fetchUsersPage).toHaveBeenLastCalledWith(expect.objectContaining({isAdmin:undefined,emailVerified:undefined}),expect.anything())
 await router.push('/admin/users?is_admin=false');await flushPromises();expect(fetchUsersPage).toHaveBeenLastCalledWith(expect.objectContaining({isAdmin:false}),expect.anything())
})
it('shows order purpose and customer email, and restores optional filters',async()=>{
 vi.mocked(fetchOrdersPage).mockResolvedValue({items:[{id:4,user_id:2,user_email:'buyer@example.test',order_type:'traffic_reset',status:'paid',amount_cents:500,trade_no:'T-4',created_at:'2026-09-28'}],total:1} as any)
 const {view,router}=await open(Orders,'/admin/orders');expect(view.text()).toContain('#4 · 重置流量')
 expect(view.text()).toContain('buyer@example.test')
 expect(view.get('button[aria-controls]').attributes('aria-expanded')).toBe('false')
 await router.push('/admin/orders?order_type=traffic_reset&user_id=2');await flushPromises();expect(view.get('button[aria-controls]').attributes('aria-expanded')).toBe('true')
 expect(fetchOrdersPage).toHaveBeenLastCalledWith(expect.objectContaining({orderType:'traffic_reset',userId:2}),expect.anything())
})
it('separates renewal recovery from quota exhaustion and starts quota editing collapsed',async()=>{
 const sub={id:3,user_id:2,plan_name:'Monthly',status:'expired',flow_used:100,flow_total:100,ended_at:'2026-09-28',end_reason:'expired',can_renew:true,renewal_until:'2026-10-05',active_credential_count:0}
 vi.mocked(fetchAdminSubscriptionDetail).mockResolvedValue(sub as any)
 const {view,router}=await open(Subscriptions,'/admin/subscriptions?subscription=3');expect(view.text()).toContain('服务已停止，可在');expect(view.text()).toContain('2026-10-05')
 expect(view.get('details.quota-disclosure').attributes('open')).toBeUndefined();expect(view.findComponent({name:'SubscriptionQuotaEditor'}).exists()).toBe(true)
 await router.push('/admin/subscriptions');await flushPromises();vi.mocked(fetchAdminSubscriptionDetail).mockResolvedValue({...sub,status:'active',ended_at:null,can_renew:false} as any)
 await router.push('/admin/subscriptions?subscription=3');await flushPromises();expect(view.text()).toContain('订阅仍在有效期内');expect(view.text()).not.toContain('服务已停止，可在')
})
it('filters plugins by actual state and purpose and clears filters while preserving unrelated context',async()=>{
 const manifest={capabilities:[],surfaces:['admin'],components:{server:{}},contributions:{pages:[]}}
 vi.mocked(fetchPlugins).mockResolvedValue([{id:'service',name:'Service',state:'active',version:'1',manifest,compatibility:{compatible:true}}, {id:'login',name:'Login',state:'disabled',version:'1',manifest:{...manifest,capabilities:['zboard.identity.provider.v1']},compatibility:{compatible:true}}] as any)
 const {view,router}=await open(Plugins,'/admin/plugins?state=failed&return_to=/admin');expect(view.text()).toContain('没有匹配的插件');expect(view.text()).not.toContain('离线导入不需要连接市场')
 const state=view.findAllComponents(WorkbenchFilterSelect).find(c=>c.props('label')==='运行状态')!;state.vm.$emit('update:modelValue','active');await flushPromises()
 expect(view.text()).toContain('Service');expect(view.text()).not.toContain('Login');expect(view.text()).toContain('1 / 2 个插件')
 await router.push('/admin/plugins?business=第三方登录与注册&return_to=/admin');await flushPromises();expect(view.text()).toContain('Login');expect(view.text()).not.toContain('Service')
 view.findComponent({name:'WorkbenchFilterBar'}).vm.$emit('clear');await flushPromises();expect(router.currentRoute.value.query).toEqual({return_to:'/admin'});expect(view.text()).toContain('2 / 2 个插件')
})
