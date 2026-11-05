<template>
  <section class="system-load-region" aria-label="服务器负载">
    <div class="system-load">
    <UiSection title="负载趋势" icon="activity" variant="outlined" class="load-trend">
      <template #actions><UiSegmentedControl v-model="range" density="compact" label="负载时间范围" :options="ranges" /></template>
      <div class="trend-body">
        <PageAlert v-if="resource.data.value?.history?.state === 'error'" tone="warning">{{ resource.data.value.history.error || '监控历史保存失败，正在自动重试。' }}</PageAlert>
        <RemoteReadStatus v-if="resource.error.value" label="服务器指标" :phase="resource.phase.value" :error="resource.error.value" :last-success-at="resource.lastSuccessAt.value" @retry="refresh" />
        <div class="chart-legend" aria-label="曲线图例">
          <span v-for="series in loadSeries" :key="series.key" :class="{ 'network-legend': series.key === 'rx_bytes_per_second' }"><i :style="{ background: series.color }" />{{ series.label }}</span>
        </div>
        <div ref="stage" class="chart-stage" tabindex="0" role="group" aria-label="负载趋势图，左右方向键查看采样" @keydown="inspectKey" @pointermove="inspectPointer" @pointerup="inspectPointer" @pointerleave="leavePlot" @focus="focusPlot" @blur="selectedAt = null">
          <div class="percent-axis" aria-hidden="true"><span v-for="tick in ticks" :key="tick">{{ tick * 100 }}%</span></div>
          <div class="plot-drawing">
            <svg viewBox="0 0 1000 180" preserveAspectRatio="none" aria-hidden="true">
              <g v-for="tick in ticks" :key="tick"><line x1="0" x2="1000" :y1="(1 - tick) * 180" :y2="(1 - tick) * 180" class="grid-line" vector-effect="non-scaling-stroke" /></g>
              <path v-for="key in filledSeries" :key="key" :d="paths[key].area" :fill="loadSeries.find(s => s.key === key)!.color" class="chart-area" />
              <path v-for="series in loadSeries" :key="series.key" :data-series="series.key" :d="paths[series.key].line" fill="none" :stroke="series.color" stroke-width="1.25" vector-effect="non-scaling-stroke" />
              <line v-if="active" :x1="activePosition * 1000" :x2="activePosition * 1000" y1="0" y2="180" class="crosshair" vector-effect="non-scaling-stroke" />
            </svg>
            <template v-if="markerPoint"><i v-for="series in markerSeries" :key="series.key" class="active-point" :style="{ left: `${markerPosition * 100}%`, top: `${(1 - fraction(markerPoint[series.key]!, series.axis)) * 100}%`, background: series.color }" /></template>
          </div>
          <div class="network-axis" aria-hidden="true"><span v-for="tick in ticks" :key="tick">{{ rate(tick * networkMax) }}</span></div>
          <div v-if="!hasPlottable" class="chart-empty">{{ resource.loading.value ? '正在读取…' : resource.data.value?.history?.state === 'loading' ? '正在恢复历史…' : points.length ? '指标未取得' : '等待采样' }}</div>
          <div v-if="active" class="chart-inspection" :style="{ left: `${tooltipLeft}px` }" role="status">
            <time>{{ inspectTime(active.at) }}</time>
            <dl><div v-for="series in loadSeries" :key="series.key"><dt>{{ series.label }}</dt><dd>{{ series.axis === 'network' ? rate(active[series.key]) : percentage(active[series.key]) }}</dd></div></dl>
          </div>
        </div>
        <div class="time-axis" aria-hidden="true"><span v-for="(time, index) in timeTicks" :key="index" :style="{ left: `${index * 100 / (timeTicks.length - 1)}%` }">{{ clock(time) }}</span></div>
        <span v-if="partialHistory" class="history-start">采集自 {{ inspectTime(points[0]!.at) }}</span>
        <span class="sr-only">百分比使用左轴，实例网络速率使用右轴。采样于 {{ current?.at || '未取得' }}。</span>
      </div>
    </UiSection>
    <UiSection title="负载" icon="chart" variant="outlined" class="load-current">
      <div class="load-current-body">
        <div v-for="item in gauges" :key="item.label" class="resource-gauge" :class="{ danger: item.value != null && item.value >= 90 }">
          <div class="gauge-heading"><span><UiIcon :name="item.icon" />{{ item.label }}</span><strong>{{ item.text }}</strong></div>
          <div class="meter" :role="item.value == null ? 'img' : 'meter'" :aria-label="item.value == null ? `${item.label}未取得` : item.label" :aria-valuenow="item.value ?? undefined" :aria-valuetext="item.value == null ? undefined : percentage(item.value)" :aria-valuemin="item.value == null ? undefined : 0" :aria-valuemax="item.value == null ? undefined : 100"><i :style="{ width: `${item.value == null ? 0 : Math.min(100, item.value)}%` }" /></div>
        </div>
        <div class="network-current"><span><UiIcon name="network" />实例网络</span><strong>↓{{ rate(current?.rx_bytes_per_second) }} <span>↑{{ rate(current?.tx_bytes_per_second) }}</span></strong></div>
        <p v-if="diskPercent != null && diskPercent >= 90" class="capacity-warning" role="status"><span class="sr-only">磁盘空间不足，</span>剩余 {{ formatBytes(current?.disk?.available ?? ((current?.disk?.total || 0) - (current?.disk?.used || 0))) }}</p>
        <div class="instance-summary" aria-label="ZBoard 进程占用"><span>ZBoard</span><span><UiIcon name="cpu" /><span class="sr-only">CPU </span>{{ cores(current?.process_cpu_cores) }}</span><span><UiIcon name="memory" /><span class="sr-only">内存 </span>{{ formatBytes(current?.process_rss) }}</span></div>
        <details class="process-details"><summary>进程详情<span v-if="current?.issues.length" class="collection-warning">部分指标不可用</span></summary><dl><div v-if="resource.data.value?.history?.persistent"><dt>历史保存至</dt><dd><TimeBadge :value="resource.data.value.history.last_saved_at" /></dd></div><div><dt>负载 / 核数</dt><dd>{{ current?.load_1?.toFixed(2) ?? '-' }} / {{ current?.cpu_count ?? '-' }}</dd></div><div><dt>Go 堆内存</dt><dd>{{ formatBytes(current?.heap_bytes) }}</dd></div><div><dt>Goroutine</dt><dd>{{ current?.goroutines ?? '-' }}</dd></div></dl><p v-for="issue in current?.issues || []" :key="issue" class="collection-warning">{{ issue }}</p></details>
      </div>
    </UiSection>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchSystemMetrics, formatBytes, type SystemMetrics } from '../api/systemMetrics'
import { getDisplayTimeZone } from '../utils/timeZone'
import { loadSeries, networkCeiling, trendPaths, trendPosition, tooltipPosition, type LoadField } from '../utils/systemLoadChart'
import { useRemoteResource } from '../composables/useRemoteResource'
import UiSection from './UiSection.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'
import UiIcon from './UiIcon.vue'
import UiSegmentedControl from './UiSegmentedControl.vue'
import RemoteReadStatus from './RemoteReadStatus.vue'

const range = ref('360'), selectedAt = ref<string | null>(null), stage = ref<HTMLElement | null>(null), stageWidth = ref(400)
const ranges = [{ value: '60', label: '1h' }, { value: '360', label: '6h' }, { value: '720', label: '12h' }, { value: '1440', label: '24h' }]
const ticks = [1, .75, .5, .25, 0], filledSeries: LoadField[] = ['memory_percent', 'disk_percent']
const resource = useRemoteResource<SystemMetrics | null>({ initial: () => null, fetch: ({ signal }) => fetchSystemMetrics(Number(range.value), signal), errorMessage: '服务器指标未更新', timeoutMs: 8000 })
const current = computed(() => resource.data.value?.current)
const points = computed(() => resource.data.value?.points || [])
const end = computed(() => new Date(current.value?.at || Date.now()).getTime())
const start = computed(() => end.value - Number(resource.data.value?.range_minutes || range.value) * 60000)
const resolution = computed(() => resource.data.value?.resolution_seconds || 60)
const networkMax = computed(() => networkCeiling(points.value))
const active = computed(() => points.value.find(p => p.at === selectedAt.value))
const activePosition = computed(() => active.value ? trendPosition(active.value.at, start.value, end.value) : 0)
const markerPoint = computed(() => active.value || (points.value.length === 1 ? points.value[0] : undefined))
const markerPosition = computed(() => markerPoint.value ? trendPosition(markerPoint.value.at, start.value, end.value) : 0)
const markerSeries = computed(() => loadSeries.filter(s => markerPoint.value?.[s.key] != null))
const tooltipLeft = computed(() => tooltipPosition(activePosition.value, stageWidth.value))
const hasPlottable = computed(() => points.value.some(p => loadSeries.some(s => p[s.key] != null)))
const partialHistory = computed(() => points.value.length && Date.parse(points.value[0]!.at) > start.value + resolution.value * 1500)
const paths = computed(() => Object.fromEntries(loadSeries.map(s => [s.key, trendPaths(points.value, s.key, start.value, end.value, s.axis === 'percent' ? 100 : networkMax.value, resolution.value)])) as Record<LoadField, { line: string; area: string }>)
const timeTicks = computed(() => {
  const count = stageWidth.value >= 580 ? 9 : stageWidth.value >= 400 ? 7 : 4
  return Array.from({ length: count }, (_, i) => start.value + i * (end.value - start.value) / (count - 1))
})
function percent(v?: { used: number; total: number } | null) { return v && v.total > 0 ? v.used * 100 / v.total : null }
const diskPercent = computed(() => percent(current.value?.disk))
const gauges = computed(() => [
  { label: 'CPU', icon: 'cpu', value: current.value?.cpu_percent ?? null, text: percentage(current.value?.cpu_percent) },
  ...(['memory', 'disk'] as const).map(key => ({ label: key === 'memory' ? '内存' : '磁盘', icon: key, value: percent(current.value?.[key]), text: current.value?.[key] ? `${formatBytes(current.value[key]!.used, 2)} / ${formatBytes(current.value[key]!.total, 2)}` : '-' })),
])
function cores(value: number | null | undefined) { return value == null ? '-' : value > 0 && value < .001 ? '<0.001 核' : `${Number(value.toFixed(3))} 核` }
function percentage(value: number | null | undefined) { return value == null ? '-' : `${Number(value.toFixed(1))}%` }
function rate(value: number | null | undefined) { return value == null ? '-' : `${formatBytes(value)}/s` }
function fraction(value: number, axis: string) { return Math.max(0, Math.min(1, value / (axis === 'percent' ? 100 : networkMax.value))) }
function clock(time: number) { return new Date(time).toLocaleTimeString('zh-CN', { timeZone: getDisplayTimeZone(), hour: '2-digit', minute: '2-digit' }) }
function inspectTime(at: string) {
  const options: Intl.DateTimeFormatOptions = { timeZone: getDisplayTimeZone(), hour: '2-digit', minute: '2-digit' }
  if (Number(resource.data.value?.range_minutes || range.value) >= 720) { options.month = '2-digit'; options.day = '2-digit' }
  return new Date(at).toLocaleString('zh-CN', options)
}
function focusPlot() { if (!selectedAt.value) selectedAt.value = points.value.at(-1)?.at || null }
function leavePlot(event: PointerEvent) { if (event.pointerType !== 'touch') selectedAt.value = null }
function inspectKey(event: KeyboardEvent) {
  if (event.key === 'Escape') { selectedAt.value = null; return }
  if (!points.value.length || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const index = Math.max(0, points.value.findIndex(p => p.at === selectedAt.value))
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? points.value.length - 1 : Math.max(0, Math.min(points.value.length - 1, index + (event.key === 'ArrowLeft' ? -1 : 1)))
  selectedAt.value = points.value[next]!.at
}
function inspectPointer(event: PointerEvent) {
  const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const position = Math.max(0, Math.min(1, (event.clientX - bounds.left - 32) / Math.max(1, bounds.width - 76)))
  const time = start.value + position * (end.value - start.value)
  let nearest: string | null = null, distance = Infinity
  for (const point of points.value) {
    const delta = Math.abs(Date.parse(point.at) - time)
    if (delta < distance) { nearest = point.at; distance = delta }
  }
  // Empty regions, including outages, must not show an unrelated distant sample.
  selectedAt.value = distance <= resolution.value * 1500 ? nearest : null
}
async function refresh() { if (!resource.loading.value) await resource.load() }
watch(range, () => { selectedAt.value = null; resource.invalidate(); void resource.load() })
watch(points, () => { if (selectedAt.value && !points.value.some(p => p.at === selectedAt.value)) selectedAt.value = null })
let timer: ReturnType<typeof setInterval> | undefined, observer: ResizeObserver | undefined
onMounted(() => {
  if (stage.value) {
    stageWidth.value = stage.value.getBoundingClientRect().width
    observer = new ResizeObserver(entries => { stageWidth.value = entries[0]?.contentRect.width || stageWidth.value })
    observer.observe(stage.value)
  }
  void refresh()
  timer = setInterval(() => { if (!document.hidden && resource.canAutoRefresh.value) void refresh() }, 15000)
})
onBeforeUnmount(() => { clearInterval(timer); observer?.disconnect(); resource.invalidate() })
defineExpose({ refresh })
</script>

<style scoped>
.system-load-region { container: system-load / inline-size; min-width: 0; }
.system-load {
  display: grid;
  grid-template-columns: minmax(0, 1.9fr) minmax(270px, 1fr);
  align-items: stretch;
  gap: 16px;
  min-width: 0;
}
.load-trend, .load-current { min-width: 0; }
.system-load :deep(.panel-header) { padding: 0 0 10px; border-bottom: 0; gap: 12px; min-height: 46px; }
.system-load :deep(.panel-header h2) { font-size: 14px; line-height: 20px; font-weight: 600; }
.trend-body { padding: 0; }
.chart-legend { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; min-height: 26px; margin-bottom: 7px; color: var(--text-secondary); font-size: 11px; }
.chart-legend span { display: inline-flex; align-items: center; gap: 4px; white-space: nowrap; }
.chart-legend i { width: 7px; height: 7px; border-radius: 50%; }
.network-legend { padding-left: 16px; border-left: 1px solid var(--line); }
.chart-stage { position: relative; height: 180px; outline-offset: 4px; touch-action: pan-y; }
.chart-stage:focus-visible { outline: 2px solid var(--focus-border); border-radius: 3px; }
.plot-drawing { position: absolute; left: 32px; right: 44px; height: 100%; }
svg { display: block; width: 100%; height: 100%; overflow: visible; }
.grid-line { stroke: var(--line); stroke-width: .7; stroke-dasharray: 2 4; }
.crosshair { stroke: var(--line-strong); stroke-width: 1; }
.chart-area { opacity: .045; }
.percent-axis, .network-axis { position: absolute; top: 0; bottom: 0; display: flex; flex-direction: column; justify-content: space-between; color: var(--muted); font-size: 11px; line-height: 12px; pointer-events: none; }
.percent-axis { left: 0; width: 27px; align-items: flex-end; }
.network-axis { right: 0; width: 36px; align-items: flex-start; }
.network-axis span { max-width: 36px; }
.percent-axis span:first-child, .network-axis span:first-child { transform: translateY(-4px); }
.percent-axis span:last-child, .network-axis span:last-child { transform: translateY(4px); }
.active-point { position: absolute; width: 8px; height: 8px; border: 1.5px solid var(--surface); border-radius: 50%; transform: translate(-50%, -50%); pointer-events: none; box-shadow: 0 0 0 .5px var(--surface); }
.chart-empty { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); font-size: 12px; pointer-events: none; }
.chart-inspection { position: absolute; top: 2px; z-index: 2; box-sizing: border-box; width: 148px; padding: 9px 10px; border: 1px solid var(--line); border-radius: 7px; background: var(--surface); box-shadow: var(--shadow-sm); color: var(--text-strong); font-size: 12px; line-height: 18px; pointer-events: none; }
.chart-inspection time { display: block; font-weight: 500; margin-bottom: 5px; }
.chart-inspection dl { margin: 0; }
.chart-inspection dl>div { display: flex; justify-content: space-between; gap: 8px; }
.chart-inspection dt { color: var(--text-secondary); }
.chart-inspection dd { margin: 0; text-align: right; font-family: var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; }
.time-axis { position: relative; height: 19px; margin: 7px 44px 0 32px; color: var(--muted); font-size: 11px; line-height: 14px; }
.time-axis span { position: absolute; transform: translateX(-50%); white-space: nowrap; }
.time-axis span:first-child { transform: none; }
.time-axis span:last-child { transform: translateX(-100%); }
.history-start { display: block; color: var(--muted); font-size: 11px; }
.load-current-body { display: grid; gap: 12px; padding: 0; }
.gauge-heading, .network-current { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 12px; }
.gauge-heading>span, .network-current>span { display: inline-flex; align-items: center; gap: 6px; color: var(--text-secondary); white-space: nowrap; }
.gauge-heading .ui-icon, .network-current .ui-icon, .instance-summary .ui-icon { width: 13px; height: 13px; flex: none; }
.gauge-heading strong, .network-current strong { color: var(--text-strong); font-family: var(--font-mono, ui-monospace, monospace); font-size: 11px; font-weight: 500; font-variant-numeric: tabular-nums; text-align: right; }
.network-current strong { display: flex; flex-wrap: wrap; justify-content: flex-end; column-gap: 5px; }
.network-current strong>span { white-space: nowrap; }
.meter { height: 7px; margin-top: 5px; overflow: hidden; border-radius: 5px; background: var(--line-strong); }
.meter i { display: block; height: 100%; background: var(--load-cpu); border-radius: inherit; }
.resource-gauge.danger .meter i { background: var(--danger); }
.resource-gauge.danger strong, .capacity-warning { color: var(--danger); }
.capacity-warning { margin: -5px 0 0; font-size: 12px; }
.instance-summary { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding-top: 11px; border-top: 1px solid var(--line); color: var(--text-secondary); font-size: 11px; }
.instance-summary>span { display: inline-flex; align-items: center; gap: 4px; font-variant-numeric: tabular-nums; }
.instance-summary>span:first-child { margin-right: auto; }
.process-details { font-size: 12px; color: var(--muted); }
.process-details summary { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.process-details summary::before { content: '›'; transition: transform .12s; }
.process-details[open] summary::before { transform: rotate(90deg); }
.process-details dl>div { display: flex; justify-content: space-between; gap: 8px; margin-top: 7px; }
.process-details dd { margin: 0; }
.collection-warning { color: var(--warning); }
@container system-load (max-width: 760px) {
  .system-load { grid-template-columns: minmax(0, 1fr); }
  .load-current-body { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 24px; }
  .capacity-warning, .instance-summary, .process-details { grid-column: 1 / -1; }
}
@container system-load (max-width: 520px) {
  .system-load :deep(.panel-header) { padding-inline: 0; }
  .trend-body { padding-inline: 12px; }
  .chart-legend { gap: 11px; font-size: 11px; }
  .network-legend { padding-left: 10px; }
  .chart-stage { height: 170px; }
  .load-current-body { padding-inline: 0; grid-template-columns: minmax(0, 1fr); gap: 13px; }
}
</style>
