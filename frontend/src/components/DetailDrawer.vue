<template>
  <DialogRoot :open="open" @update:open="handleOpenChange">
    <DialogPortal>
      <DialogOverlay class="ui-dialog-overlay" />
      <DialogContent class="detail-drawer ui-sheet-content" @open-auto-focus="handleOpenAutoFocus" @close-auto-focus="handleCloseAutoFocus" @keydown="handleKeydown">
        <header class="detail-drawer-header">
          <div>
            <span v-if="eyebrow">{{ eyebrow }}</span>
            <DialogTitle>{{ title }}</DialogTitle>
            <DialogDescription :class="{ 'sr-only': !description }">{{ description || title }}</DialogDescription>
          </div>
          <UiButton ref="closeButton" variant="ghost" icon class="icon-button" type="button" aria-label="关闭详情" @click="emit('close')"><UiIcon name="close" /></UiButton>
        </header>
        <div class="detail-drawer-body"><slot /></div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{ open: boolean; title: string; eyebrow?: string; description?: string; returnFocusSelector?: string }>(), { eyebrow: '', description: '', returnFocusSelector: '' })
const emit = defineEmits<{ close: [] }>()
const closeButton = ref<InstanceType<typeof UiButton> | null>(null)
let previousFocus: HTMLElement | null = null
let returnFocusSelector = ''
watch(() => props.open, async (open, wasOpen) => {
  if (open && !wasOpen) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    returnFocusSelector = props.returnFocusSelector
  } else if (!open && wasOpen) {
    await nextTick()
    restoreFocus()
  }
}, { immediate: true })
function handleOpenChange(open: boolean) { if (!open) emit('close') }
function handleOpenAutoFocus(event: Event) {
  event.preventDefault()
  ;(closeButton.value?.$el as HTMLElement | undefined)?.focus()
}
function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const drawer = event.currentTarget as HTMLElement
  const focusable = Array.from(drawer.querySelectorAll<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )).filter(element => !element.hasAttribute('hidden'))
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}
function handleCloseAutoFocus(event: Event) {
  event.preventDefault()
  restoreFocus()
}
function restoreFocus() {
  const replacement = returnFocusSelector ? document.querySelector<HTMLElement>(returnFocusSelector) : null
  ;(replacement || (previousFocus?.isConnected ? previousFocus : null))?.focus()
  previousFocus = null
  returnFocusSelector = ''
}
</script>
