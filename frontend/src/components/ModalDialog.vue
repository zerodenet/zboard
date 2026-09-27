<template>
  <DialogRoot :open="open" @update:open="handleOpenChange">
    <DialogPortal>
      <DialogOverlay class="ui-dialog-overlay" />
      <DialogContent
        class="app-dialog ui-dialog-content"
        :class="{ 'app-dialog-fixed': fixedBody }"
        :style="dialogStyle"
        :close-on-escape="!busy"
        :disable-outside-pointer-events="true"
        @escape-key-down="handleDismiss"
        @pointer-down-outside="handleDismiss"
        @close-auto-focus="handleCloseAutoFocus"
      >
        <header class="app-dialog-header">
          <div class="app-dialog-heading">
            <DialogTitle>{{ title }}</DialogTitle>
            <DialogDescription :class="{ 'sr-only': !description }">{{ description || title }}</DialogDescription>
          </div>
          <UiButton variant="ghost" icon type="button" aria-label="关闭弹窗" :disabled="busy" @click="requestClose">×</UiButton>
        </header>
        <div class="app-dialog-body"><slot /></div>
        <footer v-if="$slots.footer" class="app-dialog-footer"><slot name="footer" :request-close="requestClose" /></footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { confirmAction } from '../utils/feedback'
import UiButton from './UiButton.vue'

const props = withDefaults(defineProps<{
  open: boolean
  title: string
  description?: string
  size?: 'sm' | 'md' | 'lg' | 'xl'
  busy?: boolean
  fixedBody?: boolean
  dirty?: boolean
  discardTitle?: string
  discardMessage?: string
  returnFocusSelector?: string
}>(), { size: 'md', busy: false, fixedBody: false, dirty: false, discardTitle: '放弃未保存的修改？', discardMessage: '当前表单包含尚未保存的内容，关闭后这些修改将丢失。', returnFocusSelector: '' })
const emit = defineEmits<{ close: [] }>()
const widths = { sm: '27.5rem', md: '40rem', lg: '51.25rem', xl: '65rem' }
const dialogStyle = computed(() => ({ width: widths[props.size], maxHeight: '88vh' }))
let previousFocus: HTMLElement | null = null
let returnFocusSelector = ''

watch(() => props.open, (open, wasOpen) => {
  if (open && !wasOpen) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    returnFocusSelector = props.returnFocusSelector
  }
}, { immediate: true })

async function requestClose() {
  if (props.busy) return
  if (props.dirty && !await confirmAction({
    title: props.discardTitle,
    message: props.discardMessage,
    confirmText: '放弃修改',
    tone: 'danger',
  })) return
  emit('close')
}
function handleOpenChange(open: boolean) { if (!open) void requestClose() }
function handleDismiss(event: Event) {
  event.preventDefault()
  void requestClose()
}
function restoreFocus() {
  const replacement = returnFocusSelector ? document.querySelector<HTMLElement>(returnFocusSelector) : null
  const target = replacement || (previousFocus?.isConnected ? previousFocus : null)
  target?.focus()
  previousFocus = null
  returnFocusSelector = ''
}
function handleCloseAutoFocus(event: Event) {
  event.preventDefault()
  restoreFocus()
}
defineExpose({ requestClose, restoreFocus })
</script>
