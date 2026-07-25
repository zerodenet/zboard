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
  <PrimeToast
    position="top-right"
    :close-button-props="{ 'aria-label': '关闭通知' }"
    @close="onToastDismiss"
    @life-end="onToastDismiss"
  />
</template>

<script setup lang="ts">
import { watch } from 'vue'
import PrimeToast from 'primevue/toast'
import type { ToastEvent } from 'primevue/toast'
import { useToast } from 'primevue/usetoast'
import { dismissToast, feedbackState, settleConfirm } from '../utils/feedback'
import ConfirmDialog from './ConfirmDialog.vue'

const toast = useToast()
const rendered = new Map<number, Record<string, any>>()

watch(() => feedbackState.toasts.slice(), (items) => {
  const activeIDs = new Set(items.map(item => item.id))
  for (const [id, message] of rendered) {
    if (activeIDs.has(id)) continue
    toast.remove(message)
    rendered.delete(id)
  }

  for (const item of items) {
    if (rendered.has(item.id)) continue
    const message = {
      id: item.id,
      severity: item.tone === 'danger' ? 'error' : item.tone,
      summary: item.title,
      detail: item.message,
      life: item.tone === 'danger' ? 6500 : 4200,
      closable: true,
    }
    rendered.set(item.id, message)
    toast.add(message)
  }
}, { deep: true, immediate: true })

function onToastDismiss(event: ToastEvent) {
  const id = Number((event?.message as { id?: unknown })?.id)
  if (!Number.isInteger(id)) return
  rendered.delete(id)
  dismissToast(id)
}
</script>
