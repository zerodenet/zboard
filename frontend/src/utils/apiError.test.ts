import { describe, expect, it } from 'vitest'
import { normalizeApiErrorMessage, normalizeApiErrorPayload, normalizeApiFormError, normalizeApiMessage } from './apiError'

describe('normalizeApiFormError', () => {
  it('accepts versioned field errors and maps only declared form fields', () => {
    const result = normalizeApiFormError({ response: { data: {
      message: '用户信息校验失败。',
      error: { version: 1, code: 'validation_failed', fields: { email: '邮箱无效。', ignored: '不应显示。' } },
    } } }, '提交失败。', { email: 'account_email' })

    expect(result).toEqual({
      code: 'validation_failed',
      message: '用户信息校验失败。',
      fields: { account_email: '邮箱无效。' },
    })
  })

  it('keeps legacy and non-localized server errors behind the caller fallback', () => {
    expect(normalizeApiFormError({ response: { data: { message: 'invalid request' } } }, '请求失败。')).toEqual({
      code: '',
      message: '请求失败。',
      fields: {},
    })
  })

  it('normalizes localized response messages and structured fields at the API boundary', () => {
    expect(normalizeApiErrorPayload({
      message: '\u001b[31m保存失败。\u001b[0m\r\n请重试。\u0001',
      error: {
        version: 1,
        code: 'validation_failed',
        fields: {
          email: ' 邮箱\u0001格式错误。 ',
          ignored: { message: 'not a string' },
          'invalid field name': '不应保留。',
        },
      },
      data: { retained: true },
    })).toEqual({
      message: '保存失败。 请重试。',
      error: {
        version: 1,
        code: 'validation_failed',
        fields: { email: '邮箱 格式错误。' },
      },
      data: { retained: true },
    })
  })

  it('does not expose raw strings, non-localized messages or invalid error codes', () => {
    expect(normalizeApiErrorPayload('upstream gateway failed')).toEqual({ message: '' })
    expect(normalizeApiErrorPayload({ message: 'internal server error', error: { code: 'INVALID CODE' } })).toEqual({
      message: '',
      error: { code: '', fields: {} },
    })
    expect(normalizeApiErrorMessage({ response: { data: { message: 'bad request' } } }, '请求失败。')).toBe('请求失败。')
    expect(normalizeApiMessage('\u001b[31m连接失败。\u001b[0m', '连接异常。')).toBe('连接失败。')
  })
})
