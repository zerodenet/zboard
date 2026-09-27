<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child>
      <UiButton
        ref="trigger"
        variant="ghost"
        size="sm"
        icon
        type="button"
        :aria-label="label"
        :title="label"
        :data-row-action-trigger="triggerKey || undefined"
      ><UiIcon name="more" /></UiButton>
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent class="ui-dropdown-content" align="end" :side-offset="5" @close-auto-focus="restoreFocus">
        <RowActionItems :actions="actions" />
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { computed, defineComponent, Fragment, h, ref, useSlots, type VNode } from 'vue'
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{ label?: string; triggerKey?: string }>(), { label: '更多操作', triggerKey: '' })
const slots = useSlots()
const trigger = ref<InstanceType<typeof UiButton> | null>(null)
function restoreFocus(event: Event) {
  event.preventDefault()
  ;(trigger.value?.$el as HTMLElement | undefined)?.focus()
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
