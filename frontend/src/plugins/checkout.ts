export const paymentSlots = ['account.checkout.payment', 'account.orders.payment']
export interface PaymentOption {
  id: string
  title: string
  prepare_action: string
  reconcile_action: string
  disabled?: boolean
}
export interface PaymentStatus { state: 'loading' | 'ready' | 'empty' | 'error'; code?: string }
export function paymentStatus(value: unknown): PaymentStatus {
  const status = value as PaymentStatus
  if (!status || !['loading', 'ready', 'empty', 'error'].includes(status.state)
    || (status.code !== undefined && (typeof status.code !== 'string' || !/^[a-z0-9_]{1,64}$/.test(status.code)))) throw new Error('invalid payment status')
  return { state: status.state, code: status.code }
}
export function paymentStatusMessage(code?: string) {
  if (['session_unavailable', 'session_expired', 'permission_denied', 'forbidden', 'invalid_session'].includes(code || '')) return '支付扩展的会话或授权已失效，请重新加载。'
  if (code === 'provider_metadata_missing') return '支付渠道的收款信息尚未就绪，请联系管理员。'
  if (code === 'host_capability_missing') return '支付功能尚未就绪，请联系管理员检查插件授权。'
  if (['bridge_timeout', 'deadline_exceeded'].includes(code || '')) return '支付方式加载超时，请重试。'
  if (code === 'invalid_argument' || code === 'invalid_response') return '支付扩展返回的数据不符合接口要求，请联系管理员。'
  return '暂时无法加载支付方式，请重试。'
}
export interface CheckoutContext {
  order_id: number
  currency: string
  currency_precision: number
  payable_amount: number
  status: string
  return_url: string
}
const action = /^[a-z0-9][a-z0-9.-]{0,63}$/
export function paymentOptions(value: unknown): PaymentOption[] {
  if (!Array.isArray(value) || value.length > 16) throw new Error('invalid payment options')
  const ids = new Set<string>()
  return value.map(item => {
    if (!item || typeof item.id !== 'string' || !action.test(item.id) || ids.has(item.id)
      || typeof item.title !== 'string' || !item.title.trim() || item.title.length > 80
      || typeof item.prepare_action !== 'string' || typeof item.reconcile_action !== 'string' || !action.test(item.prepare_action) || !action.test(item.reconcile_action)
      || (item.disabled !== undefined && typeof item.disabled !== 'boolean')) throw new Error('invalid payment option')
    ids.add(item.id)
    return { id: item.id, title: item.title, prepare_action: item.prepare_action, reconcile_action: item.reconcile_action, disabled: !!item.disabled }
  })
}
export function checkoutURL(value: unknown) {
  if (typeof value !== 'string' || value.length > 4096 || value !== value.trim() || /[\u0000-\u0020\u007f]/.test(value)) throw new Error('收银台地址无效。')
  const url = new URL(value)
  if (url.protocol !== 'https:' || !url.hostname || url.username || url.password) throw new Error('收银台地址必须使用 HTTPS，且不能包含账号密码。')
  return url.href
}
export interface PaymentWindow { closed: boolean; location: { replace(url: string): void }; close(): void; opener: unknown }
export function reservePaymentWindow(): PaymentWindow {
  const popup = window.open('about:blank', '_blank')
  if (!popup) throw new Error('请允许本站打开支付窗口，再重试。')
  popup.opener = null
  return popup
}
// Called directly by the host button's click handler, before any async work.
export function startPayment(ensureOrder: () => Promise<number>, prepare: (id: number) => Promise<Record<string, unknown>>, reserve = reservePaymentWindow) {
  const popup = reserve()
  return (async () => {
    try {
      const id = await ensureOrder()
      if (popup.closed) throw new Error('支付窗口已关闭，可使用同一订单重试。')
      const result = await prepare(id)
      const target = checkoutURL(result.checkout_url)
      if (popup.closed) throw new Error('支付窗口已关闭，可使用同一订单重试。')
      popup.location.replace(target)
      return id
    } catch (error) { if (!popup.closed) popup.close(); throw error }
  })()
}
