import { computed } from 'vue'
import { useRemoteResource } from './useRemoteResource'

export interface CursorTablePage<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  items: T[]
  total: number
  page: {
    next_cursor: string | null
    previous_cursor: string | null
  }
  aggregates?: A
  facets?: F
}

export interface CursorTableFetchContext {
  signal: AbortSignal
}

export interface CursorTableOptions<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  fetchPage: (context: CursorTableFetchContext) => Promise<CursorTablePage<T, A, F>>
  errorMessage: string | ((error: unknown) => string)
  onPageLoaded?: (page: CursorTablePage<T, A, F>) => void | Promise<void>
}

export function useCursorTable<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
>(options: CursorTableOptions<T, A, F>) {
  type Page = CursorTablePage<T, A, F>
  const resource = useRemoteResource<Page>({
    initial: () => ({ items: [], total: 0, page: { next_cursor: null, previous_cursor: null }, aggregates: {} as A, facets: {} as F }),
    errorMessage: options.errorMessage,
    fetch: async ({ signal }) => {
      const page = await options.fetchPage({ signal })
      if (signal.aborted) throw new Error('read cancelled')
      if (options.onPageLoaded) await options.onPageLoaded(page)
      return { ...page, aggregates: (page.aggregates || {}) as A, facets: (page.facets || {}) as F }
    },
  })
  const items = computed({ get: () => resource.data.value.items, set: (items: T[]) => { resource.data.value = { ...resource.data.value, items } } })
  const total = computed({ get: () => resource.data.value.total, set: (total: number) => { resource.data.value = { ...resource.data.value, total } } })
  const aggregates = computed({ get: () => resource.data.value.aggregates as A, set: (aggregates: A) => { resource.data.value = { ...resource.data.value, aggregates } } })
  const facets = computed({ get: () => resource.data.value.facets as F, set: (facets: F) => { resource.data.value = { ...resource.data.value, facets } } })
  const nextCursor = computed({ get: () => resource.data.value.page.next_cursor, set: (next_cursor: string | null) => { resource.data.value = { ...resource.data.value, page: { ...resource.data.value.page, next_cursor } } } })
  const previousCursor = computed({ get: () => resource.data.value.page.previous_cursor, set: (previous_cursor: string | null) => { resource.data.value = { ...resource.data.value, page: { ...resource.data.value.page, previous_cursor } } } })
  const initialLoading = computed(() => resource.loading.value && !resource.loaded.value)
  return { ...resource, items, total, aggregates, facets, nextCursor, previousCursor, initialLoading, hasLoaded: resource.loaded }
}
