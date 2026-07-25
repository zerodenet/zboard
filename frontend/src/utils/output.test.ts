import { describe, expect, it } from 'vitest'
import { normalizeOutput, truncateOutput } from './output'

describe('output normalization', () => {
  it('removes terminal escapes, normalizes line endings and replaces controls', () => {
    expect(normalizeOutput('  \u001b[31m失败\u001b[0m\r\n下一行\u0001  ')).toBe('失败\n下一行�')
  })

  it('normalizes unicode and truncates only the displayed value', () => {
    expect(normalizeOutput('e\u0301')).toBe('é')
    expect(truncateOutput('abcdef', 4)).toBe('abcd\n…')
    expect(truncateOutput('abcd', 4)).toBe('abcd')
  })
})
