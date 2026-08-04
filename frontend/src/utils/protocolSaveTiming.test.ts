import { describe, expect, it } from 'vitest'
import { formatProtocolSaveTiming, summarizeProtocolSaveTiming } from './protocolSaveTiming'

describe('protocol save timing', () => {
  it('separates server work, approximate network time and list refresh', () => {
    const summary = summarizeProtocolSaveTiming({
      validation_ms: 12,
      transaction_ms: 20,
      task_enqueue_ms: 7,
      response_preparation_ms: 5,
      server_total_ms: 44,
    }, 61.4, 18.6, 81.2)

    expect(summary).toEqual({
      validation_ms: 12,
      transaction_ms: 20,
      task_enqueue_ms: 7,
      response_preparation_ms: 5,
      server_total_ms: 44,
      request_ms: 61,
      network_ms: 17,
      refresh_ms: 19,
      total_ms: 81,
    })
    expect(formatProtocolSaveTiming(summary)).toContain('事务 20ms')
    expect(formatProtocolSaveTiming(summary)).toContain('列表刷新 19ms')
  })

  it('normalizes missing or invalid timing values', () => {
    expect(summarizeProtocolSaveTiming(undefined, 10, Number.NaN, 12)).toMatchObject({
      server_total_ms: 0,
      request_ms: 10,
      network_ms: 10,
      refresh_ms: 0,
      total_ms: 12,
    })
  })
})
