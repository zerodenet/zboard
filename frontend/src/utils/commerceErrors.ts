import { normalizeApiErrorPayload } from './apiError'

const commerceErrorMessages: Record<string, string> = {
  plan_slug_conflict: '商品标识已被使用，请更换后重试。',
  plan_sku_code_conflict: 'SKU 编码已被使用，请更换后重试。',
  commerce_identifier_conflict: '商品或销售规格的标识已被使用，请检查商品标识和 SKU 编码。',
  plan_subscription_limit_reached: '该套餐的有效订阅数量已达到上限，暂时无法创建新的订阅订单。',
  commerce_persistence_failed: '数据保存失败，请稍后重试。',
}

export function commerceErrorCode(cause: any) {
  const response = normalizeApiErrorPayload(cause?.response?.data)
  return typeof response.error?.code === 'string' ? response.error.code : ''
}

export function commerceErrorMessage(cause: any, fallback: string) {
  const response = normalizeApiErrorPayload(cause?.response?.data)
  const code = typeof response.error?.code === 'string' ? response.error.code : ''
  return commerceErrorMessages[code] || response.message || fallback
}
