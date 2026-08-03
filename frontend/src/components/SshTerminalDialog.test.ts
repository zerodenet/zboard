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

  it("keeps xterm's measured host free of visual padding and borders", () => {
    expect(source).toContain('.terminal-stage{box-sizing:border-box;position:relative;')
    expect(source).toContain('overflow:hidden;padding:10px;border:1px solid var(--terminal-border)')
    expect(source).toContain('.terminal-host{width:100%;height:100%;min-height:0;overflow:hidden;background:var(--terminal-bg)}')
    expect(source).not.toContain('.terminal-host{box-sizing:border-box')
    expect(source).not.toContain('.terminal-host{width:100%;height:100%;min-height:0;overflow:hidden;padding:')
  })
})
