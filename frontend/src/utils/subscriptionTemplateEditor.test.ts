import { describe, expect, it } from 'vitest'
import {
  defaultSubscriptionCustomization,
  defaultSubscriptionRuleSet,
  normalizeSubscriptionCustomization,
  subscriptionRuleFormatOptions,
  subscriptionTemplateOutput,
  subscriptionTemplateOutputOptions,
} from './subscriptionTemplateEditor'

describe('subscription template editor policy', () => {
  it('maps only backend-owned renderers to their derived response metadata', () => {
    expect(subscriptionTemplateOutput('clash')).toMatchObject({
      contentType: 'application/yaml',
    })
    expect(subscriptionTemplateOutput('unsupported')).toBeNull()
    expect(subscriptionTemplateOutputOptions.map(option => option.value)).toEqual([
      'znet-sink',
      'clash',
      'sing-box',
    ])
  })

  it('provides renderer-specific rule defaults without exposing Go templates', () => {
    expect(defaultSubscriptionCustomization('clash')).toMatchObject({
      version: 2,
      main_group: 'main',
      final: 'group:main',
      rule_sets: [],
    })
    expect(defaultSubscriptionCustomization('sing-box').policy_groups).toMatchObject([
      { id: 'main', name: '节点选择', type: 'select', include_groups: ['auto'] },
      { id: 'auto', name: '自动选择', type: 'urltest' },
    ])
    expect(normalizeSubscriptionCustomization('clash', { version: 1, group_name: 'Legacy', final: 'proxy', rule_sets: [] })).toMatchObject({
      version: 2,
      main_group: 'main',
      policy_groups: [{ id: 'main', name: 'Legacy', type: 'select' }],
    })
    expect(normalizeSubscriptionCustomization('clash', {
      version: 1,
      group_name: 'Library',
      final: 'proxy',
      rule_sets: [{ rule_set_id: 7, action: 'reject', tag: 'must-not-be-copied' }],
    })).toMatchObject({
      rule_sets: [{ rule_set_id: 7, target: 'reject' }],
    })
    expect(defaultSubscriptionRuleSet('sing-box')).toMatchObject({ format: 'source', interval: 86400 })
    expect(subscriptionRuleFormatOptions('znet-sink').map(option => option.value)).toEqual([
      'domain_list',
      'cidr_list',
      'zero_rule_ir',
      'zrs',
    ])
  })
})
