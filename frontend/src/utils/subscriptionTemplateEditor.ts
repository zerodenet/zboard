import type {
  SubscriptionPolicyGroup,
  SubscriptionPolicyGroupType,
  SubscriptionRenderer,
  SubscriptionTemplateCustomization,
  SubscriptionTemplateRuleSet,
} from '../api/client'

export type SupportedSubscriptionRenderer = Exclude<SubscriptionRenderer, 'unsupported'> | 'zero' | 'shadowrocket' | 'quantumult-x' | 'v2rayn'

type RendererInput = SubscriptionRenderer | SupportedSubscriptionRenderer | 'znet-sink'
type EditableSubscriptionPolicyGroup = SubscriptionPolicyGroup & {
  include_direct?: boolean
  include_reject?: boolean
}

export interface SubscriptionTemplateOutputOption {
  value: SupportedSubscriptionRenderer
  label: string
  description: string
  contentType: string
  icon: string
  mode: 'full' | 'nodes'
}

function canonicalRenderer(renderer: RendererInput): SupportedSubscriptionRenderer | 'unsupported' {
  const normalized = String(renderer || '').trim().toLowerCase()
  if (['zero', 'znet-sink', 'znet_sink', 'znetsink', 'zero-json', 'zero-base64-json'].includes(normalized)) return 'zero'
  if (normalized === 'clash-yaml') return 'clash'
  if (normalized === 'singbox') return 'sing-box'
  if (normalized === 'quantumultx' || normalized === 'quantumult_x') return 'quantumult-x'
  if (normalized === 'v2ray-n' || normalized === 'v2ray_n') return 'v2rayn'
  if (['clash', 'sing-box', 'shadowrocket', 'quantumult-x', 'v2rayn'].includes(normalized)) return normalized as SupportedSubscriptionRenderer
  return 'unsupported'
}

export const subscriptionTemplateOutputOptions: SubscriptionTemplateOutputOption[] = [
  {
    value: 'zero',
    label: 'Zero',
    description: 'Base64 编码的 Zero JSON，适合 ZNet Sink 和其他 Zero 客户端。',
    contentType: 'text/plain',
    icon: 'activity',
    mode: 'full',
  },
  {
    value: 'clash',
    label: 'Clash / Mihomo',
    description: '生成节点、策略组和规则的完整 YAML 配置。',
    contentType: 'application/yaml',
    icon: 'nodes',
    mode: 'full',
  },
  {
    value: 'sing-box',
    label: 'sing-box',
    description: '生成 outbounds、selector 和路由规则的完整 JSON 配置。',
    contentType: 'application/json',
    icon: 'database',
    mode: 'full',
  },
  {
    value: 'shadowrocket',
    label: 'Shadowrocket',
    description: '生成标准协议分享链接订阅；策略与分组由客户端侧管理。',
    contentType: 'text/plain',
    icon: 'activity',
    mode: 'nodes',
  },
  {
    value: 'quantumult-x',
    label: 'Quantumult X',
    description: '生成 Quantumult X server_remote 节点资源；策略与分流由客户端侧管理。',
    contentType: 'text/plain',
    icon: 'nodes',
    mode: 'nodes',
  },
  {
    value: 'v2rayn',
    label: 'v2rayN',
    description: '生成标准协议分享链接订阅；策略与分组由客户端侧管理。',
    contentType: 'text/plain',
    icon: 'activity',
    mode: 'nodes',
  },
]

export function subscriptionTemplateOutput(renderer: RendererInput) {
  const canonical = canonicalRenderer(renderer)
  return subscriptionTemplateOutputOptions.find(option => option.value === canonical) || null
}

export function subscriptionRendererSupportsPolicyConfig(renderer: RendererInput) {
  return subscriptionTemplateOutput(renderer)?.mode === 'full'
}

export function subscriptionPolicyGroupTypeOptions(renderer: RendererInput) {
  const canonical = canonicalRenderer(renderer)
  if (!subscriptionRendererSupportsPolicyConfig(renderer)) return []
  const options = [
    { label: '手动选择', value: 'select' as SubscriptionPolicyGroupType },
    { label: '自动测速', value: 'urltest' as SubscriptionPolicyGroupType },
  ]
  if (canonical === 'clash' || canonical === 'zero') {
    options.push({ label: '故障转移', value: 'fallback' })
  }
  return options
}

export const clashRuleBehaviorOptions = [
  { label: '经典规则', value: 'classical' },
  { label: '域名集合', value: 'domain' },
  { label: 'IP/CIDR 集合', value: 'ipcidr' },
]

export function subscriptionRuleFormatOptions(renderer: RendererInput) {
  const canonical = canonicalRenderer(renderer)
  if (canonical === 'zero') {
    return [
      { label: '域名列表', value: 'domain_list' },
      { label: 'CIDR 列表', value: 'cidr_list' },
      { label: 'Zero Rule IR', value: 'zero_rule_ir' },
      { label: 'ZRS', value: 'zrs' },
    ]
  }
  if (canonical === 'sing-box') {
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

export function defaultSubscriptionRuleSet(renderer: RendererInput): SubscriptionTemplateRuleSet {
  const canonical = canonicalRenderer(renderer)
  if (canonical === 'zero') {
    return { tag: '', url: '', format: 'domain_list', target: 'group:main', interval: 86400 }
  }
  if (canonical === 'sing-box') {
    return { tag: '', url: '', format: 'source', target: 'group:main', interval: 86400 }
  }
  return { tag: '', url: '', behavior: 'classical', format: 'yaml', target: 'group:main', interval: 86400 }
}

export function defaultSubscriptionCustomization(
  renderer: RendererInput,
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
  renderer: RendererInput,
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
          behavior: canonicalRenderer(renderer) === 'clash' ? (rule.behavior || 'classical') : undefined,
        }),
    advanced_source: String(value?.advanced_source || ''),
  }
}

export function defaultSubscriptionPolicyGroup(
  id: string,
  name: string,
  type: SubscriptionPolicyGroupType,
): SubscriptionPolicyGroup {
  const group: EditableSubscriptionPolicyGroup = {
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
  if (type === 'select') {
    group.include_direct = true
    group.include_reject = true
  }
  return group
}

export function subscriptionPolicyGroupIncludesDirect(group: SubscriptionPolicyGroup) {
  return group.type === 'select' && (group as EditableSubscriptionPolicyGroup).include_direct !== false
}

export function subscriptionPolicyGroupIncludesReject(group: SubscriptionPolicyGroup) {
  return group.type === 'select' && (group as EditableSubscriptionPolicyGroup).include_reject !== false
}

export function setSubscriptionPolicyGroupSpecialTarget(
  group: SubscriptionPolicyGroup,
  target: 'direct' | 'reject',
  enabled: boolean,
) {
  const editable = group as EditableSubscriptionPolicyGroup
  if (target === 'direct') editable.include_direct = enabled
  else editable.include_reject = enabled
}

export function initializeSubscriptionPolicyGroupSpecialTargets(group: SubscriptionPolicyGroup) {
  const editable = group as EditableSubscriptionPolicyGroup
  if (group.type !== 'select') {
    delete editable.include_direct
    delete editable.include_reject
    return
  }
  if (editable.include_direct === undefined) editable.include_direct = true
  if (editable.include_reject === undefined) editable.include_reject = true
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
  renderer: RendererInput,
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
    if (canonicalRenderer(renderer) === 'clash') {
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
  const group: EditableSubscriptionPolicyGroup = {
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
  if (type === 'select') {
    group.include_direct = value?.include_direct !== false
    group.include_reject = value?.include_reject !== false
  } else {
    delete group.include_direct
    delete group.include_reject
  }
  return group
}

function normalizeSubscriptionTarget(target: string, mainGroup: string, includeReject: boolean) {
  const normalized = target.trim().toLowerCase()
  if (!normalized || normalized === 'proxy') return `group:${mainGroup}`
  if (normalized === 'direct') return 'direct'
  if (includeReject && normalized === 'reject') return 'reject'
  if (normalized.startsWith('group:')) return normalized
  return `group:${normalized}`
}

export function advancedSubscriptionLanguage(renderer: RendererInput) {
  return canonicalRenderer(renderer) === 'clash' ? 'YAML' : 'JSON'
}
