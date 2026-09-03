import { computed } from 'vue'
import { fetchTrafficUsagePage, fetchTrafficUsageStatistics, type TrafficUsagePage, type TrafficUsageQuery,
  type TrafficUsageStatistics, type TrafficUsageReferences, type TrafficUsageAggregates } from '../api/trafficUsage'
import { keyedLoad } from './keyedLoad'
import { useRemoteResource } from './useRemoteResource'

/** Live cursor pages and an explicitly dated whole-range statistics snapshot. */
export function useTrafficUsageTable(query: () => TrafficUsageQuery, admin = false) {
  const page = useRemoteResource<TrafficUsagePage | null>({
    initial: () => null,
    fetch: ({ signal }) => fetchTrafficUsagePage({ ...query(), includeTotals: false }, admin, { signal }),
    errorMessage: '流量记录加载失败。',
  })
  function statisticsQuery() {
    const { cursor, limit, includeTotals, ...scope } = query()
    return scope
  }
  const statistics = useRemoteResource<TrafficUsageStatistics | null>({
    initial: () => null,
    fetch: ({ signal }) => fetchTrafficUsageStatistics(statisticsQuery(), admin, { signal }),
    errorMessage: '流量区间统计加载失败。',
  })
  const statisticsReady = computed(() => Boolean(statistics.data.value && !statistics.error.value))
  return {
    load: page.load, reset: page.reset,
    items: computed(() => page.data.value?.items || []),
    facets: computed(() => page.data.value?.facets || {} as TrafficUsageReferences),
    hasLoaded: page.loaded, loading: page.loading, error: page.error,
    refreshing: computed(() => page.loaded.value && page.loading.value),
    nextCursor: computed(() => page.data.value?.page.next_cursor || null),
    previousCursor: computed(() => page.data.value?.page.previous_cursor || null),
    total: computed(() => statisticsReady.value ? statistics.data.value!.total : undefined),
    aggregates: computed(() => statisticsReady.value ? statistics.data.value!.aggregates : {} as TrafficUsageAggregates),
    statistics, statisticsReady,
    loadStatistics: keyedLoad(() => JSON.stringify(statisticsQuery()), statistics),
  }
}
