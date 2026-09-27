<template>
  <ConfirmDialog
    :open="Boolean(feedbackState.confirm)"
    :title="feedbackState.confirm?.title || ''"
    :description="feedbackState.confirm?.description"
    :message="feedbackState.confirm?.message"
    :confirm-text="feedbackState.confirm?.confirmText"
    :tone="feedbackState.confirm?.tone"
    @close="settleConfirm(false)"
    @confirm="settleConfirm(true)"
  />
  <div class="ui-toast-viewport" aria-label="通知">
    <div v-for="item in feedbackState.toasts" :key="item.id" class="ui-toast" :data-tone="item.tone" role="status">
      <div><strong>{{ item.title }}</strong><p v-if="item.message">{{ item.message }}</p></div>
      <UiButton variant="ghost" icon size="sm" type="button" aria-label="关闭通知" @click="dismissToast(item.id)">×</UiButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { dismissToast, feedbackState, settleConfirm } from '../utils/feedback'
import ConfirmDialog from './ConfirmDialog.vue'
import UiButton from './UiButton.vue'

const timers = new Map<number, ReturnType<typeof setTimeout>>()
watch(() => feedbackState.toasts.map(item => item.id), ids => {
  const active = new Set(ids)
  for (const [id, timer] of timers) {
    if (active.has(id)) continue
    clearTimeout(timer)
    timers.delete(id)
  }
  for (const item of feedbackState.toasts) {
    if (timers.has(item.id)) continue
    timers.set(item.id, setTimeout(() => dismissToast(item.id), item.tone === 'danger' ? 6500 : 4200))
  }
}, { immediate: true })
onBeforeUnmount(() => { for (const timer of timers.values()) clearTimeout(timer) })
</script>
