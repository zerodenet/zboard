import { reactive } from 'vue'
import { flushPromises,mount,type VueWrapper } from '@vue/test-utils'
import { beforeEach,afterEach,expect,it,vi } from 'vitest'
import PluginLoginMethods from './PluginLoginMethods.vue'
import { preparePluginComposition,clearPluginComposition } from './composition'
const mocks=vi.hoisted(()=>({fetch:vi.fn(),start:vi.fn(),app:null as any}))
vi.mock('../api/plugins',()=>({fetchPluginComposition:mocks.fetch,startPluginIdentity:mocks.start}))
vi.mock('../api/client',()=>({getAuthToken:()=>mocks.app.token}))
vi.mock('../stores/app',()=>({useAppStore:()=>mocks.app}))
const contribution={plugin_id:'example.login',generation:2,config_revision:3,availability:'ready',presentation:'identity',slot:{id:'login',surface:'public',slot:'auth.login.methods',title:'登录服务',entrypoint:'ui/login.html'},providers:[{id:'example.login~github',name:'GitHub'},{id:'example.login~oidc',name:'企业账号'}]}
let wrapper:VueWrapper|undefined
beforeEach(()=>{clearPluginComposition();vi.clearAllMocks();mocks.app=reactive({token:'',user:{id:0},sessionEpoch:0});mocks.fetch.mockResolvedValue({revision:'one',slots:[contribution]});mocks.start.mockResolvedValue({authorization_url:'https://identity.example.test/auth?state=safe'})})
afterEach(()=>{wrapper?.unmount();vi.restoreAllMocks()})
async function render(beforeIdentityLogin?:()=>boolean){await preparePluginComposition('public');wrapper=mount(PluginLoginMethods,{props:{beforeIdentityLogin}});await flushPromises();return wrapper}
it('renders provider-specific native buttons directly from the prepared snapshot with no iframe or asset session',async()=>{
 const w=await render();expect(w.find('iframe').exists()).toBe(false)
 expect(w.findAll('.login-method-button').map(button=>button.text())).toEqual(['GitHub 登录','企业账号 登录'])
 expect(mocks.start).not.toHaveBeenCalled();expect(mocks.fetch).toHaveBeenCalledTimes(1)
})
it('checks the selected provider and presentation generation before preparing recovery and navigating',async()=>{
 const events:string[]=[];const prepare=vi.fn(()=>{events.push('prepare');return true})
 vi.spyOn(window.location,'assign').mockImplementation(()=>{events.push('navigate')})
 const w=await render(prepare);await w.findAll('.login-method-button')[1]!.trigger('click');await flushPromises()
 expect(mocks.start).toHaveBeenCalledWith('example.login','login','example.login~oidc',2,3)
 expect(events).toEqual(['prepare','navigate'])
})
it('keeps named unavailable providers and lets the user retry without removing the login area',async()=>{
 mocks.fetch.mockResolvedValue({revision:'offline',slots:[{...contribution,availability:'unavailable'}]});const w=await render()
 expect(w.findAll('.login-method-button')).toHaveLength(2);expect(w.get('.login-method-button').attributes('disabled')).toBeDefined()
 await w.findAll('button').find(button=>button.text()==='重试')!.trigger('click');await flushPromises()
 expect(mocks.fetch).toHaveBeenCalledTimes(2)
})
it.each(['javascript:alert(1)','https://user:secret@identity.example.test/auth'])('rejects unsafe provider navigation %s before recovery',async(url)=>{
 const prepare=vi.fn(()=>true);const navigate=vi.spyOn(window.location,'assign').mockImplementation(()=>{})
 mocks.start.mockResolvedValue({authorization_url:url});const w=await render(prepare)
 await w.get('.login-method-button').trigger('click');await flushPromises()
 expect(prepare).not.toHaveBeenCalled();expect(navigate).not.toHaveBeenCalled();expect(w.findAll('.login-method-button')).toHaveLength(2)
})
it('keeps the current editor mounted if recovery refuses navigation',async()=>{
 const navigate=vi.spyOn(window.location,'assign').mockImplementation(()=>{});const w=await render(()=>false)
 await w.get('.login-method-button').trigger('click');await flushPromises();expect(navigate).not.toHaveBeenCalled();expect(w.get('.login-method-button').attributes('disabled')).toBeUndefined()
})
it('does not navigate from an old account response and prevents duplicate starts',async()=>{
 const navigate=vi.spyOn(window.location,'assign').mockImplementation(()=>{});let finish!:(value:any)=>void
 mocks.start.mockReturnValueOnce(new Promise(resolve=>{finish=resolve}));const w=await render()
 await w.get('.login-method-button').trigger('click');await w.get('.login-method-button').trigger('click');expect(mocks.start).toHaveBeenCalledTimes(1)
 mocks.app.token='another';finish({authorization_url:'https://identity.example.test/auth'});await flushPromises();expect(navigate).not.toHaveBeenCalled()
})

it('keeps the named entry on configuration conflict and refreshes presentation before the next explicit start',async()=>{
 const navigate=vi.spyOn(window.location,'assign').mockImplementation(()=>{})
 const w=await render();mocks.start.mockRejectedValueOnce({response:{status:409,data:{message:'changed'}}})
 await w.get('.login-method-button').trigger('click');await flushPromises()
 expect(w.findAll('.login-method-button')).toHaveLength(2);expect(w.text()).toContain('配置已更新');expect(navigate).not.toHaveBeenCalled()
 mocks.fetch.mockResolvedValueOnce({revision:'two',slots:[{...contribution,config_revision:4}]})
 await w.findAll('button').find(button=>button.text()==='重试')!.trigger('click');await flushPromises()
 expect(mocks.start).toHaveBeenCalledTimes(1)
 await w.get('.login-method-button').trigger('click');await flushPromises()
 expect(mocks.start).toHaveBeenLastCalledWith('example.login','login','example.login~github',2,4)
 expect(navigate).toHaveBeenCalledOnce()
})
