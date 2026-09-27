<template>
  <span ref="root" class="workbench-filter-chip" :class="{ active }">
    <button
      ref="trigger"
      class="workbench-filter-chip-trigger"
      type="button"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="popoverId"
      @click="toggle"
      @keydown.down.prevent="show(true)"
      @keydown.escape.prevent="close(true)"
    >
      <slot name="icon"><UiIcon v-if="!active" :name="icon" /></slot>
      <span>{{ label }}</span>
      <strong v-if="active && valueLabel">{{ valueLabel }}</strong>
      <UiIcon v-if="active" name="chevron" />
    </button>
    <button
      v-if="active && clearable"
      class="workbench-filter-chip-clear"
      type="button"
      :aria-label="`清除${label}筛选`"
      @click.stop="$emit('clear')"
    >
      <UiIcon name="close" />
    </button>

    <Teleport to="body">
      <section
        v-if="open"
        :id="popoverId"
        ref="popover"
        class="workbench-filter-popover"
        :class="[panelClass, { 'workbench-filter-popover-wide': wide, 'workbench-filter-popover-compact': compact, 'workbench-filter-popover-plain': !showHeader }]"
        role="dialog"
        :aria-labelledby="showHeader ? headingId : undefined"
        :aria-label="showHeader ? undefined : label"
        :style="position"
        @keydown.escape.prevent="close(true)"
      >
        <header v-if="showHeader">
          <slot name="header" :close="close"><strong :id="headingId">{{ label }}</strong></slot>
          <UiButton variant="ghost" size="sm" icon type="button" aria-label="关闭筛选浮层" @click="close(true)">
            <UiIcon name="close" />
          </UiButton>
        </header>
        <div class="workbench-filter-popover-body">
          <slot :close="close" />
        </div>
      </section>
    </Teleport>
  </span>
</template>

<script lang="ts">
let nextFilterChipID = 0
</script>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  label: string
  active?: boolean
  valueLabel?: string
  icon?: string
  clearable?: boolean
  wide?: boolean
  compact?: boolean
  showHeader?: boolean
  panelClass?: string
}>(), {
  active: false,
  valueLabel: '',
  icon: 'plus',
  clearable: true,
  wide: false,
  compact: false,
  showHeader: false,
  panelClass: '',
})

const emit = defineEmits<{ clear: []; open: []; close: [] }>()

const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const popover = ref<HTMLElement | null>(null)
const open = ref(false)
const instanceID = ++nextFilterChipID
const popoverId = `workbench-filter-popover-${instanceID}`
const headingId = `workbench-filter-heading-${instanceID}`
const position = reactive({ top: '0px', left: '0px' })
let resizeObserver: ResizeObserver | undefined

function triggerElement() { return trigger.value || undefined }

async function show(focusFirst = false) {
  if (open.value) return
  emit('open')
  open.value = true
  await nextTick()
  if (!open.value || !popover.value) return
  updatePosition()
  resizeObserver = new ResizeObserver(updatePosition)
  resizeObserver.observe(popover.value)
  if (focusFirst) {
    popover.value?.querySelector<HTMLElement>('button:not([disabled]),input:not([disabled]),[tabindex="0"]')?.focus()
  }
}

function toggle() {
  if (open.value) close()
  else void show()
}

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  resizeObserver?.disconnect()
  resizeObserver = undefined
  emit('close')
  if (restoreFocus) void nextTick(() => triggerElement()?.focus())
}

function updatePosition() {
  const target = triggerElement()?.getBoundingClientRect()
  const panel = popover.value?.getBoundingClientRect()
  if (!target || !panel) return
  const gap = 6
  const viewportPadding = 8
  const left = Math.max(
    viewportPadding,
    Math.min(window.innerWidth - panel.width - viewportPadding, target.left),
  )
  const preferredTop = target.bottom + gap
  const top = preferredTop + panel.height <= window.innerHeight - viewportPadding
    ? preferredTop
    : Math.max(viewportPadding, target.top - panel.height - gap)
  position.left = `${Math.round(left)}px`
  position.top = `${Math.round(top)}px`
}

function containsTarget(target: EventTarget | null) {
  if (!(target instanceof Node)) return false
  if (root.value?.contains(target) || popover.value?.contains(target)) return true
  // Select menus can be teleported outside the dialog; keep their owning filter open.
  return Array.from(popover.value?.querySelectorAll('[aria-controls]') || []).some(control =>
    (control.getAttribute('aria-controls') || '').split(/\s+/).some(id =>
      id && document.getElementById(id)?.contains(target),
    ),
  )
}

function handleOutsidePointer(event: PointerEvent) {
  if (!containsTarget(event.target)) close()
}

function handleScroll(event: Event) {
  if (!containsTarget(event.target)) close()
}

function closeOnViewportChange() {
  close()
}

onMounted(() => {
  document.addEventListener('pointerdown', handleOutsidePointer)
  window.addEventListener('resize', closeOnViewportChange)
  window.addEventListener('scroll', handleScroll, true)
})

onBeforeUnmount(() => {
  open.value = false
  resizeObserver?.disconnect()
  document.removeEventListener('pointerdown', handleOutsidePointer)
  window.removeEventListener('resize', closeOnViewportChange)
  window.removeEventListener('scroll', handleScroll, true)
})

defineExpose({ close, show })
</script>

<style scoped>
.workbench-filter-chip,
.workbench-filter-chip.active { border: 0; border-radius: 0; background: transparent; }
.workbench-filter-chip-trigger { min-height: 28px; display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border: 1px dashed var(--border); border-radius: 999px; color: var(--text-secondary); background: transparent; font-size: 11px; font-weight: 500; line-height: 1.25; white-space: nowrap; cursor: pointer; }
.workbench-filter-chip-trigger:hover, .workbench-filter-chip-trigger[aria-expanded='true'] { border-color: var(--input); color: var(--foreground); background: var(--surface-subtle); }
.workbench-filter-chip-trigger:focus-visible, .workbench-filter-chip-clear:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.workbench-filter-chip-trigger :deep(.ui-icon) { width: 12px; height: 12px; color: var(--muted-foreground); }
.workbench-filter-chip-trigger strong { max-width: 150px; overflow: hidden; color: var(--foreground); font-weight: 500; text-overflow: ellipsis; }
.workbench-filter-chip-trigger strong::before { content: ': '; color: var(--muted-foreground); font-weight: 400; }
.workbench-filter-chip.active .workbench-filter-chip-trigger { border-style: solid; border-color: var(--border); border-radius: 999px 0 0 999px; background: var(--surface-subtle); }
.workbench-filter-chip.active .workbench-filter-chip-trigger :deep(.ui-icon:last-child) { width: 10px; height: 10px; transform: rotate(90deg); }
.workbench-filter-chip-clear { width: 25px; min-height: 28px; display: grid; place-items: center; padding: 0 5px 0 2px; border: 1px solid var(--border); border-left: 0; border-radius: 0 999px 999px 0; color: var(--muted-foreground); background: var(--surface-subtle); cursor: pointer; }
.workbench-filter-chip-clear:hover { color: var(--foreground); background: var(--muted-surface); }
.workbench-filter-chip-clear :deep(.ui-icon) { width: 11px; height: 11px; }
.workbench-filter-popover {
  display: flex;
  flex-direction: column;
  max-height: calc(100dvh - 16px);
  border-radius: 8px;
  box-shadow: 0 8px 24px var(--card-shadow);
}
.workbench-filter-popover-compact { width: min(216px, calc(100vw - 16px)); }
.workbench-filter-popover-compact .workbench-filter-popover-body { padding: 5px; }
.workbench-filter-popover > header {
  flex-shrink: 0;
}
.workbench-filter-popover-body {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}
</style>
