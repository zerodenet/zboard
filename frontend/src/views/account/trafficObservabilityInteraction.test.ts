import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const chartSource = readFileSync(fileURLToPath(new URL('./TrafficObservabilityChart.vue', import.meta.url)), 'utf8')

describe('TrafficObservabilityChart interactions', () => {
  it('exposes pointer and keyboard exploration', () => {
    expect(chartSource).toContain('@pointermove="onTrafficPointer"')
    expect(chartSource).toContain('@keydown.left.prevent="moveTrafficKeyboardPoint(-1)"')
    expect(chartSource).toContain('@keydown.right.prevent="moveTrafficKeyboardPoint(1)"')
    expect(chartSource).toContain('hover-crosshair')
    expect(chartSource).toContain('chart-tooltip')
  })

  it('lets users toggle traffic series', () => {
    expect(chartSource).toContain(':aria-pressed="seriesVisibility.used"')
    expect(chartSource).toContain(':aria-pressed="seriesVisibility.upload"')
    expect(chartSource).toContain(':aria-pressed="seriesVisibility.download"')
  })

  it('keeps edge tooltips inside the scrollable chart width', () => {
    expect(chartSource).toContain('.chart-tooltip.from-left {\n  transform: translateX(0);')
    expect(chartSource).toContain('.chart-tooltip.from-right {\n  transform: translateX(-100%);')
    expect(chartSource).toContain('overflow-x: auto;')
    expect(chartSource).not.toContain('transform: translateX(-90%);')
    expect(chartSource).not.toContain('transform: translateX(-10%);')
  })
})
