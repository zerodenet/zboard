<template>
  <PrimeDialog
    :visible="open"
    modal
    block-scroll
    :closable="!busy"
    :close-button-props="{ 'aria-label': '关闭弹窗' }"
    :close-on-escape="!busy"
    :dismissable-mask="!busy"
    :draggable="false"
    :style="dialogStyle"
    :breakpoints="{ '700px': '100vw' }"
    class="app-dialog"
    :class="{ 'app-dialog-fixed': fixedBody }"
    @update:visible="handleVisible"
    @after-hide="restoreFocus"
  >
    <template #header>
      <div class="app-dialog-heading">
        <h2>{{ title }}</h2>
        <p v-if="description">{{ description }}</p>
      </div>
    </template>
    <div class="app-dialog-body"><slot /></div>
    <template v-if="$slots.footer" #footer><slot name="footer" :request-close="requestClose" /></template>
  </PrimeDialog>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import PrimeDialog from 'primevue/dialog'
import { confirmAction } from '../utils/feedback'

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

function handleVisible(visible: boolean) {
  if (!visible) void requestClose()
}

function restoreFocus() {
  const replacement = returnFocusSelector ? document.querySelector<HTMLElement>(returnFocusSelector) : null
  const target = replacement || (previousFocus?.isConnected ? previousFocus : null)
  target?.focus()
  previousFocus = null
  returnFocusSelector = ''
}

defineExpose({ requestClose, restoreFocus })
</script>
