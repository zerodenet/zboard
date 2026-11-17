export interface OrderDraft<T> { key: string; request: T; orderId?: number }
// These versioned handler rejections guarantee that this attempt did not commit.
// An idempotency conflict may refer to an existing order, even with HTTP 400.
export function isDefiniteOrderRejection(cause: any): boolean {
  const status = cause?.response?.status, detail = cause?.response?.data?.error
  if (!detail || typeof detail !== 'object' || Array.isArray(detail) || detail.version !== 1) return false
  if (status === 400 && detail.code === 'validation_failed') {
    if (detail.fields !== undefined && (!detail.fields || typeof detail.fields !== 'object' || Array.isArray(detail.fields))) return false
    return !Object.prototype.hasOwnProperty.call(detail.fields || {}, 'idempotency_key')
  }
  return status === 503 && detail.code === 'discount_unavailable' || status === 502 && detail.code === 'discount_invalid'
}
export function readCheckoutOrderDraft<T>(storage: Storage, scope: string): OrderDraft<T> | null {
  let draft: OrderDraft<T>
  try {
    const raw = storage.getItem(scope)
    if (raw === null) return null
    draft = JSON.parse(raw)
  }
  catch { throw new Error('订单创建记录无法读取，请到“我的订单”核对后重新下单。') }
  if (!draft || typeof draft !== 'object' || Array.isArray(draft) || typeof draft.key !== 'string' || !draft.key || !draft.request || typeof draft.request !== 'object' || Array.isArray(draft.request) || draft.orderId !== undefined && (!Number.isSafeInteger(draft.orderId) || draft.orderId <= 0)) {
    throw new Error('订单创建记录不完整，请到“我的订单”核对。')
  }
  return draft
}
const pending = new Map<string, Promise<number>>()
// The intent belongs to a host checkout route, not the selected payment method.
// Persist before sending, so a lost creation response/reload repeats the same
// request and idempotency key instead of creating a second business order.
export function ensureCheckoutOrder<T>(storage: Storage, scope: string, request: T, create: (request: T, key: string) => Promise<{ id: number }>) {
  const existing = pending.get(scope)
  if (existing) return existing
  let draft = readCheckoutOrderDraft<T>(storage, scope)
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
