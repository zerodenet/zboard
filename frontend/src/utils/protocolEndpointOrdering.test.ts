import { describe, expect, it } from 'vitest'
import type { ProtocolEndpointOrderItem } from '../api/client'
import { moveProtocolEndpointOrder, protocolEndpointOrderChanged } from './protocolEndpointOrdering'

const items: ProtocolEndpointOrderItem[] = [
  { id: 1, node_id: 1, name: 'A', protocol: 'vless', is_active: true, sort_order: 0 },
  { id: 2, node_id: 1, name: 'B', protocol: 'trojan', is_active: true, sort_order: 1 },
  { id: 3, node_id: 2, name: 'C', protocol: 'hysteria2', is_active: false, sort_order: 2 },
]

describe('protocol endpoint delivery ordering', () => {
  it('moves one endpoint without mutating the loaded complete scope', () => {
    const moved = moveProtocolEndpointOrder(items, 1, -1)
    expect(moved.map(item => item.id)).toEqual([2, 1, 3])
    expect(items.map(item => item.id)).toEqual([1, 2, 3])
    expect(protocolEndpointOrderChanged(moved, [1, 2, 3])).toBe(true)
  })

  it('does not move an endpoint beyond the complete scope', () => {
    expect(moveProtocolEndpointOrder(items, 0, -1)).toBe(items)
    expect(moveProtocolEndpointOrder(items, 2, 1)).toBe(items)
    expect(protocolEndpointOrderChanged(items, [1, 2, 3])).toBe(false)
  })
})
