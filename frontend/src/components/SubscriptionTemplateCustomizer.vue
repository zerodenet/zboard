<template>
  <div class="subscription-customizer">
    <div class="customizer-header">
      <div>
        <strong>个性化配置</strong>
        <span>策略组会生成客户端原生节点组；所有协议配置默认按名称注入，可用正则进一步筛选。</span>
      </div>
      <div class="customizer-counts">
        <StatusBadge tone="info" icon="nodes">{{ model.policy_groups.length }} 个策略组</StatusBadge>
        <StatusBadge tone="neutral" icon="audit">{{ model.rule_sets.length }} 个规则集</StatusBadge>
      </div>
    </div>
    <div v-if="error" data-form-error tabindex="-1">
      <PageAlert tone="danger" title="个性化配置校验失败">{{ error }}</PageAlert>
    </div>

    <UiTabs v-model="activeTab" label="订阅模板配置方式" :items="tabs" />

    <div v-if="activeTab === 'basic'" class="customizer-basic">
      <SubscriptionRuntimeCustomizer v-model="model" :renderer="renderer" />

      <div class="section-heading">
        <div>
          <strong>策略组</strong>
          <span>名称会成为客户端中的组名；类型和参数仅按当前输出格式支持的能力生成。</span>
        </div>
        <UiButton type="button" variant="secondary" size="sm" :disabled="model.policy_groups.length >= 16" @click="addPolicyGroup">
          <UiIcon name="plus" />添加策略组
        </UiButton>
      </div>

      <div class="policy-group-list">
        <section v-for="(group, index) in model.policy_groups" :key="group.id" class="policy-group-row">
          <header>
            <div>
              <span class="group-index">{{ index + 1 }}</span>
              <strong>{{ group.name || '未命名策略组' }}</strong>
              <StatusBadge v-if="group.id === model.main_group" tone="success" icon="check">主策略组</StatusBadge>
              <StatusBadge v-else tone="neutral">{{ groupTypeLabel(group.type) }}</StatusBadge>
            </div>
            <div class="group-actions">
              <UiButton type="button" variant="ghost" size="sm" :aria-label="`上移策略组 ${group.name || index + 1}`" title="上移" :disabled="index === 0" @click="movePolicyGroup(index, -1)"><UiIcon name="arrow-up" /></UiButton>
              <UiButton type="button" variant="ghost" size="sm" :aria-label="`下移策略组 ${group.name || index + 1}`" title="下移" :disabled="index === model.policy_groups.length - 1" @click="movePolicyGroup(index, 1)"><UiIcon name="arrow-down" /></UiButton>
              <UiButton v-if="group.id !== model.main_group" type="button" variant="ghost" size="sm" @click="setMainGroup(group.id)">设为主策略组</UiButton>
              <UiButton type="button" variant="ghost" size="sm" :disabled="model.policy_groups.length <= 1" @click="removePolicyGroup(index)">删除</UiButton>
            </div>
          </header>

          <div class="policy-group-grid">
            <FormField label="策略组名称" :name="`policy-group-${index}-name`" hint="客户端展示名称，必须在当前模板内唯一。" required>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="group.name" v-bind="controlAttrs" maxlength="64" placeholder="例如：节点选择" /></template>
            </FormField>
            <FormField label="策略组类型" :name="`policy-group-${index}-type`" hint="只提供当前输出格式支持的类型。" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="group.type" v-bind="controlAttrs" :options="groupTypeOptions" @change="normalizeGroupType(group)" /></template>
            </FormField>
            <FormField label="包含节点名称" :name="`policy-group-${index}-include`" hint="可选 RE2 正则；留空表示包含所有协议配置名称。">
              <template #default="{ controlAttrs }"><UiInput v-model="group.include_pattern" v-bind="controlAttrs" maxlength="256" placeholder="例如：(?i)香港|HK" /></template>
            </FormField>
            <FormField label="排除节点名称" :name="`policy-group-${index}-exclude`" hint="可选 RE2 正则；在包含规则之后执行。">
              <template #default="{ controlAttrs }"><UiInput v-model="group.exclude_pattern" v-bind="controlAttrs" maxlength="256" placeholder="例如：测试|维护" /></template>
            </FormField>

            <template v-if="group.type === 'urltest' || group.type === 'fallback'">
              <FormField label="检测地址" :name="`policy-group-${index}-probe-url`" :hint="renderer === 'znet-sink' ? 'Zero 当前仅支持 HTTP 检测地址。' : '用于可用性或延迟检测。'" required full>
                <template #default="{ controlAttrs }"><UiInput v-model.trim="group.probe_url" v-bind="controlAttrs" type="url" maxlength="2048" placeholder="http://www.gstatic.com/generate_204" /></template>
              </FormField>
              <FormField label="检测间隔" :name="`policy-group-${index}-interval`" hint="60 秒至 24 小时。" required>
                <template #default="{ controlAttrs }"><UiNumberInput v-model="group.interval" v-bind="controlAttrs" :min="60" :max="86400" suffix=" 秒" /></template>
              </FormField>
              <FormField v-if="group.type === 'urltest' && renderer !== 'znet-sink'" label="延迟容差" :name="`policy-group-${index}-tolerance`" hint="容差内保持当前节点，减少频繁切换。">
                <template #default="{ controlAttrs }"><UiNumberInput v-model="group.tolerance" v-bind="controlAttrs" :min="0" :max="10000" suffix=" ms" /></template>
              </FormField>
            </template>
          </div>

          <div v-if="group.type === 'select'" class="group-special-target-editor">
            <div>
              <strong>特殊节点</strong>
              <span>决定系统特殊节点是否作为该策略组成员导出；sing-box 使用路由拒绝动作，不提供 REJECT 出站。</span>
            </div>
            <label class="group-reference-option">
              <UiCheckbox
                :model-value="subscriptionPolicyGroupIncludesDirect(group)"
                @update:model-value="setSubscriptionPolicyGroupSpecialTarget(group, 'direct', $event)"
              />
              <span><b>DIRECT</b> · 直连</span>
            </label>
            <label v-if="renderer !== 'sing-box'" class="group-reference-option">
              <UiCheckbox
                :model-value="subscriptionPolicyGroupIncludesReject(group)"
                @update:model-value="setSubscriptionPolicyGroupSpecialTarget(group, 'reject', $event)"
              />
              <span><b>REJECT</b> · 拒绝</span>
            </label>
          </div>

          <div v-if="otherGroups(group.id).length" class="group-reference-editor">
            <div>
              <strong>包含其他策略组</strong>
              <span>这些组会和正则匹配的全部节点一起成为当前组成员。</span>
            </div>
            <div class="group-reference-options">
              <label v-for="candidate in otherGroups(group.id)" :key="candidate.id" class="group-reference-option">
                <UiCheckbox
                  :model-value="(group.include_groups || []).includes(candidate.id)"
                  @update:model-value="toggleIncludedGroup(group, candidate.id, $event)"
                />
                <span>{{ candidate.name }}</span>
              </label>
            </div>
            <FormField v-if="group.type === 'select' && (group.include_groups || []).length" label="默认子策略组" :name="`policy-group-${index}-default`" hint="未指定时使用第一个匹配节点。">
              <template #default="{ controlAttrs }">
                <UiSelect v-model="group.default_group" v-bind="controlAttrs" :options="defaultGroupOptions(group)" placeholder="第一个匹配节点" />
              </template>
            </FormField>
          </div>
        </section>
      </div>

      <FormField label="最终路由" name="template-final-target" hint="所有规则均未命中时使用；默认指向主策略组。" required>
        <template #default="{ controlAttrs }"><UiSelect v-model="model.final" v-bind="controlAttrs" :options="finalTargetOptions" /></template>
      </FormField>

      <div class="section-heading">
        <div>
          <strong>规则集绑定</strong>
          <span>规则集命中后可指向任一策略组，也可以直接选择直连或拦截。</span>
        </div>
        <UiButton type="button" variant="secondary" size="sm" :disabled="model.rule_sets.length >= 64" @click="addRemoteRuleSet">
          <UiIcon name="plus" />快捷添加远端
        </UiButton>
      </div>
      <FormField label="选择规则集" name="template-rule-set-library" hint="仅检索当前输出格式中已启用的规则集；选择后默认绑定主策略组。" full>
        <template #default="{ controlAttrs }">
          <SubscriptionRuleSetLookup
            v-bind="controlAttrs"
            :renderer="supportedRenderer"
            :exclude-ids="referencedRuleSetIDs"
            :disabled="model.rule_sets.length >= 64"
            @select="addLibraryRuleSet"
          />
        </template>
      </FormField>

      <div v-if="model.rule_sets.length" class="rule-set-list">
        <section v-for="(rule, index) in model.rule_sets" :key="rule.rule_set_id ? `library-${rule.rule_set_id}` : `remote-${index}`" class="rule-set-row">
          <header>
            <div>
              <span class="group-index">{{ index + 1 }}</span>
              <strong>{{ rule.rule_set_id ? ruleSetDetail(rule.rule_set_id)?.name || `规则集 #${rule.rule_set_id}` : rule.tag || '未命名远端规则集' }}</strong>
              <StatusBadge :tone="rule.rule_set_id ? 'info' : 'neutral'" :icon="rule.rule_set_id ? 'database' : 'activity'">{{ rule.rule_set_id ? '规则集' : '快捷远端' }}</StatusBadge>
            </div>
            <UiButton type="button" variant="ghost" size="sm" :aria-label="`删除第 ${index + 1} 个规则集`" @click="removeRuleSet(index)">删除</UiButton>
          </header>
          <div v-if="rule.rule_set_id" class="library-rule-grid">
            <div class="library-rule-source">
              <code>{{ ruleSetDetail(rule.rule_set_id)?.tag || `#${rule.rule_set_id}` }}</code>
              <span>{{ ruleSetDetail(rule.rule_set_id)?.format || '正在读取规则集详情' }} · {{ formatInterval(ruleSetDetail(rule.rule_set_id)?.interval) }}</span>
              <small>{{ ruleSetDetail(rule.rule_set_id)?.url || '保存和预览时由后端校验引用。' }}</small>
            </div>
            <FormField label="命中后出站" :name="`rule-set-${index}-target`" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="rule.target" v-bind="controlAttrs" :options="ruleTargetOptions" /></template>
            </FormField>
          </div>
          <div v-else class="rule-set-grid">
            <FormField label="标识" :name="`rule-set-${index}-tag`" hint="仅字母、数字、点、下划线和连字符。" required>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="rule.tag" v-bind="controlAttrs" maxlength="64" placeholder="例如：reject-ads" /></template>
            </FormField>
            <FormField label="命中后出站" :name="`rule-set-${index}-target`" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="rule.target" v-bind="controlAttrs" :options="ruleTargetOptions" /></template>
            </FormField>
            <FormField label="下载地址" :name="`rule-set-${index}-url`" hint="仅支持不含账号密码的 HTTP(S) 地址。" required full>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="rule.url" v-bind="controlAttrs" type="url" maxlength="2048" placeholder="https://example.com/rules/ads.yaml" /></template>
            </FormField>
            <FormField v-if="renderer === 'clash'" label="规则行为" :name="`rule-set-${index}-behavior`" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="rule.behavior" v-bind="controlAttrs" :options="clashRuleBehaviorOptions" @change="normalizeClashRule(rule)" /></template>
            </FormField>
            <FormField label="资源格式" :name="`rule-set-${index}-format`" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="rule.format" v-bind="controlAttrs" :options="formatOptions(rule)" @change="normalizeClashRule(rule)" /></template>
            </FormField>
            <FormField label="更新间隔" :name="`rule-set-${index}-interval`" hint="60 秒至 7 天。" required>
              <template #default="{ controlAttrs }"><UiNumberInput v-model="rule.interval" v-bind="controlAttrs" :min="60" :max="604800" suffix=" 秒" /></template>
            </FormField>
          </div>
        </section>
      </div>
      <div v-else class="rule-set-empty">
        <UiIcon name="audit" />
        <div><strong>尚未绑定规则集</strong><span>可以选择统一维护的规则集，也可以添加当前模板专用的远端来源。</span></div>
      </div>
    </div>

    <SubscriptionRawCustomizer v-else-if="activeTab === 'raw'" v-model="model" :renderer="renderer" :active="activeTab === 'raw'" @error="$emit('raw-error', $event)" />

    <div v-else class="customizer-advanced">
      <PageAlert tone="warning" title="高级配置会直接影响客户端输出">
        这里填写 {{ advancedSubscriptionLanguage(renderer) }} 配置。保存、预览和订阅输出都会校验最终结构与引用；
        <code>{{ protectedField }}</code> 默认由系统生成。需要接管该数组时，必须保留对应的系统注入标记。
      </PageAlert>
      <div class="advanced-editor-heading">
        <div>
          <strong>{{ advancedSubscriptionLanguage(renderer) }} 高级配置</strong>
          <span>{{ advancedInjectionHint }}</span>
        </div>
        <StatusBadge :tone="model.advanced_source ? 'warning' : 'neutral'" :icon="model.advanced_source ? 'edit' : 'minus'">{{ model.advanced_source ? '高级配置已启用' : '未启用' }}</StatusBadge>
      </div>
      <TemplateCodeEditor
        v-model="model.advanced_source"
        :aria-label="`${advancedSubscriptionLanguage(renderer)} 高级配置`"
        :placeholder="advancedPlaceholder"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  fetchSubscriptionRuleSetsPage,
  type SubscriptionPolicyGroup,
  type SubscriptionRenderer,
  type SubscriptionRuleSet,
  type SubscriptionTemplateCustomization,
  type SubscriptionTemplateRuleSet,
} from '../api/client'
import {
  advancedSubscriptionLanguage,
  clashRuleBehaviorOptions,
  defaultSubscriptionPolicyGroup,
  defaultSubscriptionRuleSet,
  initializeSubscriptionPolicyGroupSpecialTargets,
  nextSubscriptionPolicyGroupID,
  setSubscriptionPolicyGroupSpecialTarget,
  subscriptionPolicyGroupIncludesDirect,
  subscriptionPolicyGroupIncludesReject,
  subscriptionPolicyGroupTypeOptions,
  subscriptionRuleFormatOptions,
  subscriptionTargetOptions,
  type SupportedSubscriptionRenderer,
} from '../utils/subscriptionTemplateEditor'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'
import SubscriptionRuleSetLookup from './SubscriptionRuleSetLookup.vue'
import SubscriptionRawCustomizer from './SubscriptionRawCustomizer.vue'
import SubscriptionRuntimeCustomizer from './SubscriptionRuntimeCustomizer.vue'
import TemplateCodeEditor from './TemplateCodeEditor.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import UiNumberInput from './UiNumberInput.vue'
import UiSelect from './UiSelect.vue'
import UiTabs from './UiTabs.vue'

const props = withDefaults(defineProps<{ renderer: SubscriptionRenderer | SupportedSubscriptionRenderer; error?: string }>(), { error: '' })
defineEmits<{ 'raw-error': [value: string] }>()
const model = defineModel<SubscriptionTemplateCustomization>({ required: true })
const activeTab = ref('basic')
const tabs = [
  { value: 'basic', label: '可视化配置', icon: 'settings' },
  { value: 'raw', label: 'Raw 模型', icon: 'database' },
  { value: 'advanced', label: '高级配置', icon: 'edit' },
]
const protectedField = computed(() => props.renderer === 'clash' ? 'proxies' : 'outbounds')
const supportedRenderer = computed(() => props.renderer as Exclude<SubscriptionRenderer, 'unsupported'>)
const groupTypeOptions = computed(() => subscriptionPolicyGroupTypeOptions(props.renderer))
const ruleTargetOptions = computed(() => subscriptionTargetOptions(model.value, true))
const finalTargetOptions = computed(() => subscriptionTargetOptions(model.value, false))
const referencedRuleSetIDs = computed(() => model.value.rule_sets.flatMap(rule => rule.rule_set_id ? [rule.rule_set_id] : []))
const ruleSetDetails = ref<Record<number, SubscriptionRuleSet>>({})
let detailRequestSequence = 0
let detailController: AbortController | null = null

const advancedInjectionHint = computed(() => props.renderer === 'clash'
  ? 'proxies 数组使用 $zboard:generated-proxies 注入动态节点；任意成员数组可使用 $zboard:all-nodes 注入节点名称。'
  : 'outbounds 数组使用 {"$zboard":"generated-outbounds"} 注入系统出站；任意成员数组可使用 $zboard:all-nodes 注入节点名称。')

const advancedPlaceholder = computed(() => {
  if (props.renderer === 'clash') {
    return 'dns:\n  enable: true\nproxy-groups:\n  - name: 自定义\n    type: select\n    proxies:\n      - $zboard:all-nodes'
  }
  if (props.renderer === 'sing-box') {
    return '{\n  "route": {\n    "auto_detect_interface": true\n  }\n}'
  }
  return '{\n  "route": {\n    "geoip_database": "GeoLite2-Country.mmdb"\n  }\n}'
})

function addPolicyGroup() {
  const id = nextSubscriptionPolicyGroupID(model.value.policy_groups)
  const group = defaultSubscriptionPolicyGroup(id, `策略组 ${model.value.policy_groups.length + 1}`, 'select')
  if (props.renderer === 'sing-box') setSubscriptionPolicyGroupSpecialTarget(group, 'reject', false)
  model.value.policy_groups.push(group)
}

function movePolicyGroup(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= model.value.policy_groups.length) return
  const groups = [...model.value.policy_groups]
  ;[groups[index], groups[target]] = [groups[target], groups[index]]
  model.value.policy_groups = groups
}

function setMainGroup(groupID: string) {
  const previousTarget = `group:${model.value.main_group}`
  model.value.main_group = groupID
  if (model.value.final === previousTarget) model.value.final = `group:${groupID}`
}

function removePolicyGroup(index: number) {
  if (model.value.policy_groups.length <= 1) return
  const [removed] = model.value.policy_groups.splice(index, 1)
  const nextMain = model.value.policy_groups[0].id
  if (removed.id === model.value.main_group) model.value.main_group = nextMain
  const removedTarget = `group:${removed.id}`
  const fallbackTarget = `group:${model.value.main_group}`
  if (model.value.final === removedTarget) model.value.final = fallbackTarget
  for (const group of model.value.policy_groups) {
    group.include_groups = (group.include_groups || []).filter(id => id !== removed.id)
    if (group.default_group === removed.id) group.default_group = ''
  }
  for (const rule of model.value.rule_sets) {
    if (rule.target === removedTarget) rule.target = fallbackTarget
  }
}

function normalizeGroupType(group: SubscriptionPolicyGroup) {
  initializeSubscriptionPolicyGroupSpecialTargets(group)
  if (props.renderer === 'sing-box' && group.type === 'select') setSubscriptionPolicyGroupSpecialTarget(group, 'reject', false)
  if (group.type === 'urltest' || group.type === 'fallback') {
    group.probe_url ||= 'http://www.gstatic.com/generate_204'
    group.interval ||= 300
    if (group.type === 'urltest') group.tolerance ??= 50
  } else {
    group.probe_url = ''
    group.tolerance = 0
  }
  if (group.type !== 'select') group.default_group = ''
}

function groupTypeLabel(groupType: string) {
  return groupTypeOptions.value.find(option => option.value === groupType)?.label || groupType
}

function otherGroups(groupID: string) {
  return model.value.policy_groups.filter(group => group.id !== groupID)
}

function toggleIncludedGroup(group: SubscriptionPolicyGroup, targetID: string, checked: boolean) {
  const current = new Set(group.include_groups || [])
  if (checked) current.add(targetID)
  else current.delete(targetID)
  group.include_groups = [...current]
  if (!checked && group.default_group === targetID) group.default_group = ''
}

function defaultGroupOptions(group: SubscriptionPolicyGroup) {
  return [
    { label: '第一个匹配节点', value: '' },
    ...(group.include_groups || []).map(id => ({
      label: model.value.policy_groups.find(item => item.id === id)?.name || id,
      value: id,
    })),
  ]
}

function addRemoteRuleSet() {
  if (model.value.rule_sets.length >= 64) return
  const rule = defaultSubscriptionRuleSet(props.renderer)
  rule.target = `group:${model.value.main_group}`
  model.value.rule_sets.push(rule)
}

function addLibraryRuleSet(ruleSet: SubscriptionRuleSet) {
  if (model.value.rule_sets.length >= 64 || referencedRuleSetIDs.value.includes(ruleSet.id)) return
  ruleSetDetails.value = { ...ruleSetDetails.value, [ruleSet.id]: ruleSet }
  model.value.rule_sets.push({ rule_set_id: ruleSet.id, target: `group:${model.value.main_group}` })
}

function removeRuleSet(index: number) {
  model.value.rule_sets.splice(index, 1)
}

function ruleSetDetail(id: number) {
  return ruleSetDetails.value[id]
}

function formatInterval(value?: number) {
  if (!value) return '更新周期待读取'
  if (value % 86400 === 0) return `${value / 86400} 天更新`
  if (value % 3600 === 0) return `${value / 3600} 小时更新`
  return `${value} 秒更新`
}

async function loadReferencedRuleSets() {
  detailController?.abort()
  const ids = referencedRuleSetIDs.value
  if (!ids.length) {
    ruleSetDetails.value = {}
    return
  }
  detailController = new AbortController()
  const currentController = detailController
  const sequence = ++detailRequestSequence
  try {
    const page = await fetchSubscriptionRuleSetsPage({
      ids,
      renderer: supportedRenderer.value,
      limit: Math.max(25, ids.length),
    }, { signal: currentController.signal })
    if (sequence === detailRequestSequence) {
      ruleSetDetails.value = Object.fromEntries(page.items.map(item => [item.id, item]))
    }
  } catch {
    if (sequence === detailRequestSequence && !currentController.signal.aborted) ruleSetDetails.value = {}
  }
}

function formatOptions(rule: SubscriptionTemplateRuleSet) {
  return subscriptionRuleFormatOptions(props.renderer).map(option => ({
    ...option,
    disabled: props.renderer === 'clash' && option.value === 'mrs' && rule.behavior === 'classical',
  }))
}

function normalizeClashRule(rule: SubscriptionTemplateRuleSet) {
  if (props.renderer !== 'clash') return
  if (rule.format === 'mrs' && rule.behavior === 'classical') rule.behavior = 'domain'
}

watch(
  () => [props.renderer, referencedRuleSetIDs.value.join(',')],
  () => { void loadReferencedRuleSets() },
  { immediate: true },
)
onBeforeUnmount(() => detailController?.abort())
</script>

<style scoped>
.subscription-customizer{display:grid;gap:12px;padding-top:2px}
.customizer-header,.section-heading,.advanced-editor-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}
.customizer-header>div:first-child,.section-heading>div,.advanced-editor-heading>div{display:grid;gap:3px}
.customizer-header strong,.section-heading strong,.advanced-editor-heading strong{color:var(--text-strong);font-size:11px}
.customizer-header span,.section-heading span,.advanced-editor-heading span{color:var(--muted);font-size:9px;line-height:1.5}
.customizer-counts,.group-actions{display:flex;align-items:center;gap:6px}
.customizer-basic,.customizer-advanced{display:grid;gap:13px}
.policy-group-list,.rule-set-list{display:grid;border:1px solid var(--line);border-radius:9px;overflow:hidden}
.policy-group-row,.rule-set-row{display:grid;gap:10px;padding:11px 12px;background:var(--surface)}
.policy-group-row+.policy-group-row,.rule-set-row+.rule-set-row{border-top:1px solid var(--line)}
.policy-group-row>header,.rule-set-row>header{display:flex;align-items:center;justify-content:space-between;gap:10px}
.policy-group-row>header>div:first-child,.rule-set-row>header>div{display:flex;align-items:center;gap:8px;color:var(--text-strong);font-size:10px}
.group-index{display:grid;width:20px;height:20px;place-items:center;border-radius:6px;background:var(--primary-soft);color:var(--primary);font-size:9px;font-weight:750}
.policy-group-grid,.rule-set-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 12px}
.policy-group-grid :deep(.form-field-full),.rule-set-grid :deep(.form-field-full){grid-column:1/-1}
.group-special-target-editor{display:grid;grid-template-columns:minmax(180px,1fr) auto auto;align-items:center;gap:12px;padding:10px;border:1px solid var(--line);border-radius:8px;background:var(--surface-soft)}
.group-special-target-editor>div{display:grid;gap:3px}.group-special-target-editor strong{font-size:10px;color:var(--text-strong)}.group-special-target-editor>div span{font-size:9px;color:var(--muted)}
.group-reference-editor{display:grid;grid-template-columns:minmax(160px,.55fr) minmax(220px,1fr) minmax(190px,.55fr);align-items:end;gap:12px;padding:10px;border:1px solid var(--line);border-radius:8px;background:var(--surface-soft)}
.group-reference-editor>div:first-child{align-self:center;display:grid;gap:3px}.group-reference-editor strong{font-size:10px;color:var(--text-strong)}.group-reference-editor span{font-size:9px;color:var(--muted)}
.group-reference-options{align-self:center;display:flex;flex-wrap:wrap;gap:8px 12px}
.group-reference-option{display:inline-flex;align-items:center;gap:6px;cursor:pointer}.group-reference-option span{color:var(--text);font-size:10px}.group-reference-option b{font-size:9px;color:var(--text-strong)}
.library-rule-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(180px,.35fr);align-items:end;gap:12px}
.library-rule-source{min-width:0;display:grid;gap:3px;padding:8px 10px;border:1px solid var(--line);border-radius:7px;background:var(--surface-soft)}
.library-rule-source code{width:max-content;max-width:100%;overflow:hidden;text-overflow:ellipsis;color:var(--code-text);font-size:10px}
.library-rule-source span,.library-rule-source small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:9px}
.rule-set-empty{min-height:74px;display:flex;align-items:center;justify-content:center;gap:9px;border:1px dashed var(--line-strong);border-radius:9px;color:var(--muted);background:var(--surface-soft)}
.rule-set-empty .ui-icon{width:20px;height:20px;color:var(--primary)}
.rule-set-empty>div{display:grid;gap:2px}.rule-set-empty strong{color:var(--text-strong);font-size:10px}.rule-set-empty span{font-size:9px}
.customizer-advanced code{padding:1px 4px;border-radius:4px;background:var(--code-soft);color:var(--code-text)}
@media(max-width:900px){.group-reference-editor,.group-special-target-editor{grid-template-columns:1fr}}
@media(max-width:700px){.customizer-header,.section-heading,.advanced-editor-heading,.policy-group-row>header,.rule-set-row>header{align-items:flex-start;flex-direction:column}.policy-group-grid,.rule-set-grid,.library-rule-grid{grid-template-columns:1fr}.policy-group-grid :deep(.form-field-full),.rule-set-grid :deep(.form-field-full){grid-column:auto}.customizer-counts,.group-actions{flex-wrap:wrap}}
</style>
