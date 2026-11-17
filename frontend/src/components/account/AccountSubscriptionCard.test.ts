import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it } from 'vitest'
import AccountSubscriptionCard from './AccountSubscriptionCard.vue'
it('keeps only recovery actions on a stopped service, with no reusable quota or future reset', async () => {
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:{template:'<div />'}}]})
 await router.push('/account/subscription');await router.isReady()
 const wrapper=mount(AccountSubscriptionCard,{props:{subscription:{id:1,status:'expired',ended_at:'2026-11-01T00:00:00Z',plan_name:'Pass Lite',flow_total:100,flow_used:0,next_reset_at:'2026-12-01T00:00:00Z',renewal_until:'2026-11-08T00:00:00Z',can_renew:true,can_reset:false} as any},global:{plugins:[router]}})
 expect(wrapper.text()).toContain('续费')
 expect(wrapper.text()).not.toContain('剩余流量')
 expect(wrapper.text()).not.toContain('下次重置')
 expect(wrapper.text()).not.toContain('连接设备')
 expect(wrapper.find('progress').exists()).toBe(false)
 expect(wrapper.find('a[href="/account/plans?operation=renew&subscription=1"]').exists()).toBe(true)
 wrapper.unmount()
})
