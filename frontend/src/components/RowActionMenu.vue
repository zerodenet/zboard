<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child>
      <Tooltip :text="label"><UiButton
        ref="trigger"
        variant="ghost"
        size="sm"
        icon
        type="button"
        :aria-label="label"
        :data-row-action-trigger="triggerKey || undefined"
      ><UiIcon name="more" /></UiButton></Tooltip>
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent class="ui-dropdown-content" align="end" :side-offset="5" :collision-padding="16" @close-auto-focus="restoreFocus">
        <RowActionItems :actions="actions" />
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { computed, defineComponent, Fragment, h, nextTick, ref, useSlots, type VNode } from 'vue'
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import Tooltip from './ui/Tooltip.vue'

withDefaults(defineProps<{ label?: string; triggerKey?: string }>(), { label: '更多操作', triggerKey: '' })
const slots = useSlots()
const trigger = ref<InstanceType<typeof UiButton> | null>(null)
async function restoreFocus(event: Event) {
  event.preventDefault()
  // An action may have opened a dialog in this render. Let its focus scope
  // mount before restoring focus, and never pull focus out of that dialog.
  await nextTick()
  const target = trigger.value?.$el as HTMLElement | undefined
  const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"]')
  if (Array.from(dialogs).some(dialog => target && !dialog.contains(target))) return
  target?.focus()
}
function flatten(nodes: VNode[]): VNode[] {
  return nodes.flatMap(node => node.type === Fragment && Array.isArray(node.children)
    ? flatten(node.children as VNode[])
    : typeof node.type === 'symbol' ? [] : [node])
}
const actions = computed(() => flatten(slots.default?.() || []))
const RowActionItems = defineComponent({
  props: { actions: { type: Array as () => VNode[], required: true } },
  setup(props) {
    return () => props.actions.map((action, index) => h(DropdownMenuItem, {
      asChild: true, key: index, class: 'ui-dropdown-item',
    }, () => action))
  },
})
</script>
