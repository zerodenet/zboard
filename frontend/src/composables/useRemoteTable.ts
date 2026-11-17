import { computed, type Ref } from 'vue'
import { useRemoteResource } from './useRemoteResource'

export interface RemoteTablePage<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  items: T[]
  total: number
  aggregates?: A
  facets?: F
}

export interface RemoteTableFetchContext {
  signal: AbortSignal
}

export interface RemoteTableOptions<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  offset: Ref<number>
  limit: Ref<number>
  fetchPage: (context: RemoteTableFetchContext) => Promise<RemoteTablePage<T, A, F>>
  errorMessage: string | ((error: unknown) => string)
  onPageLoaded?: (page: RemoteTablePage<T, A, F>) => void | Promise<void>
  onOffsetCorrected?: () => void | Promise<void>
}

export function useRemoteTable<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
>(options: RemoteTableOptions<T, A, F>) {
  type Page = RemoteTablePage<T, A, F>
  const resource = useRemoteResource<Page>({
    initial: () => ({ items: [], total: 0, aggregates: {} as A, facets: {} as F }),
    errorMessage: options.errorMessage,
    fetch: async ({ signal }) => {
      // Correct pagination inside the same cancellable, bounded read. A newer
      // filter/route cancels this continuation before it can publish old rows.
      for (let correction = 0; ; correction++) {
        const page = await options.fetchPage({ signal })
        if (signal.aborted) throw new Error('read cancelled')
        if (options.offset.value >= page.total && options.offset.value > 0) {
          if (correction >= 3) throw new Error('pagination changed repeatedly')
          options.offset.value = Math.max(0, Math.floor(Math.max(0, page.total - 1) / options.limit.value) * options.limit.value)
          await options.onOffsetCorrected?.()
          if (signal.aborted) throw new Error('read cancelled')
          continue
        }
        if (options.onPageLoaded) await options.onPageLoaded(page)
        return { ...page, aggregates: (page.aggregates || {}) as A, facets: (page.facets || {}) as F }
      }
    },
  })
  // Keep the existing writable refs for list owners, while all read lifecycle,
  // timeout, authorization and stale-data semantics use the shared resource.
  const items = computed({ get: () => resource.data.value.items, set: (items: T[]) => { resource.data.value = { ...resource.data.value, items } } })
  const total = computed({ get: () => resource.data.value.total, set: (total: number) => { resource.data.value = { ...resource.data.value, total } } })
  const aggregates = computed({ get: () => resource.data.value.aggregates as A, set: (aggregates: A) => { resource.data.value = { ...resource.data.value, aggregates } } })
  const facets = computed({ get: () => resource.data.value.facets as F, set: (facets: F) => { resource.data.value = { ...resource.data.value, facets } } })
  const initialLoading = computed(() => resource.loading.value && !resource.loaded.value)
  return { ...resource, items, total, aggregates, facets, initialLoading, hasLoaded: resource.loaded }
}
