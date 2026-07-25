import { describe, expect, it } from 'vitest'
import {
  collectFieldErrors,
  isBlank,
  isCharacterLengthInRange,
  isEmail,
  isHttpUrl,
  isIntegerInRange,
  isOneOf,
  isSlug,
  isUtf8LengthInRange,
  utf8ByteLength,
  unicodeCharacterLength,
} from './validation'

describe('validation primitives', () => {
  it('counts UTF-8 bytes instead of JavaScript code units', () => {
    expect(utf8ByteLength('节点A')).toBe(7)
    expect(isUtf8LengthInRange('节点', 1, 6)).toBe(true)
    expect(isUtf8LengthInRange('节点', 1, 5)).toBe(false)
    expect(unicodeCharacterLength('节点🙂')).toBe(3)
    expect(isCharacterLengthInRange('节点🙂', 3, 3)).toBe(true)
  })

  it('normalizes blank checks and validates shared identifiers', () => {
    expect(isBlank('  ')).toBe(true)
    expect(isEmail('operator@example.com')).toBe(true)
    expect(isEmail('operator@localhost')).toBe(false)
    expect(isHttpUrl('https://panel.example.com/base')).toBe(true)
    expect(isHttpUrl('https://user:secret@panel.example.com/#private')).toBe(false)
    expect(isSlug('clash-yaml', 80)).toBe(true)
    expect(isSlug('Clash YAML', 80)).toBe(false)
  })

  it('validates enums and bounded integers without coercion', () => {
    expect(isOneOf('active', ['active', 'suspended'] as const)).toBe(true)
    expect(isOneOf('unknown', ['active', 'suspended'] as const)).toBe(false)
    expect(isIntegerInRange(22, 1, 65535)).toBe(true)
    expect(isIntegerInRange(22.5, 1, 65535)).toBe(false)
    expect(isIntegerInRange('22', 1, 65535)).toBe(false)
  })

  it('collects only concrete validation messages', () => {
    expect(collectFieldErrors({ name: '请输入名称。', slug: null, code: false, note: '' })).toEqual({ name: '请输入名称。' })
  })
})
