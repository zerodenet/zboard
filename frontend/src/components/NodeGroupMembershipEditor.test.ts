import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { ProtocolEndpointNodeGroupMembership } from '../api/client'
import NodeGroupMembershipEditor from './NodeGroupMembershipEditor.vue'

const candidate = {
  id: 9,
  name: 'Hong Kong Premium',
  code: 'hong-kong-premium',
  description: 'Premium delivery group',
  is_enabled: true,
  revision: 7,
  protocol_endpoint_count: 12,
  plan_count: 2,
  created_at: '2026-08-04T00:00:00Z',
  updated_at: '2026-08-04T00:00:00Z',
}

const NodeGroupLookupStub = defineComponent({
  props: { modelValue: Number, disabled: Boolean },
  emits: ['update:modelValue', 'select'],
  template: '<button data-lookup type="button" :disabled="disabled" @click="$emit(\'update:modelValue\', 9); $emit(\'select\', candidate)">选择节点组</button>',
  setup() { return { candidate } },
})

const UiButtonStub = defineComponent({
  props: { disabled: Boolean },
  emits: ['click'],
  template: '<button type="button" :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
})

const UiIconStub = defineComponent({ template: '<span />' })

function membership(id = 2): ProtocolEndpointNodeGroupMembership {
  return {
    node_group_id: id,
    name: `Group ${id}`,
    code: `group-${id}`,
    description: '',
    is_enabled: true,
    revision: 3,
    sort_order: 1,
  }
}

function mountEditor(props: { modelValue: ProtocolEndpointNodeGroupMembership[]; canAdd?: boolean }) {
  return mount(NodeGroupMembershipEditor, {
    props,
    global: {
      stubs: {
        NodeGroupLookup: NodeGroupLookupStub,
        UiButton: UiButtonStub,
        UiIcon: UiIconStub,
      },
    },
  })
}

describe('NodeGroupMembershipEditor', () => {
  it('adds the selected group with its loaded revision and no invented group order', async () => {
    const wrapper = mountEditor({ modelValue: [] })
    await wrapper.get('[data-lookup]').trigger('click')
    const buttons = wrapper.findAll('button')
    await buttons[1].trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([{
      node_group_id: 9,
      name: candidate.name,
      code: candidate.code,
      description: candidate.description,
      is_enabled: true,
      revision: 7,
      sort_order: 0,
    }])
  })

  it('blocks additions while inactive but still permits removing an existing relation', async () => {
    const wrapper = mountEditor({ modelValue: [membership()], canAdd: false })
    expect(wrapper.get('[data-lookup]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('协议服务停用时不能新增节点组关联')

    const buttons = wrapper.findAll('button')
    expect(buttons[1].attributes('disabled')).toBeDefined()
    await buttons[2].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([])
  })
})
