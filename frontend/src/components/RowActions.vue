<script lang="ts">
import {
  cloneVNode,
  Comment,
  defineComponent,
  Fragment,
  h,
  Text,
  type VNode,
} from 'vue'
import RowActionMenu from './RowActionMenu.vue'

function collectActions(nodes: VNode[], result: VNode[] = []) {
  for (const node of nodes) {
    if (node.type === Comment) continue
    if (node.type === Text) {
      if (String(node.children || '').trim()) result.push(node)
      continue
    }
    if (node.type === Fragment && Array.isArray(node.children)) {
      collectActions(node.children as VNode[], result)
      continue
    }
    result.push(node)
  }
  return result
}

export default defineComponent({
  name: 'RowActions',
  props: {
    label: { type: String, default: '更多操作' },
    triggerKey: { type: String, default: '' },
  },
  setup(props, { slots }) {
    return () => {
      const actions = collectActions(slots.default?.() || [])
      if (!actions.length) return null

      const mode = actions.length === 1 ? 'single' : 'menu'
      const content = mode === 'single'
        ? actions
        : [
            h(
              RowActionMenu,
              { label: props.label, triggerKey: props.triggerKey },
              {
                default: () => actions.map(action => cloneVNode(action, {
                  role: 'menuitem',
                  tabindex: -1,
                })),
              },
            ),
          ]

      return h('div', {
        class: 'cell-actions row-actions',
        'data-row-action-mode': mode,
        'data-row-action-count': actions.length,
      }, content)
    }
  },
})
</script>
