<template>
  <span ref="root" class="row-action-menu">
    <UiButton
      ref="trigger"
      variant="ghost"
      size="sm"
      icon
      type="button"
      :aria-label="label"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-controls="menuId"
      :data-row-action-trigger="triggerKey || undefined"
      @click="toggle"
      @keydown.down.prevent="openAndFocus"
      @keydown.escape.prevent="close(true)"
    >
      <UiIcon name="more" />
    </UiButton>
    <Teleport to="body">
      <div
        v-if="open"
        :id="menuId"
        ref="popover"
        class="row-action-popover"
        role="menu"
        :style="position"
        @click="handleActionClick"
        @keydown="handleMenuKeydown"
      >
        <slot />
      </div>
    </Teleport>
  </span>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

let nextMenuID = 0
const props = withDefaults(defineProps<{ label?: string; triggerKey?: string }>(), { label: '更多操作', triggerKey: '' })
const root = ref<HTMLElement | null>(null)
const trigger = ref<InstanceType<typeof UiButton> | null>(null)
const popover = ref<HTMLElement | null>(null)
const open = ref(false)
const menuId = `row-action-menu-${++nextMenuID}`
const position = reactive({ top: '0px', left: '0px' })

function triggerElement() {
  return trigger.value?.$el as HTMLElement | undefined
}

function menuItems() {
  return Array.from(popover.value?.querySelectorAll<HTMLElement>(
    '[role="menuitem"]:not([aria-disabled="true"]), a[href], button:not([disabled])'
  ) || [])
}

async function show(focusFirst = false) {
  open.value = true
  await nextTick()
  updatePosition()
  if (focusFirst) menuItems()[0]?.focus()
}

function toggle() {
  if (open.value) close()
  else void show()
}

function openAndFocus() {
  void show(true)
}

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) void nextTick(() => triggerElement()?.focus())
}

function updatePosition() {
  const target = triggerElement()?.getBoundingClientRect()
  const menu = popover.value?.getBoundingClientRect()
  if (!target || !menu) return
  const gap = 5
  const viewportPadding = 8
  const left = Math.max(viewportPadding, Math.min(window.innerWidth - menu.width - viewportPadding, target.right - menu.width))
  const preferredTop = target.bottom + gap
  const top = preferredTop + menu.height <= window.innerHeight - viewportPadding
    ? preferredTop
    : Math.max(viewportPadding, target.top - menu.height - gap)
  position.left = `${Math.round(left)}px`
  position.top = `${Math.round(top)}px`
}

function handleActionClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (target.closest('a,button,[role="menuitem"]')) close()
}

function handleMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    close(true)
    return
  }
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  const items = menuItems()
  if (!items.length) return
  const current = items.indexOf(document.activeElement as HTMLElement)
  const next = event.key === 'ArrowDown'
    ? (current + 1) % items.length
    : (current <= 0 ? items.length - 1 : current - 1)
  items[next]?.focus()
}

function handleOutsidePointer(event: PointerEvent) {
  const target = event.target as Node
  if (!root.value?.contains(target) && !popover.value?.contains(target)) close()
}

function closeOnViewportChange() {
  close()
}

onMounted(() => {
  document.addEventListener('pointerdown', handleOutsidePointer)
  window.addEventListener('resize', closeOnViewportChange)
  window.addEventListener('scroll', closeOnViewportChange, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleOutsidePointer)
  window.removeEventListener('resize', closeOnViewportChange)
  window.removeEventListener('scroll', closeOnViewportChange, true)
})
</script>
