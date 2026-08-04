import type { ProtocolEndpointOrderItem } from '../api/client'

// Reordering always returns a new array so dirty-state comparisons cannot be
// invalidated by mutating the complete ordering snapshot in place.
export function moveProtocolEndpointOrder(
  items: readonly ProtocolEndpointOrderItem[],
  index: number,
  delta: -1 | 1,
): ProtocolEndpointOrderItem[] {
  const target = index + delta
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) return items.slice()
  const next = items.slice()
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}

export function protocolEndpointOrderChanged(
  current: readonly ProtocolEndpointOrderItem[],
  originalIds: readonly number[],
): boolean {
  return current.length !== originalIds.length || current.some((item, index) => item.id !== originalIds[index])
}
