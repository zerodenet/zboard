import { describe, expect, it } from 'vitest'
import { subscriptionPreviewDelivery, subscriptionPreviewExtension } from './subscriptionPreviewRaw'
describe('raw subscription preview bytes', () => {
  it('keeps whitespace, CRLF and decomposed Unicode verbatim', () => {
    const value = '  {"name":"e\u0301 支付 🚀"}\r\n\r\n'
    expect(subscriptionPreviewDelivery(value, 'identity')).toBe(value)
    expect(subscriptionPreviewDelivery(value, 'base64')).toBe(Buffer.from(value, 'utf8').toString('base64'))
  })
  it('chooses the real format instead of making every download JSON', () => {
    expect(subscriptionPreviewExtension('application/json')).toBe('json')
    expect(subscriptionPreviewExtension('text/yaml')).toBe('yaml')
    expect(subscriptionPreviewExtension('text/plain')).toBe('txt')
  })
})
