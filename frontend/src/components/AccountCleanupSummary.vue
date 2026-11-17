<template>
  <div class="account-cleanup-summary" :class="{ 'account-cleanup-due': due }" :data-cleanup-state="status?.state || 'unknown'">
    <time v-if="showDeadline" :datetime="status?.eligible_at || undefined" :aria-label="due ? `${formatDateTime(status?.eligible_at)}，已到清理时间` : undefined">{{ formatDateTime(status?.eligible_at) }}</time>
    <PopoverRoot v-else-if="protectedReason" v-model:open="reasonOpen">
      <PopoverTrigger as-child><button class="cleanup-reason-trigger" type="button" :aria-label="`暂不自动删除：${protectedReason}`">-</button></PopoverTrigger>
      <PopoverPortal><PopoverContent as-child align="start" :side-offset="6" :collision-padding="16"><div class="ui-popover-content cleanup-reason-content"><span>{{ protectedReason }}</span><RouterLink v-if="userId && protectedReason === '有未结订单'" :to="{ path: '/admin/orders', query: { user_id: String(userId) } }">查看订单</RouterLink></div></PopoverContent></PopoverPortal>
    </PopoverRoot>
    <span v-else :title="status?.state === 'protected' ? status.reason : undefined" :aria-label="due ? '待清理，删除时间未确定' : deadline !== null && managed ? '暂无近期自动删除安排' : '删除时间未确定'">-</span>
  </div>
</template>
<script setup lang="ts">
import type { AccountCleanupStatus } from '../api/client'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { formatDateTime } from '../utils/format'
const props = defineProps<{ status?: AccountCleanupStatus | null; userId?: number }>()
const reasonOpen = ref(false)
const protectedReason = computed(() => props.status?.enabled && props.status.state === 'protected' ? props.status.reason : '')
watch([() => props.userId, () => props.status?.enabled, () => props.status?.state, protectedReason], () => { reasonOpen.value = false })
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
.cleanup-reason-trigger { width: fit-content; min-width: 24px; min-height: 24px; padding: 0; border: 0; background: transparent; color: inherit; font: inherit; text-align: start; cursor: help; }
.cleanup-reason-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: 3px; }
.cleanup-reason-content { display: grid; gap: 8px; max-width: min(280px, calc(100vw - 32px)); font-size: 13px; }
</style>
