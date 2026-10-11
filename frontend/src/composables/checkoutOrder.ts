interface OrderDraft<T> { key: string; request: T; orderId?: number }
const pending = new Map<string, Promise<number>>()
// The intent belongs to a host checkout route, not the selected payment method.
// Persist before sending, so a lost creation response/reload repeats the same
// request and idempotency key instead of creating a second business order.
export function ensureCheckoutOrder<T>(storage: Storage, scope: string, request: T, create: (request: T, key: string) => Promise<{ id: number }>) {
  const existing = pending.get(scope)
  if (existing) return existing
  let draft: OrderDraft<T> | undefined
  const raw = storage.getItem(scope)
  try { draft = raw ? JSON.parse(raw) : undefined } catch { throw new Error('订单创建记录无法读取，请到“我的订单”核对后重新下单。') }
  if (!draft) {
    draft = { key: newCheckoutIntent(), request }
    storage.setItem(scope, JSON.stringify(draft))
  }
  if (!draft.key || !draft.request) throw new Error('订单创建记录不完整，请到“我的订单”核对。')
  if (draft.orderId) return Promise.resolve(draft.orderId)
  const call = create(draft.request, draft.key).then(order => {
    if (!Number.isSafeInteger(order.id) || order.id <= 0) throw new Error('没有收到有效订单，请重试恢复。')
    draft.orderId = order.id
    storage.setItem(scope, JSON.stringify(draft))
    return order.id
  }).finally(() => pending.delete(scope))
  pending.set(scope, call)
  return call
}

export function restoreCheckoutIntent(storage: Storage, scope: string, fallback = newCheckoutIntent()) {
  try {
    const saved = storage.getItem(scope)
    if (saved && /^[a-f0-9]{32}$/.test(saved)) return saved
    storage.setItem(scope, fallback)
  } catch { /* Creation will report unavailable persistence before sending. */ }
  return fallback
}

export function newCheckoutIntent() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('')
}
