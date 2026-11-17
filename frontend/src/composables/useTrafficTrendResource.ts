import { computed, shallowRef, watch } from 'vue'
import type { TrafficTrendResult, TrafficTrendScope, TrafficTrendSnapshot } from '../api/readModels'
import { useRemoteResource } from './useRemoteResource'

/** Keep a complete chart snapshot separate from an accepted pending response. */
export function useTrafficTrendResource(options: {
  initial: () => TrafficTrendSnapshot
  fetch: (context: { signal: AbortSignal }) => Promise<TrafficTrendResult>
  scope: () => string
  errorMessage: string
}) {
  const snapshot = shallowRef<TrafficTrendSnapshot | null>(null)
  const resource = useRemoteResource<TrafficTrendResult | null>({
    initial: () => null, fetch: options.fetch, errorMessage: options.errorMessage,
  })
  const serverScope = (scope?: TrafficTrendScope) => scope && JSON.stringify([
    scope.administrative, scope.user_id, scope.subscription_id, scope.node_id,
    scope.protocol_endpoint_id, scope.time_zone,
  ])
  // This watcher only observes results committed by useRemoteResource's latest
  // request guard. A late transport response cannot replace the chart snapshot.
  watch(resource.data, result => {
    if (!result) snapshot.value = null
    else if (!result.projection_pending) snapshot.value = result
    else if (snapshot.value && serverScope(result.scope) !== serverScope(snapshot.value.scope)) snapshot.value = null
  }, { flush: 'sync' })

  function reset() {
    snapshot.value = null
    resource.reset()
  }
  // Invalidate before a new identity, permission, filter, or calendar can render
  // an old curve. The page's normal keyed reader performs the replacement read.
  watch(options.scope, reset, { flush: 'sync' })

  return {
    ...resource,
    reset,
    data: computed(() => snapshot.value || options.initial()),
    snapshotAvailable: computed(() => snapshot.value !== null),
    projectionPending: computed(() => !resource.error.value && resource.data.value?.projection_pending === true),
  }
}
