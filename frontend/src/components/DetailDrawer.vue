<template>
  <Teleport to="body">
    <Transition name="drawer" @after-leave="restoreFocus">
      <div v-if="open" class="detail-drawer-layer">
        <UiButton class="detail-drawer-scrim" type="button" aria-label="关闭详情" @click="$emit('close')" />
        <aside class="detail-drawer" role="dialog" aria-modal="true" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" @keydown="handleKeydown">
          <header class="detail-drawer-header">
            <div><span v-if="eyebrow">{{ eyebrow }}</span><h2 :id="titleId">{{ title }}</h2><p v-if="description" :id="descriptionId">{{ description }}</p></div>
      <UiButton ref="closeButton" variant="ghost" icon class="icon-button" type="button" aria-label="关闭详情" @click="$emit('close')"><UiIcon name="close" /></UiButton>
          </header>
          <div class="detail-drawer-body"><slot /></div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

let nextDrawerID = 0
const props = withDefaults(defineProps<{ open: boolean; title: string; eyebrow?: string; description?: string; returnFocusSelector?: string }>(), { eyebrow: '', description: '', returnFocusSelector: '' })
const emit = defineEmits<{ close: [] }>()
const closeButton = ref<InstanceType<typeof UiButton> | null>(null)
const titleId = `detail-drawer-${++nextDrawerID}`
const descriptionId = `${titleId}-description`
let previousFocus: HTMLElement | null = null
let returnFocusSelector = ''

watch(() => props.open, async (open, wasOpen) => {
  if (open) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    returnFocusSelector = props.returnFocusSelector
    await nextTick()
    ;(closeButton.value?.$el as HTMLElement | undefined)?.focus()
    return
  }
  if (wasOpen) {
    await nextTick()
    focusPreviousTarget()
  }
}, { immediate: true })

function focusPreviousTarget() {
  const replacement = returnFocusSelector ? document.querySelector<HTMLElement>(returnFocusSelector) : null
  const target = replacement || (previousFocus?.isConnected ? previousFocus : null)
  target?.focus()
}

function restoreFocus() {
  focusPreviousTarget()
  previousFocus = null
  returnFocusSelector = ''
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    emitClose()
    return
  }
  if (event.key !== 'Tab') return
  const drawer = event.currentTarget as HTMLElement
  const focusable = Array.from(drawer.querySelectorAll<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
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

function emitClose() { emit('close') }
</script>
