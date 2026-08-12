<template>
  <section class="panel traffic-observability" aria-labelledby="traffic-observability-title">
    <header class="panel-header observability-header">
      <div>
        <h2 id="traffic-observability-title">使用趋势</h2>
        <p>按时间查看计费、上下行和连接峰值；移动指针可查看对应时间点详情。</p>
      </div>
      <div class="observability-status">
        <span v-if="recordCount">已汇总 {{ recordCount.toLocaleString('zh-CN') }} 条原始记录</span>
        <span v-if="truncated" class="observability-warning">当前范围数据已截断</span>
      </div>
    </header>

    <div v-if="loading" class="observability-loading" aria-live="polite">
      <UiIcon name="refresh" />
      正在加载趋势数据…
    </div>

    <div v-else-if="!points.length" class="observability-empty">
      <UiIcon name="activity" />
      <div>
        <strong>当前范围没有可绘制的数据</strong>
        <p>节点产生计费记录后，这里会显示流量和最高连接数趋势。</p>
      </div>
    </div>

    <div v-else class="observability-grid">
      <article class="observability-chart-card traffic-card">
        <div class="chart-heading">
          <div>
            <span class="chart-eyebrow">TRAFFIC</span>
            <h3>流量使用</h3>
          </div>
          <div class="chart-legend" aria-label="流量图例">
            <button
              type="button"
              class="legend-control used"
              :class="{ muted: !seriesVisibility.used }"
              :aria-pressed="seriesVisibility.used"
              @click="seriesVisibility.used = !seriesVisibility.used"
            >
              <i class="legend-line used" />计费
            </button>
            <button
              type="button"
              class="legend-control upload"
              :class="{ muted: !seriesVisibility.upload }"
              :aria-pressed="seriesVisibility.upload"
              @click="seriesVisibility.upload = !seriesVisibility.upload"
            >
              <i class="legend-line upload" />上行
            </button>
            <button
              type="button"
              class="legend-control download"
              :class="{ muted: !seriesVisibility.download }"
              :aria-pressed="seriesVisibility.download"
              @click="seriesVisibility.download = !seriesVisibility.download"
            >
              <i class="legend-line download" />下行
            </button>
          </div>
        </div>

        <div class="chart-stage">
          <svg
            class="traffic-chart"
            :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
            role="img"
            aria-label="按日期汇总的计费、上行和下行流量趋势"
            tabindex="0"
            @pointermove="onTrafficPointer"
            @pointerleave="trafficHoverIndex = null"
            @focus="ensureTrafficKeyboardPoint"
            @keydown.left.prevent="moveTrafficKeyboardPoint(-1)"
            @keydown.right.prevent="moveTrafficKeyboardPoint(1)"
          >
            <defs>
              <linearGradient id="traffic-used-gradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--primary)" stop-opacity="0.22" />
                <stop offset="100%" stop-color="var(--primary)" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <g class="chart-grid">
              <template v-for="tick in trafficTicks" :key="tick.value">
                <line :x1="plotLeft" :x2="plotRight" :y1="tick.y" :y2="tick.y" />
                <text :x="plotLeft - 10" :y="tick.y + 4" text-anchor="end">{{ formatBytes(tick.value) }}</text>
              </template>
            </g>

            <path v-if="seriesVisibility.used && usedAreaPath" class="traffic-area used" :d="usedAreaPath" />
            <path v-if="seriesVisibility.used && usedPath" class="traffic-line used" :d="usedPath" />
            <path v-if="seriesVisibility.upload && uploadPath" class="traffic-line upload" :d="uploadPath" />
            <path v-if="seriesVisibility.download && downloadPath" class="traffic-line download" :d="downloadPath" />

            <g class="chart-axis-labels">
              <text
                v-for="label in xLabels"
                :key="label.date"
                :x="label.x"
                :y="chartHeight - 8"
                text-anchor="middle"
              >{{ label.label }}</text>
            </g>

            <g v-if="trafficHoverPoint" class="chart-hover-layer" aria-hidden="true">
              <line class="hover-crosshair" :x1="trafficHoverPoint.x" :x2="trafficHoverPoint.x" :y1="plotTop" :y2="plotBottom" />
              <circle v-if="seriesVisibility.used" class="hover-dot used" :cx="trafficHoverPoint.x" :cy="pointY(trafficHoverPoint.point.used_bytes, trafficMax)" r="5" />
              <circle v-if="seriesVisibility.upload" class="hover-dot upload" :cx="trafficHoverPoint.x" :cy="pointY(trafficHoverPoint.point.upload_bytes, trafficMax)" r="4" />
              <circle v-if="seriesVisibility.download" class="hover-dot download" :cx="trafficHoverPoint.x" :cy="pointY(trafficHoverPoint.point.download_bytes, trafficMax)" r="4" />
            </g>
          </svg>

          <div
            v-if="trafficHoverPoint"
            class="chart-tooltip"
            :class="tooltipSideClass(trafficHoverPoint.index)"
            :style="{ left: `${trafficHoverPoint.leftPercent}%` }"
          >
            <strong>{{ trafficHoverPoint.point.label }}</strong>
            <div class="tooltip-row"><span><i class="tooltip-key used" />计费</span><b>{{ formatBytes(trafficHoverPoint.point.used_bytes) }}</b></div>
            <div class="tooltip-row"><span><i class="tooltip-key upload" />上行</span><b>{{ formatBytes(trafficHoverPoint.point.upload_bytes) }}</b></div>
            <div class="tooltip-row"><span><i class="tooltip-key download" />下行</span><b>{{ formatBytes(trafficHoverPoint.point.download_bytes) }}</b></div>
            <div class="tooltip-meta">{{ trafficHoverPoint.point.record_count.toLocaleString('zh-CN') }} 条计费记录</div>
          </div>
        </div>
      </article>

      <article class="observability-chart-card connection-card">
        <div class="chart-heading">
          <div>
            <span class="chart-eyebrow">CONNECTIONS</span>
            <h3>最高连接数</h3>
          </div>
          <div class="connection-summary">
            <span>区间峰值</span>
            <strong>{{ peakConnections === null ? '—' : peakConnections.toLocaleString('zh-CN') }}</strong>
          </div>
        </div>

        <div v-if="hasConnectionSamples" class="chart-stage">
          <svg
            class="connection-chart"
            :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
            role="img"
            aria-label="按日期统计的最高并发连接数"
            tabindex="0"
            @pointermove="onConnectionPointer"
            @pointerleave="connectionHoverIndex = null"
            @focus="ensureConnectionKeyboardPoint"
            @keydown.left.prevent="moveConnectionKeyboardPoint(-1)"
            @keydown.right.prevent="moveConnectionKeyboardPoint(1)"
          >
            <defs>
              <linearGradient id="connection-gradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--primary)" stop-opacity="0.18" />
                <stop offset="100%" stop-color="var(--primary)" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <g class="chart-grid">
              <template v-for="tick in connectionTicks" :key="tick.value">
                <line :x1="plotLeft" :x2="plotRight" :y1="tick.y" :y2="tick.y" />
                <text :x="plotLeft - 10" :y="tick.y + 4" text-anchor="end">{{ tick.value.toLocaleString('zh-CN') }}</text>
              </template>
            </g>

            <path v-if="connectionAreaPath" class="connection-area" :d="connectionAreaPath" />
            <path v-if="connectionPath" class="connection-line" :d="connectionPath" />

            <g class="chart-axis-labels">
              <text
                v-for="label in xLabels"
                :key="label.date"
                :x="label.x"
                :y="chartHeight - 8"
                text-anchor="middle"
              >{{ label.label }}</text>
            </g>

            <g v-if="connectionHoverPoint" class="chart-hover-layer" aria-hidden="true">
              <line class="hover-crosshair" :x1="connectionHoverPoint.x" :x2="connectionHoverPoint.x" :y1="plotTop" :y2="plotBottom" />
              <circle class="hover-dot connection" :cx="connectionHoverPoint.x" :cy="connectionHoverPoint.y" r="5" />
            </g>
          </svg>

          <div
            v-if="connectionHoverPoint"
            class="chart-tooltip compact"
            :class="tooltipSideClass(connectionHoverPoint.index)"
            :style="{ left: `${connectionHoverPoint.leftPercent}%` }"
          >
            <strong>{{ connectionHoverPoint.label }}</strong>
            <div class="tooltip-row"><span>最高连接数</span><b>{{ connectionHoverPoint.value.toLocaleString('zh-CN') }}</b></div>
          </div>
        </div>

        <div v-else class="connection-unavailable">
          <UiIcon name="activity" />
          <div>
            <strong>连接数暂未采集</strong>
            <p>当前上报链路还没有用户级峰值连接数。字段已经预留，后续接入后会直接按采样最大值展示。</p>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import UiIcon from '../../components/UiIcon.vue'
import { formatBytes } from '../../utils/format'
import type { TrafficObservabilityPoint } from './trafficObservability'

const props = withDefaults(defineProps<{
  points: TrafficObservabilityPoint[]
  loading?: boolean
  truncated?: boolean
  recordCount?: number
  peakConnections?: number | null
}>(), {
  loading: false,
  truncated: false,
  recordCount: 0,
  peakConnections: null,
})

const chartWidth = 720
const chartHeight = 238
const plotLeft = 68
const plotRight = 704
const plotTop = 18
const plotBottom = 198
const plotWidth = plotRight - plotLeft
const plotHeight = plotBottom - plotTop

const trafficHoverIndex = ref<number | null>(null)
const connectionHoverIndex = ref<number | null>(null)
const seriesVisibility = reactive({ used: true, upload: true, download: true })

type NumericPoint = {
  date: string
  label: string
  value: number
  index: number
  x: number
  y: number
}

const trafficMax = computed(() => Math.max(1, ...props.points.flatMap(point => [
  seriesVisibility.used ? point.used_bytes : 0,
  seriesVisibility.upload ? point.upload_bytes : 0,
  seriesVisibility.download ? point.download_bytes : 0,
])))

const connectionMax = computed(() => Math.max(1, ...props.points.map(point => point.peak_connections ?? 0)))
const hasConnectionSamples = computed(() => props.points.some(point => point.peak_connections !== null))

function pointX(index: number) {
  if (props.points.length <= 1) return plotLeft + plotWidth / 2
  return plotLeft + (index / (props.points.length - 1)) * plotWidth
}

function pointY(value: number, maximum: number) {
  return plotBottom - (value / Math.max(1, maximum)) * plotHeight
}

function numericPoints(key: 'used_bytes' | 'upload_bytes' | 'download_bytes'): NumericPoint[] {
  return props.points.map((point, index) => ({
    date: point.date,
    label: point.label,
    value: point[key],
    index,
    x: pointX(index),
    y: pointY(point[key], trafficMax.value),
  }))
}

function linePath(points: NumericPoint[]) {
  return points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(' ')
}

function areaPath(points: NumericPoint[]) {
  if (!points.length) return ''
  const first = points[0]
  const last = points[points.length - 1]
  return `${linePath(points)} L ${last.x.toFixed(2)} ${plotBottom} L ${first.x.toFixed(2)} ${plotBottom} Z`
}

const usedPoints = computed(() => numericPoints('used_bytes'))
const uploadPoints = computed(() => numericPoints('upload_bytes'))
const downloadPoints = computed(() => numericPoints('download_bytes'))
const usedPath = computed(() => linePath(usedPoints.value))
const uploadPath = computed(() => linePath(uploadPoints.value))
const downloadPath = computed(() => linePath(downloadPoints.value))
const usedAreaPath = computed(() => areaPath(usedPoints.value))

const trafficTicks = computed(() => [1, 0.5, 0].map(ratio => ({
  value: trafficMax.value * ratio,
  y: plotBottom - plotHeight * ratio,
})))

const connectionTicks = computed(() => [1, 0.5, 0].map(ratio => ({
  value: Math.round(connectionMax.value * ratio),
  y: plotBottom - plotHeight * ratio,
})))

const connectionPoints = computed<NumericPoint[]>(() => props.points.flatMap((point, index) => {
  if (point.peak_connections === null) return []
  return [{
    date: point.date,
    label: point.label,
    value: point.peak_connections,
    index,
    x: pointX(index),
    y: pointY(point.peak_connections, connectionMax.value),
  }]
}))

const connectionPath = computed(() => linePath(connectionPoints.value))
const connectionAreaPath = computed(() => areaPath(connectionPoints.value))

const xLabels = computed(() => {
  const maximumLabels = 7
  const step = Math.max(1, Math.ceil(props.points.length / maximumLabels))
  return props.points.flatMap((point, index) => {
    const shouldShow = index === 0 || index === props.points.length - 1 || index % step === 0
    return shouldShow ? [{ date: point.date, label: point.label, x: pointX(index) }] : []
  })
})

function pointerIndex(event: PointerEvent, count: number) {
  if (!count) return null
  const svg = event.currentTarget as SVGSVGElement | null
  if (!svg) return null
  const rect = svg.getBoundingClientRect()
  if (!rect.width) return null
  const svgX = ((event.clientX - rect.left) / rect.width) * chartWidth
  const ratio = Math.max(0, Math.min(1, (svgX - plotLeft) / plotWidth))
  return Math.max(0, Math.min(count - 1, Math.round(ratio * Math.max(0, count - 1))))
}

function onTrafficPointer(event: PointerEvent) {
  trafficHoverIndex.value = pointerIndex(event, props.points.length)
}

function onConnectionPointer(event: PointerEvent) {
  const index = pointerIndex(event, props.points.length)
  if (index === null) {
    connectionHoverIndex.value = null
    return
  }
  let bestIndex: number | null = null
  let bestDistance = Number.POSITIVE_INFINITY
  for (const point of connectionPoints.value) {
    const distance = Math.abs(point.index - index)
    if (distance < bestDistance) {
      bestDistance = distance
      bestIndex = point.index
    }
  }
  connectionHoverIndex.value = bestIndex
}

function ensureTrafficKeyboardPoint() {
  if (trafficHoverIndex.value === null && props.points.length) trafficHoverIndex.value = props.points.length - 1
}

function moveTrafficKeyboardPoint(direction: number) {
  ensureTrafficKeyboardPoint()
  if (trafficHoverIndex.value === null) return
  trafficHoverIndex.value = Math.max(0, Math.min(props.points.length - 1, trafficHoverIndex.value + direction))
}

function ensureConnectionKeyboardPoint() {
  if (connectionHoverIndex.value !== null || !connectionPoints.value.length) return
  connectionHoverIndex.value = connectionPoints.value[connectionPoints.value.length - 1].index
}

function moveConnectionKeyboardPoint(direction: number) {
  ensureConnectionKeyboardPoint()
  if (connectionHoverIndex.value === null) return
  const samples = connectionPoints.value
  const currentPosition = samples.findIndex(point => point.index === connectionHoverIndex.value)
  const nextPosition = Math.max(0, Math.min(samples.length - 1, (currentPosition < 0 ? samples.length - 1 : currentPosition) + direction))
  connectionHoverIndex.value = samples[nextPosition].index
}

const trafficHoverPoint = computed(() => {
  const index = trafficHoverIndex.value
  if (index === null || !props.points[index]) return null
  const x = pointX(index)
  return {
    index,
    x,
    leftPercent: (x / chartWidth) * 100,
    point: props.points[index],
  }
})

const connectionHoverPoint = computed(() => {
  const index = connectionHoverIndex.value
  if (index === null) return null
  const point = connectionPoints.value.find(item => item.index === index)
  if (!point) return null
  return {
    ...point,
    leftPercent: (point.x / chartWidth) * 100,
  }
})

function tooltipSideClass(index: number) {
  if (props.points.length <= 1) return 'centered'
  const ratio = index / (props.points.length - 1)
  if (ratio < 0.2) return 'from-left'
  if (ratio > 0.8) return 'from-right'
  return 'centered'
}
</script>

<style scoped>
.traffic-observability {
  overflow: hidden;
}

.observability-header {
  align-items: flex-start;
}

.observability-status {
  display: grid;
  justify-items: end;
  gap: 4px;
  color: var(--muted);
  font-size: 11px;
}

.observability-warning {
  color: var(--warning);
  font-weight: 650;
}

.observability-loading,
.observability-empty,
.connection-unavailable {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  min-height: 220px;
  padding: 28px;
  color: var(--muted);
  background: var(--surface-soft);
}

.observability-loading .ui-icon {
  animation: observability-spin 1s linear infinite;
}

.observability-empty strong,
.connection-unavailable strong {
  display: block;
  margin-bottom: 5px;
  color: var(--text-strong);
}

.observability-empty p,
.connection-unavailable p {
  max-width: 520px;
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}

.observability-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(320px, .8fr);
}

.observability-chart-card {
  min-width: 0;
  padding: 18px 20px 16px;
}

.observability-chart-card + .observability-chart-card {
  border-left: 1px solid var(--line);
}

.chart-heading {
  min-height: 44px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.chart-heading h3 {
  margin: 2px 0 0;
  font-size: 14px;
}

.chart-eyebrow {
  color: var(--primary);
  font-size: 9px;
  font-weight: 750;
  letter-spacing: .12em;
}

.chart-legend {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
}

.legend-control {
  min-height: 28px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 10px;
  cursor: pointer;
  transition: opacity .16s ease, border-color .16s ease, background .16s ease;
}

.legend-control:hover,
.legend-control:focus-visible {
  border-color: var(--primary);
  background: var(--surface-soft);
  outline: none;
}

.legend-control.muted {
  opacity: .38;
}

.legend-line {
  width: 14px;
  height: 2px;
  display: inline-block;
  border-radius: 999px;
}

.legend-line.used,
.tooltip-key.used {
  background: var(--primary);
}

.legend-line.upload,
.tooltip-key.upload {
  background: var(--success);
}

.legend-line.download,
.tooltip-key.download {
  background: var(--warning);
}

.chart-stage {
  position: relative;
  width: 100%;
  overflow-x: auto;
}

.traffic-chart,
.connection-chart {
  width: 100%;
  min-width: 520px;
  height: auto;
  display: block;
  touch-action: pan-y;
  cursor: crosshair;
  outline: none;
}

.traffic-chart:focus-visible,
.connection-chart:focus-visible {
  box-shadow: inset 0 0 0 1px var(--primary);
}

.chart-grid line {
  stroke: var(--line-soft);
  stroke-width: 1;
}

.chart-grid text,
.chart-axis-labels text {
  fill: var(--muted);
  font-size: 10px;
}

.traffic-area.used {
  fill: url(#traffic-used-gradient);
}

.traffic-line,
.connection-line {
  fill: none;
  stroke-width: 2.2;
  stroke-linecap: round;
  stroke-linejoin: round;
  vector-effect: non-scaling-stroke;
}

.traffic-line.used {
  stroke: var(--primary);
  stroke-width: 2.6;
}

.traffic-line.upload {
  stroke: var(--success);
}

.traffic-line.download {
  stroke: var(--warning);
}

.connection-area {
  fill: url(#connection-gradient);
}

.connection-line {
  stroke: var(--primary);
  stroke-width: 2.4;
}

.hover-crosshair {
  stroke: var(--muted);
  stroke-width: 1;
  stroke-dasharray: 3 4;
  opacity: .55;
  vector-effect: non-scaling-stroke;
}

.hover-dot {
  fill: var(--surface);
  stroke-width: 2.5;
  vector-effect: non-scaling-stroke;
}

.hover-dot.used,
.hover-dot.connection {
  stroke: var(--primary);
}

.hover-dot.upload {
  stroke: var(--success);
}

.hover-dot.download {
  stroke: var(--warning);
}

.chart-tooltip {
  position: absolute;
  top: 12px;
  z-index: 3;
  width: 184px;
  padding: 11px 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow-lg);
  pointer-events: none;
}

.chart-tooltip.centered {
  transform: translateX(-50%);
}

.chart-tooltip.from-left {
  transform: translateX(-10%);
}

.chart-tooltip.from-right {
  transform: translateX(-90%);
}

.chart-tooltip.compact {
  width: 160px;
}

.chart-tooltip > strong {
  display: block;
  margin-bottom: 8px;
  color: var(--text-strong);
  font-size: 12px;
}

.tooltip-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 3px 0;
  color: var(--muted);
  font-size: 11px;
}

.tooltip-row span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.tooltip-row b {
  color: var(--text-strong);
  font-weight: 650;
}

.tooltip-key {
  width: 7px;
  height: 7px;
  border-radius: 999px;
}

.tooltip-meta {
  margin-top: 7px;
  padding-top: 7px;
  border-top: 1px solid var(--line-soft);
  color: var(--muted);
  font-size: 10px;
}

.connection-summary {
  display: grid;
  justify-items: end;
  gap: 2px;
  color: var(--muted);
  font-size: 10px;
}

.connection-summary strong {
  color: var(--text-strong);
  font-size: 20px;
  line-height: 1.1;
}

@keyframes observability-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 1050px) {
  .observability-grid {
    grid-template-columns: 1fr;
  }

  .observability-chart-card + .observability-chart-card {
    border-left: 0;
    border-top: 1px solid var(--line);
  }
}

@media (max-width: 720px) {
  .observability-header,
  .chart-heading {
    align-items: stretch;
  }

  .observability-status {
    justify-items: start;
  }

  .chart-heading {
    flex-direction: column;
  }

  .chart-legend {
    justify-content: flex-start;
  }

  .connection-summary {
    justify-items: start;
  }
}
</style>
