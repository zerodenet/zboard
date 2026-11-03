<template>
  <div v-if="compact" class="remote-read-status-compact" :data-read-phase="phase" :aria-label="`${label}读取状态`">
    <PopoverRoot v-if="retainedError">
      <PopoverTrigger as-child><UiButton class="read-failed" variant="ghost" size="sm" :aria-label="`${label}：查看失败原因`"><span role="status">{{ label }}：更新失败</span><UiIcon name="chevron" /></UiButton></PopoverTrigger>
      <PopoverPortal><PopoverContent as-child align="end" :side-offset="6" :collision-padding="16"><div class="ui-popover-content read-error-details">
        <strong>{{ label }}读取失败</strong>
        <p>{{ retainedError }}</p>
        <p v-if="lastSuccessAt">当前保留上次结果，成功读取于 <TimeBadge :value="lastSuccessAt" mode="relative" />。</p>
        <UiButton variant="secondary" size="sm" :loading="phase === 'loading' || phase === 'refreshing'" @click="$emit('retry')">重试</UiButton>
      </div></PopoverContent></PopoverPortal>
    </PopoverRoot>
    <span v-else-if="lastSuccessAt" class="read-success">更新于 <TimeBadge :value="lastSuccessAt" mode="relative" /></span>
    <span v-else role="status">{{ label }}：{{ phase === 'idle' ? '等待加载' : message }}</span>
  </div>
  <div v-else-if="phase !== 'idle' && (phase !== 'ready' || error)" class="remote-read-status" :class="{ 'read-failed': !!error }" role="status">
    <span>{{ label }}：{{ message }}</span>
    <TimeBadge v-if="lastSuccessAt" :value="lastSuccessAt" mode="relative" />
    <UiButton v-if="error" type="button" variant="ghost" size="sm" @click="$emit('retry')">重试</UiButton>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { PopoverRoot, PopoverTrigger, PopoverPortal, PopoverContent } from 'reka-ui'
import type { ReadPhase } from '../composables/useRemoteResource'
import TimeBadge from './TimeBadge.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
const props = defineProps<{ label: string; phase: ReadPhase; error: string; lastSuccessAt: string | null; compact?: boolean }>()
defineEmits<{ retry: [] }>()
const retainedError = ref('')
watch(() => [props.phase, props.error], () => {
  if (props.error) retainedError.value = props.error
  else if (props.phase === 'ready' || props.phase === 'idle') retainedError.value = ''
}, { immediate: true })
const message = computed(() => ({ idle: '', loading: '加载中', ready: '已更新', refreshing: '更新中，显示上次结果', stale: `更新失败，显示上次结果。${props.error}`, error: props.error, unauthorized: '登录或授权已失效', forbidden: '已无权查看此内容' }[props.phase]))
</script>
<style scoped>
.remote-read-status{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-block:8px;color:var(--muted);font-size:12px}.read-failed{color:var(--warning)}
.remote-read-status-compact { display: flex; justify-content: flex-end; align-items: center; width: 180px; height: 32px; flex: 0 0 180px; color: var(--muted-foreground); font-size: 12px; }
.read-success { display: inline-flex; gap: 6px; align-items: center; }
.read-error-details { box-sizing: border-box; width: 360px; max-width: min(calc(100vw - 32px), var(--reka-popover-content-available-width, 360px)); max-height: var(--reka-popover-content-available-height, 80vh); min-width: 0; overflow: auto; font-size: 13px; overscroll-behavior: contain; }
.read-error-details p { margin: 8px 0 12px; overflow-wrap: anywhere; }
</style>
