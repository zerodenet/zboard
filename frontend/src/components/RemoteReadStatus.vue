<template>
  <div v-if="phase !== 'idle'" class="remote-read-status" :class="{ 'read-failed': !!error }" role="status">
    <span>{{ label }}：{{ message }}</span>
    <TimeBadge v-if="lastSuccessAt" :value="lastSuccessAt" mode="relative" />
    <button v-if="error" type="button" class="button button-ghost button-sm" @click="$emit('retry')">重试</button>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { ReadPhase } from '../composables/useRemoteResource'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ label: string; phase: ReadPhase; error: string; lastSuccessAt: string | null }>()
defineEmits<{ retry: [] }>()
const message = computed(() => ({ idle: '', loading: '加载中', ready: '已更新', refreshing: '更新中，显示上次结果', stale: `更新失败，显示上次结果。${props.error}`, error: props.error, unauthorized: '登录或授权已失效', forbidden: '已无权查看此内容' }[props.phase]))
</script>
<style scoped>
.remote-read-status{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-block:8px;color:var(--muted);font-size:12px}.read-failed{color:var(--warning)}
</style>
