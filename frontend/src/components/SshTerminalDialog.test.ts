import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(join(import.meta.dirname, 'SshTerminalDialog.vue'), 'utf8')

describe('SshTerminalDialog layout', () => {
  it('keeps the non-fullscreen dialog and xterm history independently scrollable', () => {
    expect(source).not.toContain('    fixed-body\n')
    expect(source).toContain('.terminal-workspace{height:clamp(320px,52vh,560px)')
    expect(source).toContain('.xterm-viewport){overflow-y:scroll!important;overscroll-behavior:contain;touch-action:pan-y;scrollbar-gutter:stable')
  })
})
