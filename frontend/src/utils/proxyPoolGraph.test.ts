import { describe, expect, it } from 'vitest'
import { hydratePathNode, mergePathNode, parseProxyPoolGraph, validateProxyPoolGraph } from './proxyPoolGraph'

import { poolFixture } from './proxyPoolGraph.fixture'
describe('lossless pool editing',()=>{
 it('preserves the entire graph including advanced fields and credentials',()=>{
  const graph=poolFixture(),parsed=parseProxyPoolGraph(JSON.stringify(graph))
  parsed.outbounds=parsed.outbounds.map(node=>{const form=hydratePathNode(node);return mergePathNode(node,form,{...form})})
  expect(parsed).toEqual(graph)
 })
 it('merges only edited nested values without deleting advanced siblings',()=>{
  const node=poolFixture().outbounds[0]!,initial=hydratePathNode(node)
  const merged=mergePathNode(node,initial,{...initial,serverName:'new.example',host:'changed.example'})
  expect(merged.protocol.tls).toEqual({server_name:'new.example',alpn:['h2'],client_fingerprint:'chrome'})
  expect(merged.protocol.ws.headers).toEqual({Host:'changed.example','X-Custom':'keep'})
  expect(merged.protocol.mux_concurrency).toBe(8)
  expect(node.protocol.tls?.server_name).toBe('tls.example')
 })
 it('replaces security/protocol when explicitly changed',()=>{
  const node=poolFixture().outbounds[0]!,initial=hydratePathNode(node)
  expect(mergePathNode(node,initial,{...initial,security:'none'}).protocol).not.toHaveProperty('tls')
  const switched=mergePathNode(node,initial,{...initial,protocol:'socks5',password:'test'})
  expect(switched.protocol).toEqual({type:'socks5',server:'proxy.example',port:443,password:'test'})
 })
 it('rejects malformed, duplicate, missing and cyclic references',()=>{
  for(const raw of ['', 'null','[]','{}','{"proxies":[]}'])expect(()=>parseProxyPoolGraph(raw)).toThrow()
  let graph:any=poolFixture();graph.outbounds[1].tag='p1';expect(()=>validateProxyPoolGraph(graph)).toThrow('重复')
  graph=poolFixture();graph.target='missing';expect(()=>validateProxyPoolGraph(graph)).toThrow('target')
  graph=poolFixture();graph.outbound_groups[0].outbounds=['pick'];expect(()=>validateProxyPoolGraph(graph)).toThrow('循环')
  graph=poolFixture();graph.outbound_groups[2].default='p1';expect(()=>validateProxyPoolGraph(graph)).toThrow('组内成员')
  graph=poolFixture();graph.outbounds=[];expect(()=>validateProxyPoolGraph(graph)).toThrow('有效成员')
 })
})
