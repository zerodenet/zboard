import { describe, expect, it } from 'vitest'
import {
  buildSubscriptionURL,
  isBuiltInDeliveryMode,
  subscriptionDeliveryAuto,
  subscriptionDeliveryNative,
} from './subscriptionDelivery'

describe('subscription delivery URL', () => {
  it('keeps the canonical URL for automatic User-Agent detection', () => {
    expect(buildSubscriptionURL(
      'https://panel.example/api/v1/client/subscription/secret?template=clash',
      subscriptionDeliveryAuto,
    )).toBe('https://panel.example/api/v1/client/subscription/secret')
  })

  it('pins native and operator-managed template responses explicitly', () => {
    expect(buildSubscriptionURL(
      'https://panel.example/api/v1/client/subscription/secret',
      subscriptionDeliveryNative,
    )).toBe('https://panel.example/api/v1/client/subscription/secret?template=native')
    expect(buildSubscriptionURL(
      'https://panel.example/api/v1/client/subscription/secret',
      'sing-box',
    )).toBe('https://panel.example/api/v1/client/subscription/secret?template=sing-box')
  })

  it('recognizes only the two reserved delivery modes', () => {
    expect(isBuiltInDeliveryMode('auto')).toBe(true)
    expect(isBuiltInDeliveryMode('native')).toBe(true)
    expect(isBuiltInDeliveryMode('clash')).toBe(false)
  })
})
