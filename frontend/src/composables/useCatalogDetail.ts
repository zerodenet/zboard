import { computed, ref } from 'vue'
import { fetchPlanCatalogItem, fetchPlanCatalogSKUs, type CatalogOperation, type PageResult, type PlanCatalogItem, type PlanSKU } from '../api/client'
import { useRemoteResource } from './useRemoteResource'

interface DetailQuery {
  planId: number
  operation: CatalogOperation
  offset: number
  limit: number
  anchorId?: number
}

interface DetailData {
  plan: PlanCatalogItem | null
  page: PageResult<PlanSKU> | null
}

/** A plan's SKU collection is a real page, including deep-link positioning. */
export function useCatalogDetail() {
  let query: DetailQuery | null = null
  const selectedSkuId = ref(0)
  const resource = useRemoteResource<DetailData>({
    initial: () => ({ plan: null as PlanCatalogItem | null, page: null as PageResult<PlanSKU> | null }),
    fetch: async ({ signal }): Promise<DetailData> => {
      const current = query!
      const cached = resource.data.value.plan
      const [plan, firstPage] = await Promise.all([
        cached?.id === current.planId ? cached : fetchPlanCatalogItem(current.planId, { signal }),
        fetchPlanCatalogSKUs(current.planId, {
          operation: current.operation, offset: current.offset, limit: current.limit, anchorId: current.anchorId,
        }, { signal }),
      ])
      if (signal.aborted) throw new Error('catalog request canceled')
      if (plan.id !== current.planId) throw new Error('catalog identity mismatch')
      if (current.anchorId && !firstPage.items.some(item => item.id === current.anchorId)) throw new Error('SKU anchor missing')
      let page = firstPage
      if (!current.anchorId && page.total > 0 && page.page.offset >= page.total) {
        page = await fetchPlanCatalogSKUs(current.planId, {
          operation: current.operation, limit: current.limit,
          offset: Math.floor((page.total - 1) / current.limit) * current.limit,
        }, { signal })
      }
      return { plan, page }
    },
    errorMessage: '套餐规格加载失败，请重试。',
  })
  const plan = computed(() => resource.data.value.plan)
  const skus = computed(() => resource.data.value.page?.items || [])
  const total = computed(() => resource.data.value.page?.total || 0)
  const offset = computed(() => resource.data.value.page?.page.offset || 0)
  const limit = computed(() => resource.data.value.page?.page.limit || 25)
  const selectedSku = computed(() => !resource.loading.value && !resource.error.value
    ? skus.value.find(item => item.id === selectedSkuId.value) || null : null)

  async function load(next: DetailQuery) {
    query = next
    selectedSkuId.value = 0
    if (!await resource.load() || query !== next) return false
    selectedSkuId.value = (next.anchorId && skus.value.find(item => item.id === next.anchorId)?.id) || skus.value[0]?.id || 0
    return true
  }

  function close() {
    query = null
    selectedSkuId.value = 0
    resource.reset()
  }

  function open(planId: number, operation: CatalogOperation = 'purchase', anchorId = 0) {
    close()
    return load({ planId, operation, offset: 0, limit: 25, anchorId: anchorId || undefined })
  }

  function changePage(page: { offset: number; limit: number }) {
    if (!query) return Promise.resolve(false)
    return load({ ...query, ...page, anchorId: undefined })
  }

  function retry() { return query ? load({ ...query }) : Promise.resolve(false) }

  return { plan, skus, total, offset, limit, selectedSkuId, selectedSku, loading: resource.loading, error: resource.error, open, close, changePage, retry }
}
