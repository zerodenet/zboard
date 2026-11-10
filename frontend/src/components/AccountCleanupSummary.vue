<template>
  <div class="account-cleanup-summary" :class="{ 'account-cleanup-due': due }" :data-cleanup-state="status?.state || 'unknown'">
    <time v-if="showDeadline" :datetime="status?.eligible_at || undefined" :aria-label="due ? `${formatDateTime(status?.eligible_at)}，已到清理时间` : undefined">{{ formatDateTime(status?.eligible_at) }}</time>
    <span v-else :title="status?.state === 'protected' ? status.reason : undefined" :aria-label="due ? '待清理，删除时间未确定' : deadline !== null && managed ? '暂无近期自动删除安排' : '删除时间未确定'">-</span>
  </div>
</template>
<script setup lang="ts">
import type { AccountCleanupStatus } from '../api/client'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { formatDateTime } from '../utils/format'
const props = defineProps<{ status?: AccountCleanupStatus | null }>()
const now = ref(Date.now())
const deadline = computed(() => {
  const value = Date.parse(props.status?.eligible_at || '')
  return Number.isFinite(value) ? value : null
})
const managed = computed(() => Boolean(props.status?.enabled && props.status.state !== 'protected'))
const due = computed(() => managed.value && (props.status?.state === 'eligible' || deadline.value !== null && deadline.value <= now.value))
const showDeadline = computed(() => {
  if (!managed.value || deadline.value === null) return false
  const windowDays = props.status?.inactive_days || 0
  return due.value || windowDays > 0 && deadline.value - now.value <= windowDays * 86_400_000
})
let clock: ReturnType<typeof setInterval> | undefined
onMounted(() => { clock = setInterval(() => { now.value = Date.now() }, 60_000) })
onUnmounted(() => { clearInterval(clock) })
</script>
<style scoped>
.account-cleanup-summary { display: grid; gap: 2px; font-size: 13px; line-height: 1.4; }
.account-cleanup-summary time { white-space: nowrap; font-variant-numeric: tabular-nums; }
.account-cleanup-due { color: var(--destructive); }
</style>
