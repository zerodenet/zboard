<template>
  <div class="date-range-picker" role="group" :aria-label="label + '（UTC）'">
    <nav class="date-range-presets" aria-label="快捷日期范围">
      <button v-for="preset in presets" :key="preset.key" type="button"
        :class="{ selected: selectedPreset === preset.key }" :aria-pressed="selectedPreset === preset.key"
        @click="choosePreset(preset.key)">{{ preset.label }}</button>
    </nav>
    <div class="date-range-main">
      <div class="date-range-months">
        <section v-for="(month, index) in months" :key="month.key" class="date-range-month"
          :class="{ 'date-range-month-secondary': index === 1 }" :aria-label="month.label">
          <header class="date-range-month-heading">
            <button v-if="index === 0" type="button" class="date-range-month-nav previous"
              :aria-label="'上一个月，当前 ' + month.label" @click="moveMonth(-1)"><UiIcon name="chevron" /></button>
            <span v-else aria-hidden="true" />
            <strong>{{ month.label }}</strong>
            <button v-if="index === 1" type="button" class="date-range-month-nav"
              :aria-label="'下一个月，当前 ' + month.label" @click="moveMonth(1)"><UiIcon name="chevron" /></button>
            <button v-else type="button" class="date-range-month-nav date-range-mobile-next"
              :aria-label="'下一个月，当前 ' + month.label" @click="moveMonth(1)"><UiIcon name="chevron" /></button>
          </header>
          <div class="date-range-grid" role="group" :aria-label="month.label + '日期'">
            <span v-for="weekday in weekdays" :key="weekday" class="date-range-weekday" aria-hidden="true">{{ weekday }}</span>
            <button v-for="day in month.days" :key="day.date" type="button" class="date-range-day"
              :class="{ outside: !day.inMonth, today: day.inMonth && day.date === today, edge: day.inMonth && isEdge(day.date), inRange: day.inMonth && isInRange(day.date) }"
              :aria-label="day.ariaLabel + (day.inMonth && isEdge(day.date) ? '，已选中' : '')"
              :aria-current="day.inMonth && day.date === today ? 'date' : undefined"
              :disabled="!day.inMonth" @click="chooseDay(day.date)">{{ day.day }}</button>
          </div>
        </section>
      </div>
      <div class="date-range-selection" aria-live="polite">
        <span>{{ selectionText }}</span><span class="date-range-timezone">UTC · 含结束日</span>
      </div>
      <p v-if="rangeError" class="date-range-error" role="alert">{{ rangeError }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UiIcon from './UiIcon.vue'

type PresetKey = 'today' | 'seven' | 'thirty' | 'sixMonths' | 'thisMonth' | 'all'
type CalendarDay = { date: string; day: number; inMonth: boolean; ariaLabel: string }
const props = withDefaults(defineProps<{ from: string; to: string; label: string; presetDirection?: 'past' | 'future' }>(), { presetDirection: 'past' })
const emit = defineEmits<{ 'update:from': [value: string]; 'update:to': [value: string] }>()
const weekdays = ['一', '二', '三', '四', '五', '六', '日']
const today = toISODate(new Date())
const selectedPreset = ref<PresetKey | ''>('')
const choosingEnd = ref(false)
const initialDate = parseISODate(props.from) || (props.presetDirection === 'past' ? addMonths(parseISODate(today)!, -1) : parseISODate(today)!)
const visibleMonth = ref(new Date(Date.UTC(initialDate.getUTCFullYear(), initialDate.getUTCMonth(), 1)))
const presets = computed<{ key: PresetKey; label: string }[]>(() => [
  { key: 'today', label: '今天' },
  { key: 'seven', label: props.presetDirection === 'future' ? '未来 7 天' : '最近 7 天' },
  { key: 'thirty', label: props.presetDirection === 'future' ? '未来 30 天' : '最近 30 天' },
  { key: 'sixMonths', label: props.presetDirection === 'future' ? '未来 6 个月' : '最近 6 个月' },
  { key: 'thisMonth', label: '本月' },
  { key: 'all', label: '不限日期' },
])
const months = computed(() => [calendarMonth(visibleMonth.value), calendarMonth(addMonths(visibleMonth.value, 1))])
const selectionText = computed(() => props.from && props.to ? props.from + '  →  ' + props.to : '选择开始和结束日期')
const rangeError = computed(() => {
  if (!props.from && !props.to) return ''
  const from = parseISODate(props.from), to = parseISODate(props.to)
  if (!from || !to) return '请选择完整的日期范围'
  if (from > to) return '开始日期不能晚于结束日期'
  if ((to.getTime() - from.getTime()) / 86400000 >= 366) return '日期范围不能超过 366 天'
  return ''
})
watch(() => [props.from, props.to], () => {
  if (!props.from && !props.to) selectedPreset.value = ''
})

function parseISODate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const date = new Date(value + 'T00:00:00Z')
  return Number.isNaN(date.getTime()) || toISODate(date) !== value ? null : date
}
function toISODate(date: Date) { return date.toISOString().slice(0, 10) }
function addDays(date: Date, days: number) { return new Date(date.getTime() + days * 86400000) }
function addMonths(date: Date, months: number) { return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + months, 1)) }
function addCalendarMonths(date: Date, months: number) {
  const first = addMonths(date, months)
  const lastDay = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)).getUTCDate()
  return new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth(), Math.min(date.getUTCDate(), lastDay)))
}
function calendarMonth(first: Date) {
  const year = first.getUTCFullYear(), month = first.getUTCMonth()
  const leading = (first.getUTCDay() + 6) % 7
  const start = addDays(first, -leading)
  const weeks = Math.ceil((leading + new Date(Date.UTC(year, month + 1, 0)).getUTCDate()) / 7)
  const days: CalendarDay[] = Array.from({ length: weeks * 7 }, (_, index) => {
    const value = addDays(start, index)
    return { date: toISODate(value), day: value.getUTCDate(), inMonth: value.getUTCMonth() === month,
      ariaLabel: value.getUTCFullYear() + '年' + (value.getUTCMonth() + 1) + '月' + value.getUTCDate() + '日' }
  })
  return { key: year + '-' + month, label: year + '年' + (month + 1) + '月', days }
}
function moveMonth(amount: number) { visibleMonth.value = addMonths(visibleMonth.value, amount) }
function isEdge(date: string) { return date === props.from || date === props.to }
function isInRange(date: string) { return Boolean(props.from && props.to && date > props.from && date < props.to) }
function chooseDay(date: string) {
  selectedPreset.value = ''
  if (!choosingEnd.value || !props.from) {
    emit('update:from', date)
    emit('update:to', date)
    choosingEnd.value = true
    return
  }
  emit('update:from', date < props.from ? date : props.from)
  emit('update:to', date < props.from ? props.from : date)
  choosingEnd.value = false
}
function choosePreset(key: PresetKey) {
  selectedPreset.value = key
  choosingEnd.value = false
  if (key === 'all') { emit('update:from', ''); emit('update:to', ''); return }
  const now = parseISODate(today)!
  let from = now, to = now
  if (key === 'seven' || key === 'thirty') {
    const days = key === 'seven' ? 6 : 29
    if (props.presetDirection === 'future') to = addDays(now, days)
    else from = addDays(now, -days)
  } else if (key === 'sixMonths') {
    if (props.presetDirection === 'future') to = addCalendarMonths(now, 6)
    else from = addCalendarMonths(now, -6)
  } else if (key === 'thisMonth') {
    from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1))
    to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 0))
  }
  emit('update:from', toISODate(from))
  emit('update:to', toISODate(to))
  visibleMonth.value = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + (props.presetDirection === 'past' ? -1 : 0), 1))
}
defineExpose({ rangeError })
</script>

<style scoped>
.date-range-picker { display:grid; grid-template-columns:124px minmax(0,1fr); min-width:0; color:var(--foreground); }
.date-range-presets { display:flex; flex-direction:column; gap:2px; padding:10px 9px; border-right:1px solid var(--line); }
.date-range-presets button { min-height:32px; padding:5px 10px; border:0; border-radius:6px; color:var(--text-secondary); background:transparent; font-size:12px; text-align:left; cursor:pointer; }
.date-range-presets button:hover,.date-range-presets button.selected { color:var(--foreground); background:var(--surface-subtle); }
.date-range-presets button:focus-visible,.date-range-day:focus-visible,.date-range-month-nav:focus-visible { outline:2px solid var(--ring); outline-offset:-2px; }
.date-range-main { min-width:0; padding:11px 14px 8px; }
.date-range-months { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:19px; }
.date-range-month { min-width:0; }
.date-range-month-heading { height:34px; display:grid; grid-template-columns:28px 1fr 28px; align-items:center; margin-bottom:5px; }
.date-range-month-heading strong { font-size:13px; font-weight:650; text-align:center; }
.date-range-month-nav { width:27px; height:27px; display:grid; place-items:center; padding:0; border:0; border-radius:5px; color:var(--text-secondary); background:transparent; cursor:pointer; }
.date-range-month-nav:hover { color:var(--foreground); background:var(--surface-subtle); }
.date-range-month-nav :deep(.ui-icon) { width:14px; height:14px; }
.date-range-month-nav.previous :deep(.ui-icon) { transform:rotate(180deg); }
.date-range-mobile-next { display:none; }
.date-range-grid { display:grid; grid-template-columns:repeat(7,minmax(0,1fr)); gap:2px 0; }
.date-range-weekday,.date-range-day { min-width:0; height:30px; display:grid; place-items:center; font-size:12px; font-variant-numeric:tabular-nums; }
.date-range-weekday { color:var(--muted-foreground); }
.date-range-day { padding:0; border:0; border-radius:5px; color:var(--foreground); background:transparent; cursor:pointer; }
.date-range-day.outside { color:var(--subtle); }
.date-range-day:disabled { cursor:default; }
.date-range-day.today:not(.edge) { box-shadow:inset 0 0 0 1px var(--input); }
.date-range-day.inRange { border-radius:0; background:var(--surface-selection); }
.date-range-day.edge { color:var(--primary-foreground); background:var(--primary); }
.date-range-day:hover:not(.edge):not(:disabled) { background:var(--surface-subtle); }
.date-range-selection { display:flex; justify-content:space-between; gap:8px; margin-top:10px; padding-top:10px; border-top:1px solid var(--line); color:var(--text-secondary); font-size:11px; font-variant-numeric:tabular-nums; }
.date-range-timezone { color:var(--muted-foreground); white-space:nowrap; }
.date-range-error { margin:6px 0 0; color:var(--danger); font-size:11px; }
@media (max-width:650px) {
  .date-range-picker { grid-template-columns:1fr; }
  .date-range-presets { flex-direction:row; overflow-x:auto; padding:7px; border-right:0; border-bottom:1px solid var(--line); scrollbar-width:thin; }
  .date-range-presets button { flex:none; min-height:28px; padding:4px 8px; white-space:nowrap; }
  .date-range-main { padding:8px 10px; }
  .date-range-months { grid-template-columns:1fr; }
  .date-range-month-secondary { display:none; }
  .date-range-mobile-next { display:grid; }
}
</style>
