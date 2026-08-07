import { describe, expect, it } from 'vitest'
import { commerceErrorCode, commerceErrorMessage } from './commerceErrors'

describe('commerce error presentation', () => {
  it('maps subscription capacity to a stable user-facing message', () => {
    const cause = {
      response: {
        data: {
          message: 'plan subscription capacity is exhausted',
          error: { version: 1, code: 'plan_subscription_limit_reached' },
        },
      },
    }
    expect(commerceErrorCode(cause)).toBe('plan_subscription_limit_reached')
    expect(commerceErrorMessage(cause, '订单操作失败。')).toContain('有效订阅数量已达到上限')
  })

  it('does not surface raw SQL when a safe fallback is available', () => {
    const cause = {
      response: {
        data: {
          message: "Error 1062: Duplicate entry 'starter' for key 'uni_plans_slug'",
        },
      },
    }
    expect(commerceErrorMessage(cause, '商品保存失败。')).toBe('商品保存失败。')
  })
})
