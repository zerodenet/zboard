<template>
  <div v-if="phase !== 'idle' && (phase !== 'ready' || error)" class="remote-read-status" :class="{ 'read-failed': !!error }" role="status">
    <span>{{ label }}：{{ message }}</span>
    <TimeBadge v-if="lastSuccessAt" :value="lastSuccessAt" mode="relative" />
    <UiButton v-if="error" type="button" variant="ghost" size="sm" @click="$emit('retry')">重试</UiButton>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { ReadPhase } from '../composables/useRemoteResource'
import TimeBadge from './TimeBadge.vue'
import UiButton from './UiButton.vue'
const props = defineProps<{ label: string; phase: ReadPhase; error: string; lastSuccessAt: string | null }>()
defineEmits<{ retry: [] }>()
const message = computed(() => ({ idle: '', loading: '加载中', ready: '已更新', refreshing: '更新中，显示上次结果', stale: `更新失败，显示上次结果。${props.error}`, error: props.error, unauthorized: '登录或授权已失效', forbidden: '已无权查看此内容' }[props.phase]))
</script>
<style scoped>
.remote-read-status{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-block:8px;color:var(--muted);font-size:12px}.read-failed{color:var(--warning)}
</style>
