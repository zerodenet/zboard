import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it } from 'vitest'
import UserTrafficActivity from './UserTrafficActivity.vue'
const item={user_id:22,used_bytes:'66571994112',days:['0','1','1073741823','1073741824','10737418240','53687091200','0'].map((used_bytes,i)=>({date:`2026-11-${String(i+4).padStart(2,'0')}`,used_bytes}))}
let wrapper:VueWrapper|undefined
afterEach(()=>{wrapper?.unmount();document.body.innerHTML=''})
async function render(extra:Record<string,unknown>={}){
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:{template:'<div />'}}]});await router.push('/');await router.isReady()
 wrapper=mount(UserTrafficActivity,{attachTo:document.body,props:{item,userLabel:'customer@example.test',link:'/traffic',...extra},global:{plugins:[router]}})
 await flushPromises();return wrapper
}
it('uses seven shared absolute intensity bands and one aggregate link',async()=>{
 const w=await render()
 expect(w.findAll('.activity-square').map(square=>square.attributes('data-level'))).toEqual(['0','1','1','2','3','4','0'])
 expect(w.findAll('a')).toHaveLength(1)
 expect(w.get('.activity-total').text()).toContain('GB')
 expect(w.get('.activity-total').attributes('aria-label')).toContain('customer@example.test')
 expect(w.findAll('button')).toHaveLength(7)
})
it('supports touch click, keyboard focus and Escape without duplicating a permanent label',async()=>{
 const w=await render();const day=w.findAll('.activity-day')[3]!
 await day.trigger('click');await flushPromises()
 expect(document.querySelector('.ui-tooltip-content')?.textContent).toContain('2026-11-07')
 expect(document.querySelector('.ui-tooltip-content')?.textContent).toContain('1.00 GB')
 await day.trigger('keydown',{key:'Escape'});await flushPromises()
 expect(document.querySelector('.ui-tooltip-content')).toBeNull()
 const today=w.findAll('.activity-day')[6]!;
 (today.element as HTMLButtonElement).focus();await flushPromises()
 expect(document.querySelector('.ui-tooltip-content')?.textContent).toContain('今天')
 expect(today.attributes('aria-label')).toContain('计费消耗 0 B')
})
it('shows zero as a dash while preserving known zero days and separates unavailable statistics',async()=>{
 const w=await render({item:{...item,used_bytes:'0',days:item.days.map(day=>({...day,used_bytes:'0'}))}})
 expect(w.get('.activity-total').text()).toBe('-')
 expect(w.findAll('[data-level="0"]')).toHaveLength(7)
 await w.setProps({error:'服务暂不可用'})
 expect(w.get('.activity-total').text()).toBe('-')
 await w.get('.activity-retry-icon').trigger('click');expect(w.emitted('retry')).toHaveLength(1)
 await w.setProps({error:'服务暂不可用',item:undefined});expect(w.find('.activity-total').exists()).toBe(false)
 await w.setProps({error:'',pending:true});expect(w.get('.activity-retry').text()).toBe('统计更新中')
 await w.setProps({pending:false,item:undefined,loading:true});expect(w.find('.activity-skeleton').exists()).toBe(true)
 expect(w.findAll('.activity-square')).toHaveLength(0)
})
