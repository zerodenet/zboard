import { mount,flushPromises } from '@vue/test-utils'
import { defineComponent,ref } from 'vue'
import { afterEach,beforeEach,expect,it,vi } from 'vitest'
import { useUserTrafficActivity } from './useUserTrafficActivity'
const fetch=vi.hoisted(()=>vi.fn())
vi.mock('../api/userTrafficActivity',()=>({fetchUserTrafficActivity:fetch}))
const data=(user_id:number)=>({from:'2026-11-04',to:'2026-11-10',projection_pending:false,items:[{user_id,used_bytes:'1',days:[]}]})
let wrapper:ReturnType<typeof mount>|undefined
beforeEach(()=>{vi.resetAllMocks();fetch.mockImplementation(async(ids:number[])=>data(ids[0]!))})
afterEach(()=>{wrapper?.unmount();vi.useRealTimers();vi.restoreAllMocks()})
function render(){
 const users=ref([{id:22}]),loading=ref(false),identity=ref('admin1');let result!:ReturnType<typeof useUserTrafficActivity>
 wrapper=mount(defineComponent({setup(){result=useUserTrafficActivity(users,loading,()=>identity.value);return()=>null}}))
 return{users,loading,identity,result}
}
it('batches once after each page finishes and preserves data during same-page refresh',async()=>{
 const {users,loading,result}=render();await flushPromises();expect(fetch).toHaveBeenCalledTimes(1)
 loading.value=true;await flushPromises();expect(result.byUser.value.has(22)).toBe(true)
 users.value=[{id:22}];await flushPromises();expect(fetch).toHaveBeenCalledTimes(1)
 loading.value=false;await flushPromises();expect(fetch).toHaveBeenCalledTimes(2)
 users.value=[];await flushPromises();expect(result.data.value).toBeNull();expect(fetch).toHaveBeenCalledTimes(2)
})
it('aborts old page reads and never applies their response to a new page or identity',async()=>{
 let finish!:(value:any)=>void;fetch.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
 const {users,identity,result}=render();await flushPromises();const oldSignal=fetch.mock.calls[0]![1] as AbortSignal
 users.value=[{id:23}];await flushPromises();expect(oldSignal.aborted).toBe(true)
 finish(data(22));await flushPromises();expect([...result.byUser.value.keys()]).toEqual([23])
 identity.value='admin2';await flushPromises();expect(fetch).toHaveBeenCalledTimes(3)
})
it('refreshes only visible pages each minute and releases the timer on unmount',async()=>{
 vi.useFakeTimers();const {result}=render();await flushPromises();expect(fetch).toHaveBeenCalledTimes(1)
 await vi.advanceTimersByTimeAsync(60_000);expect(fetch).toHaveBeenCalledTimes(2)
 vi.spyOn(document,'hidden','get').mockReturnValue(true)
 await vi.advanceTimersByTimeAsync(60_000);expect(fetch).toHaveBeenCalledTimes(2)
 expect(result.byUser.value.has(22)).toBe(true)
 wrapper!.unmount();expect(vi.getTimerCount()).toBe(0);wrapper=undefined
})
