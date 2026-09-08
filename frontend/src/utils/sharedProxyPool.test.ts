import {describe,expect,it} from 'vitest'
import {buildSharedProxyPool} from './sharedProxyPool'
import {buildPathNode,emptyPathNode} from './networkEntryPath'
describe('shared URLTest pool',()=>{
 it('namespaces member graphs while keeping credentials opaque',()=>{
  const node=buildPathNode({...emptyPathNode(),server:'192.0.2.1',password:'proxy'})
  const result=buildSharedProxyPool([node,node],'',300,50)
  expect(result.outbounds.map(item=>item.tag)).toEqual(['member-1/proxy','member-2/proxy'])
  expect(result.outbounds[0].protocol.password).toBe('proxy')
  expect(result.outbound_groups).toEqual([{tag:'shared',type:'url_test',outbounds:['member-1/proxy','member-2/proxy'],interval_seconds:300,tolerance_ms:50}])
  expect((node as any).outbounds[0].tag).toBe('proxy')
 })
 it('preserves nested raw relay references and rejects missing targets',()=>{
  const raw={outbounds:[{tag:'leaf',protocol:{type:'socks5',server:'leaf',port:1080}}],outbound_groups:[{tag:'chain',type:'relay',proxies:['leaf']}],target:'chain'}
  expect(buildSharedProxyPool([raw],'https://example.com/check',60,10).outbound_groups[0].proxies).toEqual(['member-1/leaf'])
  expect(()=>buildSharedProxyPool([{...raw,target:'missing'}],'',300,50)).toThrow('不存在')
 })
})
