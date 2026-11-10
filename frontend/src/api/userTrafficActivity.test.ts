import { beforeEach,expect,it,vi } from 'vitest'
import { fetchUserTrafficActivity } from './userTrafficActivity'
const get=vi.hoisted(()=>vi.fn())
vi.mock('./client',()=>({authenticatedAPI:{get}}))
const page=()=>({from:'2026-11-04',to:'2026-11-10',timezone:'Asia/Shanghai',projection_pending:false,items:[{user_id:22,used_bytes:'7',days:Array.from({length:7},(_,i)=>({date:`2026-11-${String(i+4).padStart(2,'0')}`,used_bytes:'1'}))}]})
beforeEach(()=>vi.resetAllMocks())
it('sends a single cancellable read with the visible users',async()=>{
 get.mockResolvedValue({data:{data:page()}});const signal=new AbortController().signal
 expect((await fetchUserTrafficActivity([22],signal)).items).toHaveLength(1)
 expect(get).toHaveBeenCalledWith('/admin/traffic/user-activity',{params:{user_ids:'22'},signal})
})
it.each(['missing','duplicate','sum','dates','negative'])('rejects %s data instead of treating it as no activity',async(kind)=>{
 const value=page()
 if(kind==='missing')value.items=[]
 if(kind==='duplicate')value.items.push(value.items[0]!)
 if(kind==='sum')value.items[0]!.used_bytes='99'
 if(kind==='dates')value.items[0]!.days[1]!.date='2026-11-04'
 if(kind==='negative')value.items[0]!.days[1]!.used_bytes='-1'
 get.mockResolvedValue({data:{data:value}})
 await expect(fetchUserTrafficActivity([22])).rejects.toThrow()
})
it('accepts an explicitly pending projection without inventing zero user totals',async()=>{
 get.mockResolvedValue({data:{data:{...page(),projection_pending:true,items:[]}}})
 const out=await fetchUserTrafficActivity([22]);expect(out.projection_pending).toBe(true);expect(out.items).toHaveLength(0)
})
