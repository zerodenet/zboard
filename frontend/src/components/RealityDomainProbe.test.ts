import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import RealityDomainProbe from './RealityDomainProbe.vue'
import { probeRealityDomains, type RealityProbeSnapshot } from '../api/client'

vi.mock('../api/client', () => ({ probeRealityDomains: vi.fn() }))
const result: RealityProbeSnapshot = { node_id: 7, sampled_at: '2026-10-09T01:00:00Z', items: [
  { server_name: 'valid.example', available: true, status: 'available', latency_ms: 62 },
  { server_name: 'expired.example', available: false, status: 'certificate', latency_ms: 80 },
] }
beforeEach(() => { vi.clearAllMocks(); vi.mocked(probeRealityDomains).mockResolvedValue(result) })

it('probes from the selected VPS and only allows passing domains to be used', async () => {
  const wrapper = mount(RealityDomainProbe, { props: { nodeId: 7, nodeName: 'Japan VPS' } })
  await flushPromises()
  expect(probeRealityDomains).toHaveBeenCalledWith(7, [], expect.any(AbortSignal))
  expect(wrapper.text()).toContain('Japan VPS')
  expect(wrapper.text()).toContain('证书不匹配或不可信')
  expect(wrapper.findAll('button').filter(button => button.text() === '使用此域名')).toHaveLength(1)
  await wrapper.findAll('button').find(button => button.text() === '使用此域名')!.trigger('click')
  expect(wrapper.emitted('select')).toEqual([['valid.example']])
  wrapper.unmount()
})

it('allows custom candidates and shows an actionable error on request failure', async () => {
  const wrapper = mount(RealityDomainProbe, { props: { nodeId: 7 } })
  await flushPromises()
  await wrapper.get('textarea').setValue('first.example, second.example\nthird.example')
  vi.mocked(probeRealityDomains).mockRejectedValueOnce({ response: { data: { message: '节点缺少 OpenSSL。' } } })
  await wrapper.findAll('button').find(button => button.text() === '重新探测')!.trigger('click')
  await flushPromises()
  expect(probeRealityDomains).toHaveBeenLastCalledWith(7, ['first.example', 'second.example', 'third.example'], expect.any(AbortSignal))
  expect(wrapper.text()).toContain('节点缺少 OpenSSL。')
  expect(wrapper.find('.probe-results').exists()).toBe(false)
  wrapper.unmount()
})

it('cancels old-node probes and ignores their stale results', async () => {
  let finish!: (value: RealityProbeSnapshot) => void
  vi.mocked(probeRealityDomains).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const wrapper = mount(RealityDomainProbe, { props: { nodeId: 7 } })
  const oldSignal = vi.mocked(probeRealityDomains).mock.calls[0][2]!
  vi.mocked(probeRealityDomains).mockResolvedValueOnce({ ...result, node_id: 8, items: [] })
  await wrapper.setProps({ nodeId: 8 })
  await flushPromises()
  expect(oldSignal.aborted).toBe(true)
  finish(result)
  await flushPromises()
  expect(wrapper.text()).not.toContain('valid.example')
  expect(wrapper.text()).toContain('本次没有找到通过检测的域名')
  wrapper.unmount()
  expect(vi.mocked(probeRealityDomains).mock.calls[1][2]!.aborted).toBe(true)
})

it('does not issue a probe without a VPS or for more than twelve candidates', async () => {
  const wrapper = mount(RealityDomainProbe, { props: { nodeId: 0 } })
  await flushPromises()
  expect(probeRealityDomains).not.toHaveBeenCalled()
  await wrapper.setProps({ nodeId: 7 })
  await flushPromises()
  await wrapper.get('textarea').setValue(Array.from({ length: 13 }, (_, i) => `host${i}.example`).join('\n'))
  await wrapper.findAll('button').find(button => button.text() === '重新探测')!.trigger('click')
  await flushPromises()
  expect(probeRealityDomains).toHaveBeenCalledTimes(1)
  expect(wrapper.text()).toContain('每次最多探测 12 个域名')
  wrapper.unmount()
})
