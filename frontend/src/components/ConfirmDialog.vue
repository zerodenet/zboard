<template>
  <AlertDialogRoot :open="open" @update:open="handleOpenChange">
    <AlertDialogPortal>
      <AlertDialogOverlay class="ui-dialog-overlay ui-confirm-overlay" />
      <AlertDialogContent class="ui-dialog-content ui-confirm-content">
      <div class="app-dialog-heading">
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription :class="{ 'sr-only': !description || description === message }">{{ description && description !== message ? description : '请确认是否继续。' }}</AlertDialogDescription>
      </div>
      <div class="confirm-content" :data-tone="tone">
        <span><UiIcon :name="tone === 'danger' ? 'alert' : 'shield'" /></span>
        <p><slot>{{ message }}</slot></p>
      </div>
      <PageAlert v-if="error" tone="danger" title="无法完成操作">{{ error }}</PageAlert>
      <div class="app-dialog-footer">
        <UiButton variant="secondary" type="button" :disabled="busy" @click="$emit('close')">取消</UiButton>
        <UiButton :variant="tone === 'danger' ? 'danger' : 'primary'" type="button" :loading="busy" @click="$emit('confirm')">
          {{ busy ? '处理中…' : confirmText }}
        </UiButton>
      </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>

<script setup lang="ts">
import {
  AlertDialogContent, AlertDialogDescription,
  AlertDialogOverlay, AlertDialogPortal, AlertDialogRoot, AlertDialogTitle,
} from 'reka-ui'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{ open: boolean; title: string; description?: string; message?: string; error?: string; confirmText?: string; tone?: 'primary' | 'danger'; busy?: boolean }>(), {
  confirmText: '确认', tone: 'primary', busy: false, message: '', error: '',
})
const emit = defineEmits<{ close: []; confirm: [] }>()
function handleOpenChange(open: boolean) { if (!open && !props.busy) emit('close') }
</script>

<style scoped>
.confirm-content { display: grid; grid-template-columns: auto 1fr; align-items: flex-start; gap: 12px; padding: 18px 20px; }
.confirm-content > span { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 8px; color: var(--muted-foreground); background: var(--muted-surface); }
.confirm-content[data-tone='danger'] > span { color: var(--destructive); background: var(--danger-soft); }
.confirm-content p { margin: 3px 0 0; color: var(--muted-foreground); font-size: 13px; line-height: 1.5; }
</style>
