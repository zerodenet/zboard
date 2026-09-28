import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import WorkbenchFilterBar from './WorkbenchFilterBar.vue'
it('keeps clear and counts alongside common filters, and reveals restored advanced conditions',async()=>{
 const view=mount(WorkbenchFilterBar,{props:{active:true,activeCount:2,advancedCount:2},slots:{default:'<span>账户状态</span>',advanced:'<span>邮箱验证</span>'}})
 const trigger=view.get('button[aria-controls]')
 expect(trigger.attributes('aria-expanded')).toBe('true')
 expect(view.get('.advanced-filter-panel').attributes('id')).toBe(trigger.attributes('aria-controls'))
 await trigger.trigger('click');expect(view.find('.advanced-filter-panel').exists()).toBe(false);expect(trigger.text()).toContain('（2 项）')
 await view.get('button:not([aria-controls])').trigger('click');expect(view.emitted('clear')).toHaveLength(1)
 await view.setProps({advancedCount:1});expect(trigger.attributes('aria-expanded')).toBe('true');view.unmount()
})
it('does not add an advanced toggle for pages that have no optional conditions',()=>{
 const view=mount(WorkbenchFilterBar,{slots:{default:'<span>搜索</span>'}});expect(view.find('button[aria-controls]').exists()).toBe(false);view.unmount()
})
