import { reactive } from 'vue'
import { flushPromises,mount,type VueWrapper } from '@vue/test-utils'
import { beforeEach,afterEach,expect,it,vi } from 'vitest'
import PluginSlot from './PluginSlot.vue'
import { clearPluginComposition,preparePluginComposition,compositionState } from './composition'
const mocks=vi.hoisted(()=>({fetch:vi.fn(),app:null as any,credential:undefined as string|undefined}))
vi.mock('../api/plugins',()=>({fetchPluginComposition:mocks.fetch,startPluginIdentity:vi.fn()}))
vi.mock('../stores/app',()=>({useAppStore:()=>mocks.app}))
vi.mock('../api/client',()=>({getAuthToken:()=>mocks.credential ?? mocks.app.token}))
const slot=(name='account.overview.cards',id='card')=>({plugin_id:'example.widgets',generation:1,availability:'ready',reserved_height:224,slot:{id,surface:'account',slot:name,title:'服务信息',entrypoint:'ui/card.html'}})
let wrapper:VueWrapper|undefined
beforeEach(()=>{clearPluginComposition();vi.clearAllMocks();mocks.credential=undefined;mocks.app=reactive({token:'user-one',user:{id:1},sessionEpoch:0})})
afterEach(()=>wrapper?.unmount())
function render(name='account.overview.cards'){
 wrapper=mount(PluginSlot,{props:{name,surface:'account'},global:{stubs:{PluginFrame:{name:'PluginFrame',props:['reservedHeight','targetUserId'],template:'<div class="test-frame" :data-height="reservedHeight" />'}}}})
 return wrapper
}
it('uses one prepared composition for every slot without waiting for frame readiness',async()=>{
 mocks.fetch.mockResolvedValue({revision:'one',slots:[slot(),slot('account.subscriptions.actions','action')]})
 await preparePluginComposition('account');const w=render();await flushPromises()
 expect(w.get('.test-frame').attributes('data-height')).toBe('224')
 expect(w.classes()).not.toContain('plugin-slot--pending')
 await w.setProps({name:'account.subscriptions.actions'});await flushPromises()
 expect(mocks.fetch).toHaveBeenCalledTimes(1)
 expect(w.find('.test-frame').exists()).toBe(true)
})
it('keeps a configured unavailable region visible with a retry action',async()=>{
 mocks.fetch.mockResolvedValue({revision:'offline',slots:[{...slot(),availability:'unavailable'}]})
 await preparePluginComposition('account');const w=render();await flushPromises()
 expect(w.get('.plugin-slot-unavailable').text()).toContain('服务信息暂不可用')
 expect(w.get('.plugin-slot-unavailable').attributes('style')).toContain('224px')
 mocks.fetch.mockResolvedValueOnce({revision:'online',slots:[slot()]})
 await w.get('button').trigger('click');await flushPromises();expect(w.find('.test-frame').exists()).toBe(true)
})
it('does not turn a failed catalogue read into an empty configured slot',async()=>{
 mocks.fetch.mockRejectedValue(new Error('offline'));await preparePluginComposition('account');const w=render();await flushPromises()
 expect(w.get('.plugin-slot-unavailable').text()).toContain('扩展区域暂不可用')
 expect(mocks.fetch).toHaveBeenCalledTimes(1)
})
it('keeps existing regions during a refresh failure but clears them for revoked access',async()=>{
 mocks.fetch.mockResolvedValue({revision:'one',slots:[slot()]});await preparePluginComposition('account');const w=render();await flushPromises()
 mocks.fetch.mockRejectedValueOnce(new Error('offline'));await preparePluginComposition('account',true);await flushPromises()
 expect(w.find('.test-frame').exists()).toBe(true);expect(w.get('.plugin-slot-refresh-error').text()).toContain('刷新失败')
 mocks.fetch.mockRejectedValueOnce({response:{status:403}});await preparePluginComposition('account',true);await flushPromises()
 expect(w.find('.test-frame').exists()).toBe(false)
})
it('discards an old in-flight catalogue after an explicit refresh',async()=>{
 let finish!:(value:any)=>void;mocks.fetch.mockReturnValueOnce(new Promise(resolve=>{finish=resolve}))
 const old=preparePluginComposition('account');mocks.fetch.mockResolvedValueOnce({revision:'new',slots:[]})
 await preparePluginComposition('account',true);finish({revision:'old',slots:[slot()]});await old
 expect(compositionState('account').state.revision).toBe('new');expect(compositionState('account').state.slots).toHaveLength(0)
})
it('isolates private catalogues across identities and never reuses sessions',async()=>{
 mocks.fetch.mockResolvedValue({revision:'one',slots:[slot()]});await preparePluginComposition('account');const old=compositionState('account').state
 mocks.app.token='user-two';const current=compositionState('account').state
 expect(current.slots).toHaveLength(0);expect(old.slots).toHaveLength(0)
 expect(current).not.toBe(old)
})
it('renders no placeholder when a successful composition confirms that no plugin contributes here',async()=>{
 mocks.fetch.mockResolvedValue({revision:'empty',slots:[]});await preparePluginComposition('account');const w=render();await flushPromises()
 expect(w.find('.plugin-slot').exists()).toBe(false)
})

it('keeps an already-empty slot empty when an unrelated catalogue refresh fails',async()=>{
 mocks.fetch.mockResolvedValue({revision:'empty',slots:[]});await preparePluginComposition('account');const w=render();await flushPromises()
 mocks.fetch.mockRejectedValueOnce(new Error('offline'));await preparePluginComposition('account',true);await flushPromises()
 expect(w.find('.plugin-slot').exists()).toBe(false)
})

it('never fetches a private catalogue under another tab’s newer credential',async()=>{
 const old=compositionState('account','user-one').state;mocks.credential='user-two'
 await preparePluginComposition('account',false,'user-one')
 expect(mocks.fetch).not.toHaveBeenCalled();expect(old.slots).toHaveLength(0);expect(old.error).toContain('登录状态已变化')
})

it('recreates the isolated container when configuration changes without a generation change',async()=>{
 mocks.fetch.mockResolvedValueOnce({revision:'one',slots:[{...slot(),config_revision:1}]})
 await preparePluginComposition('account');const w=render();await flushPromises()
 const previous=w.findComponent({name:'PluginFrame'}).vm
 mocks.fetch.mockResolvedValueOnce({revision:'two',slots:[{...slot(),config_revision:2}]})
 await preparePluginComposition('account',true);await flushPromises()
 expect(w.findComponent({name:'PluginFrame'}).vm).not.toBe(previous)
})
