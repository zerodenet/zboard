import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { SitePolicyDocument } from '../utils/siteProfile'
import PolicyDocumentsEditor from './PolicyDocumentsEditor.vue'

const defaults: SitePolicyDocument[] = [
  { slug: 'terms', title: '服务条款', summary: '', content: '# 服务条款', published: true, placements: ['footer', 'auth', 'purchase'] },
  { slug: 'privacy', title: '隐私政策', summary: '', content: '# 隐私政策', published: true, placements: ['footer', 'auth'] },
  { slug: 'refund', title: '退款政策', summary: '', content: '# 退款政策', published: true, placements: ['footer', 'purchase'] },
]

describe('PolicyDocumentsEditor', () => {
  it.each(['', 'null', null])('projects legacy defaults into editable cards for an unset collection (%s)', modelValue => {
    const wrapper = shallowMount(PolicyDocumentsEditor, {
      props: { modelValue, fallbackDocuments: defaults },
    })

    expect(wrapper.findAll('.policy-document-card')).toHaveLength(3)
    expect(wrapper.text()).toContain('服务条款')
    expect(wrapper.text()).toContain('隐私政策')
    expect(wrapper.text()).toContain('退款政策')
  })

  it('keeps an explicitly empty collection empty', () => {
    const wrapper = shallowMount(PolicyDocumentsEditor, {
      props: { modelValue: '[]', fallbackDocuments: defaults },
    })

    expect(wrapper.findAll('.policy-document-card')).toHaveLength(0)
    expect(wrapper.text()).toContain('还没有政策文档')
  })
})
