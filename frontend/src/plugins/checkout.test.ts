import { describe, expect, it, vi } from 'vitest'
import { checkoutURL, paymentOptions, startPayment } from './checkout'
import { ensureCheckoutOrder, newCheckoutIntent, restoreCheckoutIntent } from '../composables/checkoutOrder'

const option = { id: 'one', title: '测试支付', prepare_action: 'provider.prepare', reconcile_action: 'provider.check' }
describe('host checkout contract', () => {
  it('retains a host checkout intent across reloads without requiring secure-context randomUUID', () => {
    localStorage.clear()
    const first = newCheckoutIntent()
    expect(first).toMatch(/^[a-f0-9]{32}$/)
    expect(restoreCheckoutIntent(localStorage, 'intent:reload', first)).toBe(first)
    expect(restoreCheckoutIntent(localStorage, 'intent:reload', newCheckoutIntent())).toBe(first)
    const other = restoreCheckoutIntent(localStorage, 'intent:other-sku')
    expect(other).not.toBe(first)
    expect(restoreCheckoutIntent(localStorage, 'intent:other-sku')).toBe(other)
  })
  it('reserves before asynchronous order creation, then navigates to validated HTTPS', async () => {
    const events: string[] = []
    let resolve!: (id: number) => void
    const pending = new Promise<number>(done => { resolve = done })
    const popup = { closed: false, opener: null, close: vi.fn(), location: { replace: vi.fn(() => events.push('navigate')) } }
    const result = startPayment(() => { events.push('create'); return pending }, async id => { events.push(`prepare:${id}`); return { checkout_url: 'https://cashier.example/pay' } }, () => { events.push('open'); return popup })
    expect(events).toEqual(['open', 'create'])
    resolve(17)
    expect(await result).toBe(17)
    expect(events).toEqual(['open', 'create', 'prepare:17', 'navigate'])
    expect(popup.close).not.toHaveBeenCalled()
  })
  it('closes the blank window when creation or preparation fails', async () => {
    for (const failedStage of ['create', 'prepare']) {
      const popup = { closed: false, opener: null, close: vi.fn(), location: { replace: vi.fn() } }
      await expect(startPayment(async () => { if (failedStage === 'create') throw new Error('failure'); return 1 }, async () => { throw new Error('failure') }, () => popup)).rejects.toThrow('failure')
      expect(popup.close).toHaveBeenCalledOnce()
      expect(popup.location.replace).not.toHaveBeenCalled()
    }
  })
  it('does not prepare an external payment after the reserved window closes during order creation', async () => {
    let resolve!: (id: number) => void
    const pending = new Promise<number>(done => { resolve = done })
    const popup = { closed: false, opener: null, close: vi.fn(), location: { replace: vi.fn() } }
    const prepare = vi.fn()
    const attempt = startPayment(() => pending, prepare, () => popup)
    popup.closed = true
    resolve(24)
    await expect(attempt).rejects.toThrow('支付窗口已关闭')
    expect(prepare).not.toHaveBeenCalled()
    expect(popup.location.replace).not.toHaveBeenCalled()
  })
  it('never creates an order when the popup is blocked', () => {
    const create = vi.fn()
    expect(() => startPayment(create, vi.fn(), () => { throw new Error('blocked') })).toThrow('blocked')
    expect(create).not.toHaveBeenCalled()
  })
  it('rejects unsafe checkout URLs and ambiguous option declarations', () => {
    for (const value of ['http://example.test', 'javascript:alert(1)', 'https://user:pass@example.test', '//example.test', 'https://example.test\n']) expect(() => checkoutURL(value)).toThrow()
    expect(checkoutURL('https://cashier.example/pay')).toBe('https://cashier.example/pay')
    expect(paymentOptions([option])).toEqual([{ ...option, disabled: false }])
    expect(() => paymentOptions([option, option])).toThrow()
    expect(() => paymentOptions([{ ...option, prepare_action: 'bad/action' }])).toThrow()
  })
  it('coalesces creation, recovers an uncertain result, and reuses the same host order', async () => {
    localStorage.clear()
    const create = vi.fn().mockRejectedValueOnce(new Error('lost response')).mockResolvedValue({ id: 23 })
    const first = ensureCheckoutOrder(localStorage, 'intent:coalescing', { sku: 5 }, create)
    const concurrent = ensureCheckoutOrder(localStorage, 'intent:coalescing', { sku: 5 }, create)
    expect(first).toBe(concurrent)
    await expect(first).rejects.toThrow('lost response')
    expect(await ensureCheckoutOrder(localStorage, 'intent:coalescing', { sku: 6 }, create)).toBe(23)
    expect(create.mock.calls[1]).toEqual(create.mock.calls[0])
    expect(await ensureCheckoutOrder(localStorage, 'intent:coalescing', { sku: 5 }, create)).toBe(23)
    expect(create).toHaveBeenCalledTimes(2)
  })
})
