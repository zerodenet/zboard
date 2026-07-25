<template>
  <span ref="root" class="workbench-filter-chip" :class="{ active }">
    <UiButton
      ref="trigger"
      class="workbench-filter-chip-trigger"
      :variant="active ? 'secondary' : 'ghost'"
      size="sm"
      type="button"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="popoverId"
      @click="toggle"
      @keydown.down.prevent="show(true)"
      @keydown.escape.prevent="close(true)"
    >
      <UiIcon v-if="!active" name="plus" />
      <span>{{ label }}</span>
      <strong v-if="active && valueLabel">{{ valueLabel }}</strong>
      <UiIcon v-if="active" name="chevron" />
    </UiButton>
    <UiButton
      v-if="active && clearable"
      class="workbench-filter-chip-clear"
      variant="ghost"
      size="sm"
      icon
      type="button"
      :aria-label="`清除${label}筛选`"
      @click.stop="$emit('clear')"
    >
      <UiIcon name="close" />
    </UiButton>

    <Teleport to="body">
      <section
        v-if="open"
        :id="popoverId"
        ref="popover"
        class="workbench-filter-popover"
        :class="{ 'workbench-filter-popover-wide': wide }"
        role="dialog"
        :aria-labelledby="headingId"
        :style="position"
        @keydown.escape.prevent="close(true)"
      >
        <header>
          <strong :id="headingId">{{ label }}</strong>
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

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

let nextFilterChipID = 0

withDefaults(defineProps<{
  label: string
  active?: boolean
  valueLabel?: string
  clearable?: boolean
  wide?: boolean
}>(), {
  active: false,
  valueLabel: '',
  clearable: true,
  wide: false,
})

const emit = defineEmits<{ clear: []; open: []; close: [] }>()

const root = ref<HTMLElement | null>(null)
const trigger = ref<InstanceType<typeof UiButton> | null>(null)
const popover = ref<HTMLElement | null>(null)
const open = ref(false)
const instanceID = ++nextFilterChipID
const popoverId = `workbench-filter-popover-${instanceID}`
const headingId = `workbench-filter-heading-${instanceID}`
const position = reactive({ top: '0px', left: '0px' })

function triggerElement() {
  return trigger.value?.$el as HTMLElement | undefined
}

async function show(focusFirst = false) {
  if (open.value) return
  emit('open')
  open.value = true
  await nextTick()
  updatePosition()
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

defineExpose({ close, show })
</script>
