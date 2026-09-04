import { describe, expect, it } from 'vitest'
import {
  defaultSubscriptionCustomization,
  defaultSubscriptionRuleSet,
  normalizeSubscriptionCustomization,
  subscriptionPolicyGroupIncludesDirect,
  subscriptionPolicyGroupIncludesReject,
  subscriptionRendererSupportsPolicyConfig,
  subscriptionRendererSupportsRuntimeNetwork,
  subscriptionRuleFormatOptions,
  subscriptionTemplateOutput,
  subscriptionTemplateOutputOptions,
  parseSubscriptionCustomizationRaw,
  serializeSubscriptionCustomization,
} from './subscriptionTemplateEditor'

describe('subscription template editor policy', () => {
  it('maps backend-owned renderers to their derived response metadata', () => {
    expect(subscriptionTemplateOutput('clash')).toMatchObject({
      contentType: 'application/yaml',
      mode: 'full',
    })
    expect(subscriptionTemplateOutput('unsupported')).toBeNull()
    expect(subscriptionTemplateOutputOptions.map(option => option.value)).toEqual([
      'zero',
      'clash',
      'sing-box',
      'shadowrocket',
      'quantumult-x',
      'v2rayn',
    ])
    expect(subscriptionTemplateOutput('shadowrocket')).toMatchObject({ mode: 'nodes' })
    expect(subscriptionTemplateOutput('quantumult-x')).toMatchObject({ mode: 'nodes' })
    expect(subscriptionTemplateOutput('v2rayn')).toMatchObject({ mode: 'nodes' })
    expect(subscriptionRendererSupportsPolicyConfig('clash')).toBe(true)
    expect(subscriptionRendererSupportsPolicyConfig('shadowrocket')).toBe(false)
    expect(subscriptionRendererSupportsRuntimeNetwork('zero')).toBe(true)
    expect(subscriptionRendererSupportsRuntimeNetwork('clash')).toBe(true)
    expect(subscriptionRendererSupportsRuntimeNetwork('sing-box')).toBe(true)
    expect(subscriptionRendererSupportsRuntimeNetwork('shadowrocket')).toBe(false)
  })

  it('provides renderer-specific rule defaults without exposing Go templates', () => {
    expect(defaultSubscriptionCustomization('clash')).toMatchObject({
      version: 3,
      mode: 'rule',
      mixed_enabled: true,
      mixed_port: 7890,
      main_group: 'main',
      final: 'group:main',
      rule_sets: [],
    })
    expect(defaultSubscriptionCustomization('sing-box').policy_groups).toMatchObject([
      { id: 'main', name: '节点选择', type: 'select', include_groups: ['auto'] },
      { id: 'auto', name: '自动选择', type: 'urltest' },
    ])
    expect(normalizeSubscriptionCustomization('clash', { version: 1, group_name: 'Legacy', final: 'proxy', rule_sets: [] })).toMatchObject({
      version: 3,
      mode: 'rule',
      mixed_enabled: true,
      mixed_port: 7890,
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

  it('defaults special select targets on and preserves explicit opt-out', () => {
    const defaultMain = defaultSubscriptionCustomization('clash').policy_groups[0] as any
    expect(defaultMain.include_direct).toBe(true)
    expect(defaultMain.include_reject).toBe(true)
    expect(subscriptionPolicyGroupIncludesDirect(defaultMain)).toBe(true)
    expect(subscriptionPolicyGroupIncludesReject(defaultMain)).toBe(true)

    const legacyV2 = normalizeSubscriptionCustomization('clash', {
      version: 2,
      mixed_port: 7890,
      main_group: 'main',
      policy_groups: [{ id: 'main', name: '节点选择', type: 'select' }],
      final: 'group:main',
      rule_sets: [],
    })
    expect((legacyV2.policy_groups[0] as any).include_direct).toBe(true)
    expect((legacyV2.policy_groups[0] as any).include_reject).toBe(true)

    const explicit = normalizeSubscriptionCustomization('clash', {
      version: 2,
      mixed_port: 7890,
      main_group: 'main',
      policy_groups: [{ id: 'main', name: '节点选择', type: 'select', include_direct: false, include_reject: true }],
      final: 'group:main',
      rule_sets: [],
    })
    expect((explicit.policy_groups[0] as any).include_direct).toBe(false)
    expect((explicit.policy_groups[0] as any).include_reject).toBe(true)
  })

  it('round-trips the complete raw model back into visual state', () => {
    const customization = defaultSubscriptionCustomization('sing-box')
    customization.mode = 'global'
    customization.system_proxy = true
    customization.dns.enabled = true
    customization.tun.enabled = true
    const parsed = parseSubscriptionCustomizationRaw('sing-box', serializeSubscriptionCustomization(customization))
    expect(parsed).toMatchObject({
      version: 3,
      mode: 'global',
      mixed_enabled: true,
      system_proxy: true,
      dns: { enabled: true },
      tun: { enabled: true },
    })
    expect(() => parseSubscriptionCustomizationRaw('sing-box', '{')).toThrow(/Raw JSON/)
  })
})
