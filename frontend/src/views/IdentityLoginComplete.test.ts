import {flushPromises,mount} from '@vue/test-utils'
import {beforeEach,expect,it,vi} from 'vitest'
import IdentityLoginComplete from './IdentityLoginComplete.vue'
const mocks=vi.hoisted(()=>({finish:vi.fn(),sendCode:vi.fn(),replace:vi.fn(),setToken:vi.fn(),setUser:vi.fn(),load:vi.fn(),query:{} as Record<string,string>}))
vi.mock('../api/identities',()=>({finishIdentityLogin:mocks.finish,requestIdentityRegistrationCode:mocks.sendCode}))
vi.mock('vue-router',()=>({useRoute:()=>({query:mocks.query}),useRouter:()=>({replace:mocks.replace})}))
vi.mock('../stores/app',()=>({useAppStore:()=>({setToken:mocks.setToken,setUser:mocks.setUser,loadSystemStatus:mocks.load})}))
const render=()=>mount(IdentityLoginComplete,{global:{stubs:{AuthLegalLinks:true,UiInput:true,UiButton:{template:'<button><slot /></button>'},RouterLink:{template:'<a><slot /></a>'}}}})
beforeEach(()=>{vi.resetAllMocks();mocks.query={};mocks.load.mockResolvedValue({})})
it('accepts only the core completion response as a login session',async()=>{
 mocks.finish.mockResolvedValue({auth:{token:'core-token'},user:{id:1,email:'a@example.com',is_admin:false,status:'active'}})
 const wrapper=render();await flushPromises();expect(mocks.setToken).toHaveBeenCalledWith('core-token');expect(mocks.replace).toHaveBeenCalledWith('/account');wrapper.unmount()
})
it('a completed binding does not replace the current login token',async()=>{
 mocks.finish.mockResolvedValue({linked:true});const wrapper=render();await flushPromises();expect(mocks.setToken).not.toHaveBeenCalled();expect(mocks.replace).toHaveBeenCalledWith({path:'/account/security',query:{linked:'1'}});wrapper.unmount()
})
it('provider errors cannot inject a token from URL parameters',async()=>{
 mocks.query={error:'failed',token:'attacker-token'};const wrapper=render();await flushPromises();expect(mocks.finish).not.toHaveBeenCalled();expect(mocks.setToken).not.toHaveBeenCalled();expect(wrapper.text()).toContain('未完成');wrapper.unmount()
})

it('prompts for missing verified email without issuing a session',async()=>{
 mocks.finish.mockResolvedValue({registration_required:true,email:'hint@example.com'})
 const wrapper=render();await flushPromises();expect(wrapper.text()).toContain('补充注册邮箱');expect(mocks.setToken).not.toHaveBeenCalled();expect(wrapper.find('form').exists()).toBe(true);wrapper.unmount()
})
