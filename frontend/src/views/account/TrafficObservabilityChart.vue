<template>
  <section class="panel traffic-observability" aria-labelledby="traffic-observability-title">
    <header class="panel-header">
      <div>
        <h2 id="traffic-observability-title">使用趋势</h2>
        <p>流量按日期汇总；连接数始终取采样窗口内的最高并发值。</p>
      </div>
      <div class="observability-status">
        <span v-if="recordCount">已汇总 {{ recordCount.toLocaleString('zh-CN') }} 条记录</span>
        <span v-if="truncated" class="observability-warning">达到 {{ recordLimit.toLocaleString('zh-CN') }} 条展示上限</span>
      </div>
    </header>

    <div v-if="loading" class="observability-loading" aria-live="polite">
      <UiIcon name="refresh" />
      正在汇总趋势数据…
    </div>

    <div v-else-if="!points.length" class="observability-empty">
      <UiIcon name="activity" />
      <div>
        <strong>当前范围没有可绘制的数据</strong>
        <p>节点产生计费记录后，这里会显示每日流量和最高连接数。</p>
      </div>
    </div>

    <div v-else class="observability-grid">
      <article class="observability-chart-card">
        <div class="chart-heading">
          <div>
            <span class="chart-eyebrow">TRAFFIC</span>
            <h3>每日流量</h3>
          </div>
          <div class="chart-legend" aria-label="流量图例">
            <span><i class="legend-swatch charged" />计费</span>
            <span><i class="legend-line upload" />上行</span>
            <span><i class="legend-line download" />下行</span>
          </div>
        </div>

        <div class="chart-canvas">
          <svg
            class="traffic-chart"
            :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
            role="img"
            aria-label="按日期汇总的计费、上行和下行流量趋势"
          >
            <g class="chart-grid">
              <template v-for="tick in trafficTicks" :key="tick.value">
                <line :x1="plotLeft" :x2="plotRight" :y1="tick.y" :y2="tick.y" />
                <text :x="plotLeft - 10" :y="tick.y + 4" text-anchor="end">{{ formatBytes(tick.value) }}</text>
              </template>
            </g>

            <g class="charged-bars">
              <rect
                v-for="bar in trafficBars"
                :key="bar.date"
                :x="bar.x"
                :y="bar.y"
                :width="bar.width"
                :height="bar.height"
                rx="3"
              >
                <title>{{ bar.label }}：计费 {{ formatBytes(bar.value) }}</title>
              </rect>
            </g>

            <path v-if="uploadPath" class="traffic-line upload" :d="uploadPath" />
            <path v-if="downloadPath" class="traffic-line download" :d="downloadPath" />

            <g class="traffic-points upload">
              <circle
                v-for="point in uploadPoints"
                :key="point.date"
                :cx="point.x"
                :cy="point.y"
                r="3"
              >
                <title>{{ point.label }}：上行 {{ formatBytes(point.value) }}</title>
              </circle>
            </g>
            <g class="traffic-points download">
              <circle
                v-for="point in downloadPoints"
                :key="point.date"
                :cx="point.x"
                :cy="point.y"
                r="3"
              >
                <title>{{ point.label }}：下行 {{ formatBytes(point.value) }}</title>
              </circle>
            </g>

            <g class="chart-axis-labels">
              <text
                v-for="label in xLabels"
                :key="label.date"
                :x="label.x"
                :y="chartHeight - 8"
                text-anchor="middle"
              >{{ label.label }}</text>
            </g>
          </svg>
        </div>
      </article>

      <article class="observability-chart-card connection-card">
        <div class="chart-heading">
          <div>
            <span class="chart-eyebrow">CONNECTIONS</span>
            <h3>每日最高连接数</h3>
          </div>
          <strong class="connection-peak">{{ peakConnections === null ? '—' : peakConnections.toLocaleString('zh-CN') }}</strong>
        </div>

        <div v-if="hasConnectionSamples" class="chart-canvas">
          <svg
            class="connection-chart"
            :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
            role="img"
            aria-label="按日期统计的最高并发连接数"
          >
            <g class="chart-grid">
              <template v-for="tick in connectionTicks" :key="tick.value">
                <line :x1="plotLeft" :x2="plotRight" :y1="tick.y" :y2="tick.y" />
                <text :x="plotLeft - 10" :y="tick.y + 4" text-anchor="end">{{ tick.value.toLocaleString('zh-CN') }}</text>
              </template>
            </g>

            <path v-if="connectionPath" class="connection-area" :d="connectionAreaPath" />
            <path v-if="connectionPath" class="connection-line" :d="connectionPath" />
            <g class="connection-points">
              <circle
                v-for="point in connectionPoints"
                :key="point.date"
                :cx="point.x"
                :cy="point.y"
                r="4"
              >
                <title>{{ point.label }}：最高 {{ point.value.toLocaleString('zh-CN') }} 个连接</title>
              </circle>
            </g>

            <g class="chart-axis-labels">
              <text
                v-for="label in xLabels"
                :key="label.date"
                :x="label.x"
                :y="chartHeight - 8"
                text-anchor="middle"
              >{{ label.label }}</text>
            </g>
          </svg>
        </div>

        <div v-else class="connection-unavailable">
          <UiIcon name="activity" />
          <div>
            <strong>连接数暂未采集</strong>
            <p>当前内核上报不包含用户级峰值连接数。字段已经预留，未来上报后会直接按最大值渲染，不会使用平均值或累计值。</p>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from '../../components/UiIcon.vue'
import { formatBytes } from '../../utils/format'
import { TRAFFIC_OBSERVABILITY_RECORD_LIMIT, type TrafficObservabilityPoint } from './trafficObservability'

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
const recordLimit = TRAFFIC_OBSERVABILITY_RECORD_LIMIT

type NumericPoint = {
  date: string
  label: string
  value: number
  x: number
  y: number
}

const trafficMax = computed(() => Math.max(1, ...props.points.flatMap(point => [
  point.upload_bytes,
  point.download_bytes,
  point.used_bytes,
])))

const connectionMax = computed(() => Math.max(1, ...props.points.map(point => point.peak_connections ?? 0)))
const hasConnectionSamples = computed(() => props.points.some(point => point.peak_connections !== null))

function pointX(index: number) {
  if (props.points.length <= 1) return plotLeft + plotWidth / 2
  return plotLeft + (index / (props.points.length - 1)) * plotWidth
}

function pointY(value: number, maximum: number) {
  return plotBottom - (value / maximum) * plotHeight
}

function numericPoints(key: 'upload_bytes' | 'download_bytes'): NumericPoint[] {
  return props.points.map((point, index) => ({
    date: point.date,
    label: point.label,
    value: point[key],
    x: pointX(index),
    y: pointY(point[key], trafficMax.value),
  }))
}

function linePath(points: NumericPoint[]) {
  return points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(' ')
}

const uploadPoints = computed(() => numericPoints('upload_bytes'))
const downloadPoints = computed(() => numericPoints('download_bytes'))
const uploadPath = computed(() => linePath(uploadPoints.value))
const downloadPath = computed(() => linePath(downloadPoints.value))

const trafficBars = computed(() => {
  const slot = plotWidth / Math.max(1, props.points.length)
  const width = Math.max(3, Math.min(24, slot * 0.58))
  return props.points.map((point, index) => {
    const height = Math.max(point.used_bytes > 0 ? 2 : 0, (point.used_bytes / trafficMax.value) * plotHeight)
    return {
      date: point.date,
      label: point.label,
      value: point.used_bytes,
      x: pointX(index) - width / 2,
      y: plotBottom - height,
      width,
      height,
    }
  })
})

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
    x: pointX(index),
    y: pointY(point.peak_connections, connectionMax.value),
  }]
}))

const connectionPath = computed(() => linePath(connectionPoints.value))
const connectionAreaPath = computed(() => {
  const points = connectionPoints.value
  if (!points.length) return ''
  const first = points[0]
  const last = points[points.length - 1]
  return `${connectionPath.value} L ${last.x.toFixed(2)} ${plotBottom} L ${first.x.toFixed(2)} ${plotBottom} Z`
})

const xLabels = computed(() => {
  const maximumLabels = 7
  const step = Math.max(1, Math.ceil(props.points.length / maximumLabels))
  return props.points.flatMap((point, index) => {
    const shouldShow = index === 0 || index === props.points.length - 1 || index % step === 0
    return shouldShow ? [{ date: point.date, label: point.label, x: pointX(index) }] : []
  })
})
</script>

<style scoped>
.traffic-observability {
  overflow: hidden;
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
  padding: 18px 20px 14px;
}

.observability-chart-card + .observability-chart-card {
  border-left: 1px solid var(--line);
}

.chart-heading {
  min-height: 42px;
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
  gap: 12px;
  color: var(--muted);
  font-size: 10px;
}

.chart-legend span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.legend-swatch {
  width: 9px;
  height: 9px;
  display: inline-block;
  border-radius: 2px;
}

.legend-swatch.charged {
  background: var(--primary-muted);
  border: 1px solid var(--primary);
}

.legend-line {
  width: 13px;
  height: 2px;
  display: inline-block;
  border-radius: 999px;
}

.legend-line.upload {
  background: var(--success);
}

.legend-line.download {
  background: var(--warning);
}

.chart-canvas {
  width: 100%;
  overflow-x: auto;
}

.traffic-chart,
.connection-chart {
  width: 100%;
  min-width: 520px;
  height: auto;
  display: block;
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

.charged-bars rect {
  fill: var(--primary-muted);
  stroke: var(--primary);
  stroke-width: .8;
}

.traffic-line,
.connection-line {
  fill: none;
  stroke-width: 2.2;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.traffic-line.upload,
.traffic-points.upload circle {
  stroke: var(--success);
}

.traffic-line.download,
.traffic-points.download circle {
  stroke: var(--warning);
}

.traffic-points circle {
  fill: var(--surface);
  stroke-width: 2;
}

.connection-peak {
  color: var(--text-strong);
  font-size: 24px;
  letter-spacing: -.04em;
}

.connection-line,
.connection-points circle {
  stroke: var(--primary);
}

.connection-points circle {
  fill: var(--surface);
  stroke-width: 2.5;
}

.connection-area {
  fill: var(--primary-muted);
  opacity: .65;
}

.connection-unavailable {
  min-height: 238px;
  justify-content: flex-start;
  border: 1px dashed var(--line-strong);
  border-radius: var(--radius-sm);
}

@keyframes observability-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 980px) {
  .observability-grid {
    grid-template-columns: 1fr;
  }

  .observability-chart-card + .observability-chart-card {
    border-top: 1px solid var(--line);
    border-left: 0;
  }
}

@media (max-width: 640px) {
  .traffic-observability .panel-header,
  .chart-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .observability-status {
    justify-items: start;
  }

  .chart-legend {
    justify-content: flex-start;
  }
}
</style>
