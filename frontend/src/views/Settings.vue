<template>
  <section class="standard-page">
    <PageHeader title="系统设置" description="按配置域管理站点身份、公开政策、通知和运行参数。部署密钥仍由服务器环境负责。" eyebrow="Configuration">
      <template #actions><PageRefreshButton label="刷新系统设置" :loading="loading" @click="reloadSettings" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="系统设置已保存" error-title="设置操作失败" />

    <UiSection class="settings-shell">
      <UiTabs v-model="activeTab" :items="tabItems" label="系统设置分类" />

      <div v-if="activeTab === 'site'" class="settings-panel">
        <div class="section-grid settings-top">
          <UiSection class="span-7" title="基础站点信息" description="公开名称、访问地址和注册策略。">
            <template #meta><StatusBadge :tone="siteState.dirty.value ? 'warning' : 'info'" :icon="siteState.dirty.value ? 'edit' : 'info'">{{ siteState.dirty.value ? '有未保存修改' : '公开配置' }}</StatusBadge></template>
            <form ref="siteFormElement" class="panel-body stack" novalidate @submit.prevent="saveSiteSettings">
              <div class="form-grid">
                <FormField v-slot="{ controlAttrs }" label="站点名称" name="settings-site-name" :error="siteErrors.fields.site_name" required><UiInput v-model.trim="form.site_name" v-bind="controlAttrs" maxlength="80" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="公开访问地址" name="settings-site-url" hint="用于订阅链接、canonical 地址和外部页面跳转。" :error="siteErrors.fields.site_url" required><UiInput v-model.trim="form.site_url" v-bind="controlAttrs" type="url" /></FormField>
              </div>
              <label class="registration-switch"><span class="switch-copy"><strong>开放用户注册</strong><small>允许访客通过登录页创建普通账户。</small></span><UiCheckbox v-model="form.allow_registration" role="switch" /></label>
              <PageAlert v-if="siteErrors.formError.value" tone="danger" title="站点设置未保存">{{ siteErrors.formError.value }}</PageAlert>
              <div class="form-actions"><UiButton type="submit" :loading="savingSite" :disabled="!siteState.dirty.value">保存基础设置</UiButton></div>
            </form>
          </UiSection>

          <UiSection class="span-5" title="品牌行为" description="这里仅开放真正属于运营者身份和内容的字段。">
            <div class="panel-body boundary-copy">
              <div><UiIcon name="check" /><span><strong>运营者可配置</strong><small>Logo、站点描述、首页主文案、Footer、联系方式和 SEO。</small></span></div>
              <div><UiIcon name="shield" /><span><strong>产品行为保持固定</strong><small>套餐入口、登录注册目标等业务动作不提供仅文案可改的“半配置”。</small></span></div>
            </div>
          </UiSection>
        </div>

        <UiSection title="品牌与公开内容" description="调整公开站点的视觉身份、联系方式、首页文案和搜索展示。">
          <template #meta><span class="config-count">{{ sitePresentationConfigs.length }} 项</span></template>
          <div v-if="sitePresentationConfigs.length" class="config-list"><ConfigRow v-for="config in sitePresentationConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
          <EmptyState v-else icon="info" title="没有站点配置" description="系统当前未返回站点品牌配置。" />
        </UiSection>
      </div>

      <div v-else-if="activeTab === 'legal'" class="settings-panel stack-lg">
        <UiSection title="公开政策" description="默认模板自动引用当前站点名称、版权和客服信息；可以直接编辑 Markdown，也可以把整个内容替换为一个远端 URL。">
          <div v-if="policyConfigs.length" class="policy-editors">
            <MarkdownContentEditor
              v-for="config in policyConfigs"
              :key="config.config_key"
              :config="config"
              :model-value="drafts[config.config_key]"
              :template="policyTemplate(config.config_key)"
              :profile="app.siteProfile"
              :dirty="configDirty(config)"
              :saving="savingKey === config.config_key"
              :error="configErrors[config.config_key]"
              :conflict="Boolean(configConflicts[config.config_key])"
              @update:model-value="updateDraft(config.config_key, $event)"
              @reload="reloadConfig(config.config_key)"
              @save="saveConfig(config)"
            />
          </div>
          <EmptyState v-else icon="audit" title="没有政策配置" description="系统当前未返回服务条款、隐私政策或退款政策。" />
        </UiSection>

        <UiSection title="法律与注册信息" description="以地区中立的条目表达 Company No.、VAT、当地备案或其他公开注册信息。">
          <div v-if="legalMetadataConfigs.length" class="config-list"><ConfigRow v-for="config in legalMetadataConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
          <EmptyState v-else icon="audit" title="没有法律信息配置" description="无需展示额外注册信息时可以保持为空。" />
        </UiSection>
      </div>

      <div v-else-if="activeTab === 'notifications'" class="settings-panel">
        <UiSection title="邮件与通知" description="先完成 SMTP 配置，再启用依赖邮件的通知任务。">
          <template #meta><span class="config-count">{{ emailConfigs.length }} 项</span></template>
          <div v-if="emailConfigs.length" class="config-list"><ConfigRow v-for="config in emailConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
          <EmptyState v-else icon="audit" title="没有邮件配置" description="系统当前未返回可编辑的邮件或通知参数。" />
        </UiSection>
      </div>

      <div v-else class="settings-panel stack-lg">
        <UiSection title="安全边界" description="敏感凭证不属于浏览器设置，不会在这里读取或回显。">
          <template #meta><UiIcon class="security-header" name="shield" /></template>
          <div class="panel-body security-list">
            <div><span><UiIcon name="database" /></span><div><strong>数据库凭证</strong><p>由部署环境变量管理</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
            <div><span><UiIcon name="key" /></span><div><strong>JWT 签名密钥</strong><p>服务启动时校验强度</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
            <div><span><UiIcon name="shield" /></span><div><strong>凭证加密密钥</strong><p>用于节点与协议敏感配置</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
          </div>
        </UiSection>

        <UiSection title="运行参数" description="系统时区、数据保留策略和其他服务端运行行为。">
          <template #meta><span class="config-count">{{ otherConfigs.length }} 项</span></template>
          <div v-if="otherConfigs.length" class="config-list"><ConfigRow v-for="config in otherConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
          <EmptyState v-else icon="settings" title="没有运行配置" description="系统当前未返回其他可编辑运行参数。" />
        </UiSection>
      </div>
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, type SystemConfig } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import MarkdownContentEditor from '../components/MarkdownContentEditor.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import ConfigRow from '../components/SettingsConfigRow.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import UiTabs from '../components/UiTabs.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { confirmAction } from '../utils/feedback'
import { defaultLegalTemplates, type LegalContentKey } from '../utils/legalContent'
import { formatSystemConfigDraft, normalizeSystemConfigDraft } from '../utils/systemConfig'
import { collectFieldErrors, isHttpUrl, isUtf8LengthInRange } from '../utils/validation'

const app = useAppStore()
const activeTab = ref('site')
const loading = ref(false)
const savingSite = ref(false)
const savingKey = ref('')
const message = ref('')
const error = ref('')
const configs = ref<SystemConfig[]>([])
const drafts = reactive<Record<string, unknown>>({})
const configErrors = reactive<Record<string, string>>({})
const configConflicts = reactive<Record<string, boolean>>({})
const form = reactive({ site_name: '', site_url: '', allow_registration: true })
const siteFormElement = ref<HTMLElement | null>(null)
const siteErrors = useFormErrors()
const siteState = useDirtyForm(() => form)

const hiddenSiteKeys = new Set(['site_name', 'site_url', 'register_switch'])
const deprecatedSiteKeys = new Set(['site_terms_url', 'site_privacy_url', 'site_refund_url', 'site_home_primary_cta'])
const policyKeyOrder: LegalContentKey[] = ['site_terms_content', 'site_privacy_content', 'site_refund_content']
const policyKeys = new Set<string>(policyKeyOrder)
const legalMetadataKeys = new Set(['site_legal_items'])
const sitePresentationKeys = new Set([
  'site_desc', 'site_logo', 'site_logo_dark', 'site_favicon', 'site_footer_copyright',
  'site_support_email', 'site_support_url', 'site_telegram_url', 'site_meta_title',
  'site_meta_description', 'site_home_kicker', 'site_home_title',
])
const publicSiteKeys = new Set([...sitePresentationKeys, ...policyKeys, ...legalMetadataKeys])

const operationalConfigs = computed(() => configs.value
  .filter(item => !hiddenSiteKeys.has(item.config_key) && !deprecatedSiteKeys.has(item.config_key))
  .sort((a, b) => a.id - b.id))
const sitePresentationConfigs = computed(() => operationalConfigs.value.filter(item => sitePresentationKeys.has(item.config_key)))
const policyConfigs = computed(() => operationalConfigs.value
  .filter(item => policyKeys.has(item.config_key))
  .sort((a, b) => policyKeyOrder.indexOf(a.config_key as LegalContentKey) - policyKeyOrder.indexOf(b.config_key as LegalContentKey)))
const legalMetadataConfigs = computed(() => operationalConfigs.value.filter(item => legalMetadataKeys.has(item.config_key)))
const emailConfigs = computed(() => operationalConfigs.value.filter(item => !publicSiteKeys.has(item.config_key) && /smtp|email/i.test(item.config_key)))
const otherConfigs = computed(() => operationalConfigs.value.filter(item => !publicSiteKeys.has(item.config_key) && !/smtp|email/i.test(item.config_key)))
const configChanges = computed(() => operationalConfigs.value.filter(configDirty).length)
const pageDirty = computed(() => siteState.dirty.value || configChanges.value > 0)
const tabItems = computed(() => [
  { value: 'site', label: '站点与品牌', icon: 'info', count: sitePresentationConfigs.value.length + 1 },
  { value: 'legal', label: '法务与政策', icon: 'audit', count: policyConfigs.value.length + legalMetadataConfigs.value.length },
  { value: 'notifications', label: '邮件与通知', icon: 'audit', count: emailConfigs.value.length },
  { value: 'runtime', label: '系统运行', icon: 'settings', count: otherConfigs.value.length },
])

useUnsavedChangesGuard(
  () => pageDirty.value,
  () => confirmAction({
    title: '离开系统设置？',
    message: `当前有 ${configChanges.value + (siteState.dirty.value ? 1 : 0)} 组修改尚未保存。`,
    confirmText: '放弃修改',
    tone: 'danger',
  }),
)

watch(() => form.site_name, () => siteErrors.clear('site_name'))
watch(() => form.site_url, () => siteErrors.clear('site_url'))

function policyTemplate(key: string) {
  return defaultLegalTemplates[key as LegalContentKey] || ''
}

function configValue(config: SystemConfig) {
  return formatSystemConfigDraft(config)
}

function sameValue(left: unknown, right: unknown) {
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null)
}

function configDirty(config: SystemConfig) {
  return !sameValue(drafts[config.config_key], configValue(config))
}

function updateDraft(key: string, value: unknown) {
  drafts[key] = value
  if (!configConflicts[key]) delete configErrors[key]
}

async function loadConfigs() {
  const next = await fetchSystemConfigs()
  configs.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const key of Object.keys(configErrors)) delete configErrors[key]
  for (const key of Object.keys(configConflicts)) delete configConflicts[key]
  for (const config of next) drafts[config.config_key] = configValue(config)
}

async function loadAllData() {
  loading.value = true; error.value = ''
  try {
    const [settings] = await Promise.all([app.loadSetupStatus(true), loadConfigs()])
    form.site_name = settings?.site_name || 'zboard'
    form.site_url = settings?.site_url || window.location.origin
    form.allow_registration = settings?.allow_registration ?? true
    siteErrors.clear()
    siteState.markClean()
  }
  catch (e: any) { error.value = e?.response?.data?.message || '系统设置加载失败。' }
  finally { loading.value = false }
}

async function reloadSettings() {
  if (pageDirty.value && !await confirmAction({
    title: '重新载入并放弃修改？',
    message: `当前有 ${configChanges.value + (siteState.dirty.value ? 1 : 0)} 组未保存修改。重新载入将使用服务器当前值。`,
    confirmText: '重新载入',
    tone: 'danger',
  })) return
  await loadAllData()
}

async function saveSiteSettings() {
  form.site_name = form.site_name.trim()
  form.site_url = form.site_url.trim().replace(/\/+$/, '')
  const valid = await siteErrors.applyValidation(collectFieldErrors({
    site_name: !isUtf8LengthInRange(form.site_name, 1, 80, true) && '站点名称必须为 1–80 个 UTF-8 字节。',
    site_url: !isHttpUrl(form.site_url) && '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。',
  }), siteFormElement, '请更正标记字段后再保存站点设置。')
  if (!valid) return
  savingSite.value = true; message.value = ''
  try {
    await updateSiteSettings(form)
    await app.loadSetupStatus(true)
    await app.loadPublicConfigs()
    siteState.markClean()
    message.value = '站点设置已保存。'
  } catch (e: any) {
    await siteErrors.applyApiError(e, '站点设置保存失败。', siteFormElement, { site_name: 'site_name', site_url: 'site_url' })
  } finally { savingSite.value = false }
}

async function saveConfig(config: SystemConfig) {
  const key = config.config_key
  if (!configDirty(config)) return
  message.value = ''; delete configErrors[key]
  const normalized = normalizeSystemConfigDraft(config, drafts[key])
  if (normalized.error) {
    configErrors[key] = normalized.error
    await nextTick()
    document.getElementById(`${key}-control`)?.focus()
    return
  }
  savingKey.value = key
  try {
    const updated = await updateSystemConfig(key, normalized.value, config.revision)
    const index = configs.value.findIndex(item => item.config_key === key)
    if (index >= 0) configs.value[index] = updated
    drafts[key] = configValue(updated)
    delete configConflicts[key]
    if (publicSiteKeys.has(key)) await app.loadPublicConfigs()
    message.value = `${config.name} 已保存。`
  } catch (e: any) {
    if (e?.response?.status === 409) {
      configConflicts[key] = true
      configErrors[key] = '服务器版本已变化。请重新载入当前值，确认后再保存。'
    } else {
      configErrors[key] = normalizeApiErrorMessage(e, '运行配置保存失败。')
    }
  } finally { savingKey.value = '' }
}

async function reloadConfig(key: string) {
  savingKey.value = key
  try {
    const latest = (await fetchSystemConfigs()).find(item => item.config_key === key)
    if (!latest) throw new Error('服务器未返回该配置。')
    const index = configs.value.findIndex(item => item.config_key === key)
    if (index >= 0) configs.value[index] = latest
    drafts[key] = configValue(latest)
    delete configErrors[key]
    delete configConflicts[key]
    message.value = `${latest.name} 已重新载入。`
  } catch (e: any) {
    configErrors[key] = normalizeApiErrorMessage(e, '重新载入失败。')
  } finally { savingKey.value = '' }
}

onMounted(loadAllData)
</script>

<style scoped>
.settings-shell { overflow: hidden; }
.settings-shell :deep(.ui-tabs) { border-bottom: 1px solid var(--line); }
.settings-panel { padding: 20px; }
.stack-lg { display: grid; gap: 18px; }
.settings-top { margin-bottom: 18px; }
.registration-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }
.switch-copy { display: grid; gap: 3px; }.switch-copy strong { font-size: 13px; }.switch-copy small { color: var(--muted); font-size: 11px; }
.boundary-copy { display: grid; gap: 14px; }.boundary-copy > div { display: grid; grid-template-columns: auto 1fr; gap: 10px; align-items: flex-start; }.boundary-copy > div > :first-child { margin-top: 2px; color: var(--primary); }.boundary-copy span { display: grid; gap: 3px; }.boundary-copy strong { font-size: 12px; }.boundary-copy small { color: var(--muted); font-size: 10px; line-height: 1.5; }
.security-header { color: var(--success); font-size: 20px; }.security-list { display: grid; gap: 4px; }.security-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 10px 0; }.security-list > div + div { border-top: 1px solid var(--line); }.security-list > div > span:first-child { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--success); background: var(--success-soft); }.security-list strong { font-size: 12px; }.security-list p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }
.config-count { color: var(--muted); font-size: 12px; }.config-list { display: grid; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }.policy-editors { display: grid; gap: 16px; padding: 16px; }
:deep(.config-row) { display: grid; grid-template-columns: minmax(260px, 1.2fr) minmax(220px, .8fr) auto; align-items: center; gap: 16px; padding: 14px; background: var(--surface); }:deep(.config-row + .config-row) { border-top: 1px solid var(--line); }
@media (max-width: 850px) { .settings-panel { padding: 14px; }:deep(.config-row) { grid-template-columns: 1fr; align-items: stretch; } }
</style>
