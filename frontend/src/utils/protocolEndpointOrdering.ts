import type { ProtocolEndpointOrderItem } from '../api/client'

// Reordering always returns a new array so dirty-state comparisons cannot be
// invalidated by mutating the complete ordering snapshot in place.
export function moveProtocolEndpointOrder<T extends ProtocolEndpointOrderItem>(
  items: readonly T[],
  index: number,
  delta: -1 | 1,
): T[] {
  const target = index + delta
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) return items.slice()
  const next = items.slice()
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}

export function moveProtocolEndpointOrderTo<T extends ProtocolEndpointOrderItem>(
  items: readonly T[],
  from: number,
  to: number,
): T[] {
  if (from < 0 || from >= items.length || to < 0 || to >= items.length || from === to) return items.slice()
  const next = items.slice()
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

export function protocolEndpointOrderChanged(
  current: readonly ProtocolEndpointOrderItem[],
  originalIds: readonly number[],
): boolean {
  return current.length !== originalIds.length || current.some((item, index) => item.id !== originalIds[index])
}
