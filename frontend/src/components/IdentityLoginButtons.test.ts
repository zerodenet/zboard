import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import IdentityLoginButtons from './IdentityLoginButtons.vue'
const api = vi.hoisted(() => ({ list:vi.fn(), start:vi.fn(), navigate:vi.fn() }))
vi.mock('../api/identities',()=>({fetchIdentityProviders:api.list,startIdentityLogin:api.start,navigateToIdentityProvider:api.navigate}))
const render=()=>mount(IdentityLoginButtons,{global:{stubs:{UiButton:{template:'<button><slot /></button>'}}}})
beforeEach(()=>{vi.resetAllMocks()})
it('keeps password login available when plugins cannot load',async()=>{
 api.list.mockRejectedValue(new Error('offline'));const wrapper=render();await flushPromises();expect(wrapper.find('section').exists()).toBe(false);wrapper.unmount()
})
it('starts only the selected core provider and shows failures without navigation',async()=>{
 api.list.mockResolvedValue([{id:'test.oauth',name:'Example'}]);api.start.mockRejectedValue(new Error('revoked'))
 const wrapper=render();await flushPromises();await wrapper.get('button').trigger('click');await flushPromises()
 expect(api.start).toHaveBeenCalledWith('test.oauth');expect(api.navigate).not.toHaveBeenCalled();expect(wrapper.text()).toContain('暂不可用');wrapper.unmount()
})
it('navigates using the authorization URL returned by the core',async()=>{
 api.list.mockResolvedValue([{id:'test.oauth',name:'Example'}]);api.start.mockResolvedValue({authorization_url:'https://id.example.com/auth?state=host-state'})
 const wrapper=render();await flushPromises();await wrapper.get('button').trigger('click');await flushPromises()
 expect(api.navigate).toHaveBeenCalledWith('https://id.example.com/auth?state=host-state');wrapper.unmount()
})
