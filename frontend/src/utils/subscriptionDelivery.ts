export const subscriptionDeliveryAuto = 'auto'
export const subscriptionDeliveryNative = 'native'

export function buildSubscriptionURL(baseURL: string, delivery: string): string {
  if (!baseURL) return ''
  const target = new URL(baseURL)
  if (!delivery || delivery === subscriptionDeliveryAuto) {
    target.searchParams.delete('template')
  } else {
    target.searchParams.set('template', delivery)
  }
  return target.toString()
}

export function isBuiltInDeliveryMode(value: string): boolean {
  return value === subscriptionDeliveryAuto || value === subscriptionDeliveryNative
}
