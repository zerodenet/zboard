import type {
  SubscriptionPolicyGroup,
  SubscriptionPolicyGroupType,
  SubscriptionRenderer,
  SubscriptionTemplateCustomization,
  SubscriptionTemplateRuleSet,
} from '../api/client'

export type SupportedSubscriptionRenderer = Exclude<SubscriptionRenderer, 'unsupported'>

export interface SubscriptionTemplateOutputOption {
  value: SupportedSubscriptionRenderer
  label: string
  description: string
  contentType: string
  icon: string
}

export const subscriptionTemplateOutputOptions: SubscriptionTemplateOutputOption[] = [
  {
    value: 'znet-sink',
    label: 'ZNet Sink',
    description: 'Zero 原生 JSON，公开订阅以 Base64 封装，适合 ZNet Sink 自动同步。',
    contentType: 'application/json',
    icon: 'activity',
  },
  {
    value: 'clash',
    label: 'Clash / Mihomo',
    description: '生成节点、选择组和默认规则的 YAML。',
    contentType: 'application/yaml',
    icon: 'nodes',
  },
  {
    value: 'sing-box',
    label: 'sing-box',
    description: '生成 outbounds、selector 和默认路由 JSON。',
    contentType: 'application/json',
    icon: 'database',
  },
]

export function subscriptionTemplateOutput(renderer: SubscriptionRenderer) {
  return subscriptionTemplateOutputOptions.find(option => option.value === renderer) || null
}

export function subscriptionPolicyGroupTypeOptions(renderer: SubscriptionRenderer) {
  const options = [
    { label: '手动选择', value: 'select' as SubscriptionPolicyGroupType },
    { label: '自动测速', value: 'urltest' as SubscriptionPolicyGroupType },
  ]
  if (renderer === 'clash' || renderer === 'znet-sink') {
    options.push({ label: '故障转移', value: 'fallback' })
  }
  return options
}

export const clashRuleBehaviorOptions = [
  { label: '经典规则', value: 'classical' },
  { label: '域名集合', value: 'domain' },
  { label: 'IP/CIDR 集合', value: 'ipcidr' },
]

export function subscriptionRuleFormatOptions(renderer: SubscriptionRenderer) {
  if (renderer === 'znet-sink') {
    return [
      { label: '域名列表', value: 'domain_list' },
      { label: 'CIDR 列表', value: 'cidr_list' },
      { label: 'Zero Rule IR', value: 'zero_rule_ir' },
      { label: 'ZRS', value: 'zrs' },
    ]
  }
  if (renderer === 'sing-box') {
    return [
      { label: 'Source JSON', value: 'source' },
      { label: 'SRS Binary', value: 'binary' },
    ]
  }
  return [
    { label: 'YAML', value: 'yaml' },
    { label: 'Text', value: 'text' },
    { label: 'MRS', value: 'mrs' },
  ]
}

export function defaultSubscriptionRuleSet(renderer: SubscriptionRenderer): SubscriptionTemplateRuleSet {
  if (renderer === 'znet-sink') {
    return { tag: '', url: '', format: 'domain_list', target: 'group:main', interval: 86400 }
  }
  if (renderer === 'sing-box') {
    return { tag: '', url: '', format: 'source', target: 'group:main', interval: 86400 }
  }
  return { tag: '', url: '', behavior: 'classical', format: 'yaml', target: 'group:main', interval: 86400 }
}

export function defaultSubscriptionCustomization(
  renderer: SubscriptionRenderer,
): SubscriptionTemplateCustomization {
  const main = defaultSubscriptionPolicyGroup('main', '节点选择', 'select')
  main.include_groups = ['auto']
  main.default_group = 'auto'
  const auto = defaultSubscriptionPolicyGroup('auto', '自动选择', 'urltest')
  return {
    version: 2,
    mixed_port: 7890,
    main_group: 'main',
    policy_groups: [main, auto],
    final: 'group:main',
    rule_sets: [],
    advanced_source: '',
  }
}

export function normalizeSubscriptionCustomization(
  renderer: SubscriptionRenderer,
  value?: Record<string, any> | null,
): SubscriptionTemplateCustomization {
  if (Number(value?.version || 0) <= 1 && value && Object.keys(value).length) {
    return normalizeLegacySubscriptionCustomization(renderer, value)
  }
  const fallback = defaultSubscriptionCustomization(renderer)
  const sourceGroups = Array.isArray(value?.policy_groups) ? value.policy_groups : fallback.policy_groups
  const policyGroups = sourceGroups.map((group: Record<string, any>, index: number) => normalizeSubscriptionPolicyGroup(group, index))
  const mainGroup = policyGroups.some((group: SubscriptionPolicyGroup) => group.id === value?.main_group)
    ? String(value?.main_group)
    : policyGroups[0]?.id || 'main'
  const sourceRules = Array.isArray(value?.rule_sets) ? value.rule_sets : []
  return {
    version: 2,
    mixed_port: Number(value?.mixed_port || 7890),
    main_group: mainGroup,
    policy_groups: policyGroups,
    final: normalizeSubscriptionTarget(String(value?.final || ''), mainGroup, false),
    rule_sets: sourceRules.map((rule: Record<string, any>) => rule.rule_set_id
      ? { rule_set_id: rule.rule_set_id, target: normalizeSubscriptionTarget(String(rule.target || rule.action || ''), mainGroup, true) }
      : {
          ...defaultSubscriptionRuleSet(renderer),
          ...rule,
          target: normalizeSubscriptionTarget(String(rule.target || rule.action || ''), mainGroup, true),
          behavior: renderer === 'clash' ? (rule.behavior || 'classical') : undefined,
        }),
    advanced_source: String(value?.advanced_source || ''),
  }
}

export function defaultSubscriptionPolicyGroup(
  id: string,
  name: string,
  type: SubscriptionPolicyGroupType,
): SubscriptionPolicyGroup {
  return {
    id,
    name,
    type,
    include_pattern: '',
    exclude_pattern: '',
    include_groups: [],
    default_group: '',
    probe_url: type === 'urltest' || type === 'fallback' ? 'http://www.gstatic.com/generate_204' : '',
    interval: 300,
    tolerance: type === 'urltest' ? 50 : 0,
  }
}

export function nextSubscriptionPolicyGroupID(groups: SubscriptionPolicyGroup[], prefix = 'group') {
  const used = new Set(groups.map(group => group.id))
  for (let index = 1; index <= 99; index += 1) {
    const candidate = `${prefix}-${index}`
    if (!used.has(candidate)) return candidate
  }
  return `${prefix}-${Date.now().toString(36)}`
}

export function subscriptionTargetOptions(customization: SubscriptionTemplateCustomization, includeReject = true) {
  const options = customization.policy_groups.map(group => ({
    label: group.id === customization.main_group ? `${group.name}（主策略组）` : group.name,
    value: `group:${group.id}`,
  }))
  options.push({ label: '直连', value: 'direct' })
  if (includeReject) options.push({ label: '拦截', value: 'reject' })
  return options
}

function normalizeLegacySubscriptionCustomization(
  renderer: SubscriptionRenderer,
  value: Record<string, any>,
): SubscriptionTemplateCustomization {
  const groupName = String(value.group_name || '节点选择')
  const main = defaultSubscriptionPolicyGroup('main', groupName, 'select')
  const groups = [main]
  if (value.profile === 'balanced') {
    const auto = defaultSubscriptionPolicyGroup('auto', '自动选择', 'urltest')
    main.include_groups = ['auto']
    main.default_group = 'auto'
    groups.push(auto)
    if (renderer === 'clash') {
      main.include_groups.push('failover')
      groups.push(defaultSubscriptionPolicyGroup('failover', '故障转移', 'fallback'))
    }
  }
  return normalizeSubscriptionCustomization(renderer, {
    version: 2,
    main_group: 'main',
    policy_groups: groups,
    final: value.final === 'direct' ? 'direct' : 'group:main',
    rule_sets: Array.isArray(value.rule_sets)
      ? value.rule_sets.map((rule: Record<string, any>) => ({ ...rule, target: normalizeSubscriptionTarget(String(rule.action || ''), 'main', true), action: undefined }))
      : [],
    advanced_source: String(value.advanced_source || ''),
  })
}

function normalizeSubscriptionPolicyGroup(value: Record<string, any>, index: number): SubscriptionPolicyGroup {
  const id = String(value?.id || `group-${index + 1}`).trim().toLowerCase()
  const type: SubscriptionPolicyGroupType = ['select', 'urltest', 'fallback'].includes(value?.type)
    ? value.type
    : 'select'
  return {
    ...defaultSubscriptionPolicyGroup(id, String(value?.name || `策略组 ${index + 1}`), type),
    ...value,
    id,
    name: String(value?.name || `策略组 ${index + 1}`).trim(),
    type,
    include_pattern: String(value?.include_pattern || ''),
    exclude_pattern: String(value?.exclude_pattern || ''),
    include_groups: Array.isArray(value?.include_groups) ? value.include_groups.map(String) : [],
    default_group: String(value?.default_group || ''),
    probe_url: String(value?.probe_url || ''),
    interval: Number(value?.interval || 300),
    tolerance: Number(value?.tolerance || 0),
  }
}

function normalizeSubscriptionTarget(target: string, mainGroup: string, includeReject: boolean) {
  const normalized = target.trim().toLowerCase()
  if (!normalized || normalized === 'proxy') return `group:${mainGroup}`
  if (normalized === 'direct') return 'direct'
  if (includeReject && normalized === 'reject') return 'reject'
  if (normalized.startsWith('group:')) return normalized
  return `group:${normalized}`
}

export function advancedSubscriptionLanguage(renderer: SubscriptionRenderer) {
  return renderer === 'clash' ? 'YAML' : 'JSON'
}
