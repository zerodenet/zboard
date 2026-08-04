import { describe, expect, it } from 'vitest'
import {
  applyRecommendedNodeAddress,
  clearPreviousSuggestedAddress,
  nodeAddressCandidateSourceLabel,
} from './managedDNSAddressSuggestions'

describe('managed DNS address suggestions', () => {
  it('replaces only the previous automatic value when the node changes', () => {
    expect(clearPreviousSuggestedAddress('1.1.1.1', '1.1.1.1', false)).toBe('')
    expect(clearPreviousSuggestedAddress('8.8.8.8', '1.1.1.1', false)).toBe('8.8.8.8')
    expect(clearPreviousSuggestedAddress('1.1.1.1', '1.1.1.1', true)).toBe('1.1.1.1')
  })

  it('never overwrites an operator edit during refresh', () => {
    expect(applyRecommendedNodeAddress('', false, '1.1.1.1')).toBe('1.1.1.1')
    expect(applyRecommendedNodeAddress('8.8.8.8', false, '1.1.1.1')).toBe('8.8.8.8')
    expect(applyRecommendedNodeAddress('', true, '1.1.1.1')).toBe('')
  })

  it('uses stable human-readable source labels', () => {
    expect(nodeAddressCandidateSourceLabel('ssh_global')).toBe('主机网卡')
    expect(nodeAddressCandidateSourceLabel('node_address_dns')).toBe('节点地址 DNS')
  })
})
