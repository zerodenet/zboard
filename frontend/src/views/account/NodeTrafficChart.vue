<template>
  <section class="panel node-traffic" aria-labelledby="node-traffic-title">
    <header class="panel-header node-traffic-header">
      <div>
        <h2 id="node-traffic-title">节点流量分布</h2>
        <p>按时间查看所选范围内每个实际使用节点产生的计费流量。</p>
      </div>
      <span v-if="data" class="bucket-label">{{ data.bucket === 'minute' ? '分钟' : '小时' }}粒度</span>
    </header>

    <div v-if="loading" class="chart-state" aria-live="polite">
      <UiIcon name="refresh" />正在加载节点流量…
    </div>
    <div v-else-if="!series.length" class="chart-state empty">
      <UiIcon name="activity" />
      <div><strong>当前范围没有节点流量</strong><p>产生计费记录后，这里会按节点绘制时间趋势。</p></div>
    </div>
    <div v-else class="node-chart-body">
      <div class="node-legend" aria-label="节点图例">
        <span v-for="item in series" :key="item.nodeId" :style="{ '--series-color': item.color }">
          <i />{{ item.label }}
          <b>{{ formatBytes(item.total) }}</b>
        </span>
      </div>

      <div class="chart-stage">
        <svg
          class="node-chart"
          :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
          role="img"
          aria-label="各节点计费流量时间折线图"
          tabindex="0"
          @pointermove="onPointerMove"
          @pointerleave="hoverIndex = null"
          @focus="ensureKeyboardPoint"
          @keydown.left.prevent="moveKeyboardPoint(-1)"
          @keydown.right.prevent="moveKeyboardPoint(1)"
        >
          <g class="chart-grid">
            <template v-for="tick in yTicks" :key="tick.value">
              <line :x1="plotLeft" :x2="plotRight" :y1="tick.y" :y2="tick.y" />
              <text :x="plotLeft - 10" :y="tick.y + 4" text-anchor="end">{{ formatBytes(tick.value) }}</text>
            </template>
          </g>
          <path
            v-for="item in series"
            :key="item.nodeId"
            class="node-line"
            :style="{ stroke: item.color }"
            :d="item.path"
          />
          <g class="axis-labels">
            <text v-for="label in xLabels" :key="label.key" :x="label.x" :y="chartHeight - 8" text-anchor="middle">{{ label.label }}</text>
          </g>
          <g v-if="hoverPoint" class="hover-layer" aria-hidden="true">
            <line :x1="hoverPoint.x" :x2="hoverPoint.x" :y1="plotTop" :y2="plotBottom" />
            <circle
              v-for="item in hoverPoint.values"
              :key="item.nodeId"
              :cx="hoverPoint.x"
              :cy="pointY(item.value)"
              r="4"
              :style="{ fill: item.color }"
            />
          </g>
        </svg>

        <div v-if="hoverPoint" class="chart-tooltip" :class="tooltipSideClass" :style="{ left: `${hoverPoint.leftPercent}%` }">
          <strong>{{ formatPointTime(hoverPoint.key) }}</strong>
          <div v-for="item in hoverPoint.values" :key="item.nodeId" class="tooltip-row">
            <span :style="{ '--series-color': item.color }"><i />{{ item.label }}</span>
            <b>{{ formatBytes(item.value) }}</b>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { TrafficNodeSeries } from '../../api/trafficUsage'
import UiIcon from '../../components/UiIcon.vue'
import { formatBytes } from '../../utils/format'

const props = withDefaults(defineProps<{
  data?: TrafficNodeSeries | null
  loading?: boolean
}>(), {
  data: null,
  loading: false,
})

const chartWidth = 920
const chartHeight = 286
const plotLeft = 76
const plotRight = 902
const plotTop = 20
const plotBottom = 242
const plotWidth = plotRight - plotLeft
const plotHeight = plotBottom - plotTop
const hoverIndex = ref<number | null>(null)
const palette = [
  'var(--primary)',
  'var(--success)',
  'var(--warning)',
  'var(--danger)',
  'var(--info, var(--primary))',
  'var(--muted)',
]

const timeKeys = computed(() => Array.from(new Set((props.data?.points || []).map(point => point.record_at))).sort())
const nodeReferences = computed(() => new Map((props.data?.nodes || []).map(node => [node.id, node])))
const maximum = computed(() => Math.max(1, ...(props.data?.points || []).map(point => point.used_bytes)))

function pointX(index: number) {
  if (timeKeys.value.length <= 1) return plotLeft + plotWidth / 2
  return plotLeft + (index / (timeKeys.value.length - 1)) * plotWidth
}

function pointY(value: number) {
  return plotBottom - (Math.max(0, value) / maximum.value) * plotHeight
}

const series = computed(() => {
  const byNode = new Map<number, Map<string, number>>()
  for (const point of props.data?.points || []) {
    const values = byNode.get(point.node_id) || new Map<string, number>()
    values.set(point.record_at, Number(point.used_bytes) || 0)
    byNode.set(point.node_id, values)
  }
  return Array.from(byNode.entries()).map(([nodeId, values], index) => {
    const reference = nodeReferences.value.get(nodeId)
    const points = timeKeys.value.map((key, pointIndex) => ({
      x: pointX(pointIndex),
      y: pointY(values.get(key) || 0),
    }))
    return {
      nodeId,
      label: reference?.display_name || `节点 #${nodeId}`,
      color: palette[index % palette.length],
      total: Array.from(values.values()).reduce((sum, value) => sum + value, 0),
      values,
      path: points.map((point, pointIndex) => `${pointIndex ? 'L' : 'M'} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(' '),
    }
  }).sort((left, right) => right.total - left.total)
})

const yTicks = computed(() => [1, 0.5, 0].map(ratio => ({
  value: maximum.value * ratio,
  y: plotBottom - plotHeight * ratio,
})))

const xLabels = computed(() => {
  const maxLabels = 7
  const step = Math.max(1, Math.ceil(timeKeys.value.length / maxLabels))
  return timeKeys.value.flatMap((key, index) => {
    if (index !== 0 && index !== timeKeys.value.length - 1 && index % step !== 0) return []
    return [{ key, x: pointX(index), label: formatAxisTime(key) }]
  })
})

const hoverPoint = computed(() => {
  const index = hoverIndex.value
  if (index === null || index < 0 || index >= timeKeys.value.length) return null
  const key = timeKeys.value[index]
  return {
    key,
    x: pointX(index),
    leftPercent: (pointX(index) / chartWidth) * 100,
    values: series.value.map(item => ({
      nodeId: item.nodeId,
      label: item.label,
      color: item.color,
      value: item.values.get(key) || 0,
    })).filter(item => item.value > 0).sort((left, right) => right.value - left.value),
  }
})

const tooltipSideClass = computed(() => {
  const index = hoverIndex.value ?? 0
  return index > timeKeys.value.length * 0.65 ? 'align-right' : index < timeKeys.value.length * 0.25 ? 'align-left' : ''
})

function onPointerMove(event: PointerEvent) {
  if (!timeKeys.value.length) return
  const svg = event.currentTarget as SVGSVGElement
  const rect = svg.getBoundingClientRect()
  if (!rect.width) return
  const svgX = ((event.clientX - rect.left) / rect.width) * chartWidth
  const ratio = Math.max(0, Math.min(1, (svgX - plotLeft) / plotWidth))
  hoverIndex.value = Math.round(ratio * Math.max(0, timeKeys.value.length - 1))
}

function ensureKeyboardPoint() {
  if (hoverIndex.value === null && timeKeys.value.length) hoverIndex.value = timeKeys.value.length - 1
}

function moveKeyboardPoint(offset: number) {
  if (!timeKeys.value.length) return
  hoverIndex.value = Math.max(0, Math.min(timeKeys.value.length - 1, (hoverIndex.value ?? timeKeys.value.length - 1) + offset))
}

function formatAxisTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const sameDayRange = timeKeys.value.length > 0 && new Date(timeKeys.value[0]).toDateString() === new Date(timeKeys.value[timeKeys.value.length - 1]).toDateString()
  return new Intl.DateTimeFormat('zh-CN', sameDayRange
    ? { hour: '2-digit', minute: '2-digit' }
    : { month: '2-digit', day: '2-digit', hour: props.data?.bucket === 'hour' ? '2-digit' : undefined }
  ).format(date)
}

function formatPointTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date)
}
</script>

<style scoped>
.node-traffic { min-width: 0; }.node-traffic-header { align-items: flex-start; }.bucket-label { flex: none; padding: 5px 8px; border: 1px solid var(--line); border-radius: 999px; color: var(--muted); background: var(--surface-2); font-size: 10px; font-weight: 700; }.chart-state { min-height: 220px; display: flex; align-items: center; justify-content: center; gap: 9px; color: var(--muted); font-size: 12px; }.chart-state.empty { text-align: left; }.chart-state.empty strong { display: block; color: var(--text); }.chart-state p { margin: 3px 0 0; }.node-chart-body { display: grid; gap: 10px; padding: 10px 18px 18px; }.node-legend { display: flex; flex-wrap: wrap; gap: 8px 16px; padding: 0 4px; }.node-legend > span { display: inline-flex; align-items: center; gap: 6px; min-width: 0; color: var(--muted); font-size: 10px; }.node-legend i,.tooltip-row i { width: 14px; height: 3px; flex: none; border-radius: 99px; background: var(--series-color); }.node-legend b { color: var(--text); font-weight: 700; }.chart-stage { position: relative; min-width: 0; overflow: hidden; }.node-chart { width: 100%; min-width: 640px; display: block; outline: none; }.chart-grid line { stroke: var(--line); stroke-width: 1; }.chart-grid text,.axis-labels text { fill: var(--muted); font-size: 9px; }.node-line { fill: none; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; vector-effect: non-scaling-stroke; }.hover-layer line { stroke: var(--line-strong, var(--muted)); stroke-width: 1; stroke-dasharray: 4 4; }.chart-tooltip { position: absolute; top: 12px; transform: translateX(-50%); min-width: 190px; max-width: 280px; display: grid; gap: 7px; padding: 10px 11px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); box-shadow: var(--shadow-md); pointer-events: none; z-index: 3; }.chart-tooltip.align-left { transform: translateX(0); }.chart-tooltip.align-right { transform: translateX(-100%); }.chart-tooltip > strong { font-size: 10px; }.tooltip-row { display: flex; align-items: center; justify-content: space-between; gap: 14px; font-size: 10px; }.tooltip-row span { min-width: 0; display: flex; align-items: center; gap: 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); }.tooltip-row b { flex: none; }@media (max-width: 720px) { .chart-stage { overflow-x: auto; }.node-chart-body { padding-inline: 12px; } }
</style>
