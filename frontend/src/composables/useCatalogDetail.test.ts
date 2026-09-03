import { effectScope, type EffectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchPlanCatalogItem, fetchPlanCatalogSKUs } from '../api/client'
import { useCatalogDetail } from './useCatalogDetail'

vi.mock('../api/client', () => ({ fetchPlanCatalogItem: vi.fn(), fetchPlanCatalogSKUs: vi.fn() }))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function page(ids: number[], total = ids.length, offset = 0) {
  return { items: ids.map(id => ({ id })), total, page: { offset, limit: 25, total } } as any
}
describe('catalog detail pages', () => {
  let scope: EffectScope
  let detail: ReturnType<typeof useCatalogDetail>
  beforeEach(() => {
    vi.resetAllMocks()
    scope = effectScope()
    detail = scope.run(useCatalogDetail)!
    vi.mocked(fetchPlanCatalogItem).mockImplementation(async id => ({ id } as any))
    vi.mocked(fetchPlanCatalogSKUs).mockResolvedValue(page([1, 2], 105))
  })
  afterEach(() => scope.stop())
  it('keeps an anchored SKU on its returned server page', async () => {
    vi.mocked(fetchPlanCatalogSKUs).mockResolvedValue(page([101, 105], 105, 100))
    expect(await detail.open(1, 'renew', 105)).toBe(true)
    expect(detail.selectedSkuId.value).toBe(105)
    expect(detail.offset.value).toBe(100)
    expect(detail.total.value).toBe(105)
  })
  it('does not silently replace an unavailable explicit SKU', async () => {
    expect(await detail.open(1, 'renew', 105)).toBe(false)
    expect(detail.selectedSku.value).toBeNull()
    expect(detail.error.value).toBeTruthy()
  })
  it('disables old-page selection on failure and retries the same requested page', async () => {
    await detail.open(1)
    vi.mocked(fetchPlanCatalogSKUs).mockRejectedValueOnce(new Error('offline'))
    expect(await detail.changePage({ offset: 25, limit: 25 })).toBe(false)
    expect(detail.selectedSku.value).toBeNull()
    expect(detail.error.value).toBeTruthy()
    vi.mocked(fetchPlanCatalogSKUs).mockResolvedValue(page([26], 105, 25))
    expect(await detail.retry()).toBe(true)
    expect(detail.selectedSkuId.value).toBe(26)
    expect(fetchPlanCatalogItem).toHaveBeenCalledOnce()
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(1, expect.objectContaining({ offset: 25 }), expect.anything())
  })
  it('corrects an empty page after its final rows were removed', async () => {
    await detail.open(1)
    vi.mocked(fetchPlanCatalogSKUs).mockResolvedValueOnce(page([], 30, 100)).mockResolvedValueOnce(page([26], 30, 25))
    expect(await detail.changePage({ offset: 100, limit: 25 })).toBe(true)
    expect(detail.offset.value).toBe(25)
    expect(detail.selectedSkuId.value).toBe(26)
  })
  it('ignores an old plan response and aborts it when a different plan opens', async () => {
    const late = deferred<any>()
    vi.mocked(fetchPlanCatalogSKUs).mockReturnValueOnce(late.promise).mockResolvedValueOnce(page([22]))
    const old = detail.open(1)
    await detail.open(2)
    expect(vi.mocked(fetchPlanCatalogSKUs).mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    late.resolve(page([11]))
    expect(await old).toBe(false)
    expect(detail.plan.value?.id).toBe(2)
    expect(detail.selectedSkuId.value).toBe(22)
  })
  it('prevents a late response and further requests after unmount', async () => {
    const late = deferred<any>()
    vi.mocked(fetchPlanCatalogSKUs).mockReturnValue(late.promise)
    const loading = detail.open(1)
    scope.stop()
    late.resolve(page([1]))
    expect(await loading).toBe(false)
    expect(await detail.open(2)).toBe(false)
    expect(fetchPlanCatalogSKUs).toHaveBeenCalledOnce()
    expect(detail.plan.value).toBeNull()
  })
})
