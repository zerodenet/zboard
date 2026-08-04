import type { NodeAddressCandidateSource } from '../api/client'

export function clearPreviousSuggestedAddress(
  currentValue: string,
  previousSuggestedValue: string,
  manuallyEdited: boolean,
): string {
  if (manuallyEdited) return currentValue
  return currentValue.trim() === previousSuggestedValue.trim() ? '' : currentValue
}

export function applyRecommendedNodeAddress(
  currentValue: string,
  manuallyEdited: boolean,
  recommendedValue?: string,
): string {
  if (manuallyEdited || currentValue.trim()) return currentValue
  return recommendedValue?.trim() || ''
}

export function nodeAddressCandidateSourceLabel(source: NodeAddressCandidateSource): string {
  return ({
    node_address: '节点地址',
    node_address_dns: '节点地址 DNS',
    ssh_host: 'SSH 主机',
    ssh_host_dns: 'SSH 主机 DNS',
    ssh_global: '主机网卡',
  } as Record<NodeAddressCandidateSource, string>)[source]
}
