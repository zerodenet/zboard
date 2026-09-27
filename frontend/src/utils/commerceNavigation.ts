import type { LocationQuery, LocationQueryRaw } from 'vue-router'

function positiveInteger(value: unknown) {
  const number = typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : 0
  return Number.isSafeInteger(number) && number > 0 ? number : 0
}

export function accountPurchaseRoute(query: LocationQuery, step: 'detail' | 'checkout' = 'detail') {
  const plan = positiveInteger(query.plan)
  const sku = plan ? positiveInteger(query.sku) : 0
  const page = positiveInteger(query.page)
  const limit = positiveInteger(query.limit)
  const search = typeof query.q === 'string' ? query.q.trim() : ''
  const targetQuery: LocationQueryRaw = {
    operation: 'purchase',
    ...(search ? { q: search } : {}),
    ...(page ? { page: String(page) } : {}),
    ...([6, 9, 12].includes(limit) ? { limit: String(limit) } : {}),
    ...(plan ? { plan: String(plan), step: step === 'checkout' && sku ? 'checkout' : 'detail' } : {}),
    ...(sku ? { sku: String(sku) } : {}),
  }
  return { path: '/account/plans', query: targetQuery }
}
