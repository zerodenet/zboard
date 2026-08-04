export interface ProtocolEndpointMutationTiming {
  validation_ms: number
  transaction_ms: number
  task_enqueue_ms: number
  response_preparation_ms: number
  server_total_ms: number
}

export interface ProtocolSaveTimingSummary extends ProtocolEndpointMutationTiming {
  request_ms: number
  network_ms: number
  refresh_ms: number
  total_ms: number
}

function milliseconds(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? Math.round(parsed) : 0
}

export function summarizeProtocolSaveTiming(
  timing: ProtocolEndpointMutationTiming | undefined,
  requestMS: number,
  refreshMS: number,
  totalMS: number,
): ProtocolSaveTimingSummary {
  const serverTotalMS = milliseconds(timing?.server_total_ms)
  const normalizedRequestMS = milliseconds(requestMS)
  return {
    validation_ms: milliseconds(timing?.validation_ms),
    transaction_ms: milliseconds(timing?.transaction_ms),
    task_enqueue_ms: milliseconds(timing?.task_enqueue_ms),
    response_preparation_ms: milliseconds(timing?.response_preparation_ms),
    server_total_ms: serverTotalMS,
    request_ms: normalizedRequestMS,
    network_ms: Math.max(0, normalizedRequestMS - serverTotalMS),
    refresh_ms: milliseconds(refreshMS),
    total_ms: milliseconds(totalMS),
  }
}

export function formatProtocolSaveTiming(summary: ProtocolSaveTimingSummary): string {
  return `耗时 ${summary.total_ms}ms：保存请求 ${summary.request_ms}ms（校验 ${summary.validation_ms}ms、事务 ${summary.transaction_ms}ms、任务入队 ${summary.task_enqueue_ms}ms、响应准备 ${summary.response_preparation_ms}ms、网络及调度约 ${summary.network_ms}ms），列表刷新 ${summary.refresh_ms}ms。`
}
