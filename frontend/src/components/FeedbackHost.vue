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
  <Teleport to="body">
    <div class="toast-viewport" aria-live="polite" aria-relevant="additions">
      <TransitionGroup name="toast">
        <article v-for="toast in feedbackState.toasts" :key="toast.id" class="ui-toast" :data-tone="toast.tone">
          <span class="toast-symbol"><UiIcon :name="toast.tone === 'danger' ? 'alert' : toast.tone === 'success' ? 'check' : 'activity'" /></span>
          <div><strong>{{ toast.title }}</strong><p v-if="toast.message">{{ toast.message }}</p></div>
          <button class="icon-button" type="button" aria-label="关闭提示" @click="dismissToast(toast.id)"><UiIcon name="close" /></button>
        </article>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { dismissToast, feedbackState, settleConfirm } from '../utils/feedback'
import ConfirmDialog from './ConfirmDialog.vue'
import UiIcon from './UiIcon.vue'
</script>
