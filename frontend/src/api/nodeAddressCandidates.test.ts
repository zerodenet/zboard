import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, fetchNodeAddressCandidates } from './client'

afterEach(() => vi.restoreAllMocks())
describe('node address candidate response compatibility', () => {
  it.each([null, []])('keeps empty legacy candidate families usable: %j', async empty => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: { data: { node_id: 9, policy: 'public_only', ipv4: empty, ipv6: empty, ssh_probe_status: 'not_configured' } } })
    const result = await fetchNodeAddressCandidates(9)
    expect(result.ipv4).toEqual([])
    expect(result.ipv6).toEqual([])
    expect(result.ssh_probe_status).toBe('not_configured')
  })
  it('preserves public IPv4 candidates and recommendations when IPv6 is empty', async () => {
    const ipv4 = [{ address: '1.1.1.1', source: 'node_address' }]
    vi.spyOn(api, 'get').mockResolvedValue({ data: { data: { node_id: 9, policy: 'public_only', ipv4, ipv6: null, recommended_ipv4: '1.1.1.1', ssh_probe_status: 'not_configured' } } })
    expect(await fetchNodeAddressCandidates(9)).toMatchObject({ ipv4, ipv6: [], recommended_ipv4: '1.1.1.1' })
  })
})
