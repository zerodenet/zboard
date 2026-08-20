<template>
  <section class="standard-page">
    <PageHeader title="系统设置" description="按配置域管理站点身份、公开政策、通知和运行参数。部署密钥仍由服务器环境负责。" eyebrow="Configuration">
      <template #actions><PageRefreshButton label="刷新系统设置" :loading="loading" @click="reloadSettings" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="系统设置已保存" error-title="设置操作失败" />

    <UiSection class="settings-shell">
      <UiTabs v-model="activeTab" :items="tabItems" label="系统设置分类" />

      <div v-if="activeTab === 'site'" class="settings-panel stack-lg">
        <UiSection title="站点身份" description="公开名称、访问地址和注册策略。">
          <template #meta><StatusBadge :tone="siteState.dirty.value ? 'warning' : 'info'" :icon="siteState.dirty.value ? 'edit' : 'info'">{{ siteState.dirty.value ? '有未保存修改' : '公开配置' }}</StatusBadge></template>
          <form ref="siteFormElement" class="panel-body stack" novalidate @submit.prevent="saveSiteSettings">
            <div class="form-grid">
              <FormField v-slot="{ controlAttrs }" label="站点名称" name="settings-site-name" :error="siteErrors.fields.site_name" required><UiInput v-model.trim="form.site_name" v-bind="controlAttrs" maxlength="80" /></FormField>
              <FormField v-slot="{ controlAttrs }" label="公开访问地址" name="settings-site-url" hint="用于订阅链接、canonical 地址和外部页面跳转。" :error="siteErrors.fields.site_url" required><UiInput v-model.trim="form.site_url" v-bind="controlAttrs" type="url" /></FormField>
            </div>
            <label class="registration-switch"><span class="switch-copy"><strong>开放用户注册</strong><small>允许访客通过登录页创建普通账户。</small></span><UiCheckbox v-model="form.allow_registration" role="switch" /></label>
            <PageAlert v-if="siteErrors.formError.value" tone="danger" title="站点设置未保存">{{ siteErrors.formError.value }}</PageAlert>
            <div class="form-actions"><UiButton type="submit" :loading="savingSite" :disabled="!siteState.dirty.value">保存站点身份</UiButton></div>
          </form>
        </UiSection>

        <UiSection title="视觉品牌" description="配置公开站点使用的 Logo 和浏览器图标。支持完整 HTTP/HTTPS URL 或 / 开头的站内资源路径。">
          <template #meta><StatusBadge v-if="groupDirty(siteVisualKeys)" tone="warning" icon="edit">有未保存修改</StatusBadge></template>
          <div class="site-config-layout">
            <div class="public-fields form-grid">
              <SettingsPublicField v-for="config in visualConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :error="configErrors[config.config_key]" @update:draft="updateDraft(config.config_key, $event)" />
            </div>
            <aside class="brand-preview" aria-label="品牌预览">
              <div class="brand-preview__surface brand-preview__surface-light">
                <small>浅色背景</small>
                <img v-if="previewProfile.logo" :src="previewProfile.logo" :alt="previewProfile.name" />
                <div v-else class="brand-preview__fallback"><span>{{ previewInitial }}</span><strong>{{ previewProfile.name }}</strong></div>
              </div>
              <div class="brand-preview__surface brand-preview__surface-dark">
                <small>深色背景</small>
                <img v-if="previewDarkLogo" :src="previewDarkLogo" :alt="previewProfile.name" />
                <div v-else class="brand-preview__fallback"><span>{{ previewInitial }}</span><strong>{{ previewProfile.name }}</strong></div>
              </div>
              <div class="favicon-preview"><span>浏览器图标</span><img v-if="previewProfile.favicon" :src="previewProfile.favicon" alt="favicon preview" /><i v-else>{{ previewInitial }}</i></div>
            </aside>
          </div>
          <div class="section-actions">
            <UiButton v-if="groupHasConflict(siteVisualKeys)" variant="ghost" type="button" :disabled="Boolean(savingGroup)" @click="reloadConfigGroup(siteVisualKeys)"><UiIcon name="refresh" />重新载入冲突项</UiButton>
            <UiButton type="button" :loading="savingGroup === 'visual'" :disabled="!groupDirty(siteVisualKeys)" @click="saveConfigGroup('visual', '视觉品牌', siteVisualKeys)">保存视觉品牌</UiButton>
          </div>
        </UiSection>

        <UiSection title="公开文案" description="只开放真正属于站点内容的文字；套餐、登录注册等固定业务动作仍由产品行为决定。">
          <template #meta><StatusBadge v-if="groupDirty(siteContentKeys)" tone="warning" icon="edit">有未保存修改</StatusBadge></template>
          <div class="public-fields form-grid">
            <SettingsPublicField v-for="config in contentConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :error="configErrors[config.config_key]" :full="config.config_key === 'site_desc' || config.config_key === 'site_home_title'" @update:draft="updateDraft(config.config_key, $event)" />
          </div>
          <div class="copy-preview">
            <span>{{ previewProfile.homeKicker }}</span>
            <strong>{{ previewProfile.homeTitle }}</strong>
            <p>{{ previewProfile.description }}</p>
          </div>
          <div class="section-actions">
            <UiButton v-if="groupHasConflict(siteContentKeys)" variant="ghost" type="button" :disabled="Boolean(savingGroup)" @click="reloadConfigGroup(siteContentKeys)"><UiIcon name="refresh" />重新载入冲突项</UiButton>
            <UiButton type="button" :loading="savingGroup === 'content'" :disabled="!groupDirty(siteContentKeys)" @click="saveConfigGroup('content', '公开文案', siteContentKeys)">保存公开文案</UiButton>
          </div>
        </UiSection>

        <UiSection title="页脚与联系方式" description="配置公开 Footer 的版权、客服与社区入口。版权留空时自动使用当前年份和站点名称。">
          <template #meta><StatusBadge v-if="groupDirty(siteContactKeys)" tone="warning" icon="edit">有未保存修改</StatusBadge></template>
          <div class="public-fields form-grid">
            <SettingsPublicField v-for="config in contactConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :error="configErrors[config.config_key]" :full="config.config_key === 'site_footer_copyright'" @update:draft="updateDraft(config.config_key, $event)" />
          </div>
          <div class="footer-preview"><strong>{{ previewProfile.name }}</strong><span>{{ previewProfile.copyright }}</span><span v-if="previewProfile.supportEmail">{{ previewProfile.supportEmail }}</span><span v-if="previewProfile.supportUrl">客服入口</span><span v-if="previewProfile.telegramUrl">Telegram</span></div>
          <div class="section-actions">
            <UiButton v-if="groupHasConflict(siteContactKeys)" variant="ghost" type="button" :disabled="Boolean(savingGroup)" @click="reloadConfigGroup(siteContactKeys)"><UiIcon name="refresh" />重新载入冲突项</UiButton>
            <UiButton type="button" :loading="savingGroup === 'contact'" :disabled="!groupDirty(siteContactKeys)" @click="saveConfigGroup('contact', '页脚与联系方式', siteContactKeys)">保存页脚与联系方式</UiButton>
          </div>
        </UiSection>

        <UiSection title="搜索与分享" description="控制浏览器标题、搜索摘要和 OpenGraph 的基础文案；留空时自动回退到站点名称和站点描述。">
          <template #meta><StatusBadge v-if="groupDirty(siteSeoKeys)" tone="warning" icon="edit">有未保存修改</StatusBadge></template>
          <div class="site-config-layout site-config-layout-seo">
            <div class="public-fields form-grid">
              <SettingsPublicField v-for="config in seoConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :error="configErrors[config.config_key]" full @update:draft="updateDraft(config.config_key, $event)" />
            </div>
            <aside class="search-preview" aria-label="搜索结果预览">
              <small>搜索结果预览</small>
              <span>{{ previewProfile.siteUrl || form.site_url }}</span>
              <strong>{{ previewProfile.metaTitle }}</strong>
              <p>{{ previewProfile.metaDescription }}</p>
            </aside>
          </div>
          <div class="section-actions">
            <UiButton v-if="groupHasConflict(siteSeoKeys)" variant="ghost" type="button" :disabled="Boolean(savingGroup)" @click="reloadConfigGroup(siteSeoKeys)"><UiIcon name="refresh" />重新载入冲突项</UiButton>
            <UiButton type="button" :loading="savingGroup === 'seo'" :disabled="!groupDirty(siteSeoKeys)" @click="saveConfigGroup('seo', '搜索与分享', siteSeoKeys)">保存搜索与分享</UiButton>
          </div>
        </UiSection>
      </div>

      <div v-else-if="activeTab === 'legal'" class="settings-panel stack-lg">
        <UiSection title="公开政策" description="以文档站方式维护所有政策。可以动态创建链接、控制发布状态与展示位置；正文支持 Markdown，或使用完整 HTTP/HTTPS URL。">
          <PolicyDocumentsEditor
            v-if="policyDocumentsConfig"
            :model-value="drafts[policyDocumentsConfig.config_key]"
            :fallback-documents="previewProfile.policyDocuments"
            :dirty="configDirty(policyDocumentsConfig)"
            :saving="savingKey === policyDocumentsConfig.config_key"
            :error="configErrors[policyDocumentsConfig.config_key]"
            :conflict="Boolean(configConflicts[policyDocumentsConfig.config_key])"
            @update:model-value="updateDraft(policyDocumentsConfig.config_key, $event)"
            @reload="reloadConfig(policyDocumentsConfig.config_key)"
            @save="saveConfig(policyDocumentsConfig)"
          />
          <EmptyState v-else icon="audit" title="没有政策文档配置" description="系统当前未返回政策文档中心配置。" />
        </UiSection>

        <UiSection title="法律与注册信息" description="可选的地区中立公开信息。没有注册号、税务编号或当地备案要求时可以完全留空。">
          <LegalItemsEditor
            v-if="legalMetadataConfig"
            :model-value="drafts[legalMetadataConfig.config_key]"
            :dirty="configDirty(legalMetadataConfig)"
            :saving="savingKey === legalMetadataConfig.config_key"
            :error="configErrors[legalMetadataConfig.config_key]"
            :conflict="Boolean(configConflicts[legalMetadataConfig.config_key])"
            @update:model-value="updateDraft(legalMetadataConfig.config_key, $event)"
            @reload="reloadConfig(legalMetadataConfig.config_key)"
            @save="saveConfig(legalMetadataConfig)"
          />
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
import LegalItemsEditor from '../components/LegalItemsEditor.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PolicyDocumentsEditor from '../components/PolicyDocumentsEditor.vue'
import ConfigRow from '../components/SettingsConfigRow.vue'
import SettingsPublicField from '../components/SettingsPublicField.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import UiTabs from '../components/UiTabs.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { confirmAction } from '../utils/feedback'
import { buildSiteProfile } from '../utils/siteProfile'
import { formatSystemConfigDraft, normalizeSystemConfigDraft } from '../utils/systemConfig'
import { collectFieldErrors, isHttpUrl, isUtf8LengthInRange } from '../utils/validation'

const app = useAppStore()
const activeTab = ref('site')
const loading = ref(false)
const savingSite = ref(false)
const savingKey = ref('')
const savingGroup = ref('')
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
const legacyPolicyKeys = new Set(['site_terms_content', 'site_privacy_content', 'site_refund_content'])
const policyDocumentsKeys = new Set(['site_policy_documents'])
const legalMetadataKeys = new Set(['site_legal_items'])
const siteVisualKeys = ['site_logo', 'site_logo_dark', 'site_favicon']
const siteContentKeys = ['site_desc', 'site_home_kicker', 'site_home_title']
const siteContactKeys = ['site_footer_copyright', 'site_support_email', 'site_support_url', 'site_telegram_url']
const siteSeoKeys = ['site_meta_title', 'site_meta_description']
const sitePresentationKeys = new Set([...siteVisualKeys, ...siteContentKeys, ...siteContactKeys, ...siteSeoKeys])
const publicSiteKeys = new Set([...sitePresentationKeys, ...legacyPolicyKeys, ...policyDocumentsKeys, ...legalMetadataKeys])
const tabItems = [
  { value: 'site', label: '站点与品牌', icon: 'info' },
  { value: 'legal', label: '法务与政策', icon: 'audit' },
  { value: 'notifications', label: '邮件与通知', icon: 'audit' },
  { value: 'runtime', label: '系统运行', icon: 'settings' },
]

const operationalConfigs = computed(() => configs.value
  .filter(item => !hiddenSiteKeys.has(item.config_key) && !deprecatedSiteKeys.has(item.config_key))
  .sort((a, b) => a.id - b.id))
const policyDocumentsConfig = computed(() => operationalConfigs.value.find(item => policyDocumentsKeys.has(item.config_key)))
const legalMetadataConfig = computed(() => operationalConfigs.value.find(item => legalMetadataKeys.has(item.config_key)))
const emailConfigs = computed(() => operationalConfigs.value.filter(item => !publicSiteKeys.has(item.config_key) && /smtp|email/i.test(item.config_key)))
const otherConfigs = computed(() => operationalConfigs.value.filter(item => !publicSiteKeys.has(item.config_key) && !/smtp|email/i.test(item.config_key)))
const visualConfigs = computed(() => configsForKeys(siteVisualKeys))
const contentConfigs = computed(() => configsForKeys(siteContentKeys))
const contactConfigs = computed(() => configsForKeys(siteContactKeys))
const seoConfigs = computed(() => configsForKeys(siteSeoKeys))
const configChanges = computed(() => operationalConfigs.value.filter(configDirty).length)
const pageDirty = computed(() => siteState.dirty.value || configChanges.value > 0)
const previewProfile = computed(() => {
  const projected = configs.value.map(config => ({
    ...config,
    value: drafts[config.config_key] !== undefined ? drafts[config.config_key] : config.value,
  })) as SystemConfig[]
  const profile = buildSiteProfile(projected, form.site_name || 'zboard')
  const name = form.site_name.trim() || profile.name
  const siteUrl = form.site_url.trim() || profile.siteUrl
  const copyrightDraft = String(drafts.site_footer_copyright ?? '').trim()
  return {
    ...profile,
    name,
    siteUrl,
    copyright: copyrightDraft || `© ${new Date().getFullYear()} ${name}`,
  }
})
const previewDarkLogo = computed(() => previewProfile.value.logoDark || previewProfile.value.logo)
const previewInitial = computed(() => Array.from(previewProfile.value.name.trim())[0]?.toUpperCase() || 'Z')

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

function configValue(config: SystemConfig) {
  return formatSystemConfigDraft(config)
}

function sameValue(left: unknown, right: unknown) {
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null)
}

function configDirty(config: SystemConfig) {
  return !sameValue(drafts[config.config_key], configValue(config))
}

function configForKey(key: string) {
  return operationalConfigs.value.find(item => item.config_key === key)
}

function configsForKeys(keys: readonly string[]) {
  return keys.map(configForKey).filter((item): item is SystemConfig => Boolean(item))
}

function groupDirty(keys: readonly string[]) {
  return configsForKeys(keys).some(configDirty)
}

function groupHasConflict(keys: readonly string[]) {
  return keys.some(key => Boolean(configConflicts[key]))
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
    message.value = '站点身份已保存。'
  } catch (e: any) {
    await siteErrors.applyApiError(e, '站点设置保存失败。', siteFormElement, { site_name: 'site_name', site_url: 'site_url' })
  } finally { savingSite.value = false }
}

function applyUpdatedConfig(updated: SystemConfig) {
  const index = configs.value.findIndex(item => item.config_key === updated.config_key)
  if (index >= 0) configs.value[index] = updated
  drafts[updated.config_key] = configValue(updated)
  delete configConflicts[updated.config_key]
  delete configErrors[updated.config_key]
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
    applyUpdatedConfig(updated)
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

async function saveConfigGroup(group: string, label: string, keys: readonly string[]) {
  const candidates = configsForKeys(keys).filter(configDirty)
  if (!candidates.length) return
  message.value = ''; error.value = ''

  const prepared: Array<{ config: SystemConfig; value: unknown }> = []
  let firstInvalidKey = ''
  for (const config of candidates) {
    delete configErrors[config.config_key]
    const normalized = normalizeSystemConfigDraft(config, drafts[config.config_key])
    if (normalized.error) {
      configErrors[config.config_key] = normalized.error
      firstInvalidKey ||= config.config_key
    } else prepared.push({ config, value: normalized.value })
  }
  if (firstInvalidKey) {
    await nextTick()
    document.getElementById(`${firstInvalidKey}-control`)?.focus()
    return
  }

  savingGroup.value = group
  let saved = 0
  let failed = 0
  try {
    for (const item of prepared) {
      const key = item.config.config_key
      try {
        const updated = await updateSystemConfig(key, item.value, item.config.revision)
        applyUpdatedConfig(updated)
        saved += 1
      } catch (e: any) {
        failed += 1
        if (e?.response?.status === 409) {
          configConflicts[key] = true
          configErrors[key] = '服务器版本已变化。请重新载入当前值，确认后再保存。'
        } else configErrors[key] = normalizeApiErrorMessage(e, `${item.config.name} 保存失败。`)
      }
    }
    if (saved) await app.loadPublicConfigs()
    if (failed) message.value = `${label}已保存 ${saved} 项，另有 ${failed} 项需要处理。`
    else message.value = `${label}已保存。`
  } finally { savingGroup.value = '' }
}

async function reloadConfig(key: string) {
  savingKey.value = key
  try {
    const latest = (await fetchSystemConfigs()).find(item => item.config_key === key)
    if (!latest) throw new Error('服务器未返回该配置。')
    applyUpdatedConfig(latest)
    message.value = `${latest.name} 已重新载入。`
  } catch (e: any) {
    configErrors[key] = normalizeApiErrorMessage(e, '重新载入失败。')
  } finally { savingKey.value = '' }
}

async function reloadConfigGroup(keys: readonly string[]) {
  const conflicted = keys.filter(key => configConflicts[key])
  if (!conflicted.length) return
  savingGroup.value = 'reload'
  try {
    const latest = await fetchSystemConfigs()
    for (const key of conflicted) {
      const config = latest.find(item => item.config_key === key)
      if (config) applyUpdatedConfig(config)
    }
    message.value = '冲突配置已重新载入。'
  } catch (e: any) {
    error.value = normalizeApiErrorMessage(e, '重新载入冲突配置失败。')
  } finally { savingGroup.value = '' }
}

onMounted(loadAllData)
</script>

<style scoped>
.settings-shell { overflow: hidden; }
.settings-shell :deep(.ui-tabs) { border-bottom: 1px solid var(--line); }
.settings-panel { padding: 20px; }
.stack-lg { display: grid; gap: 18px; }
.registration-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }
.switch-copy { display: grid; gap: 3px; }.switch-copy strong { font-size: 13px; }.switch-copy small { color: var(--muted); font-size: 11px; }
.public-fields { padding: 16px; }
.site-config-layout { display: grid; grid-template-columns: minmax(0, 1.25fr) minmax(250px, .75fr); gap: 16px; align-items: start; }
.site-config-layout .public-fields { padding-right: 0; }
.brand-preview { display: grid; gap: 10px; margin: 16px 16px 16px 0; }
.brand-preview__surface { min-height: 116px; display: grid; align-content: center; justify-items: start; gap: 12px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.brand-preview__surface small { font-size: 9px; opacity: .68; }
.brand-preview__surface img { display: block; width: auto; max-width: 190px; max-height: 48px; object-fit: contain; }
.brand-preview__surface-light { background: #fff; color: #111827; }
.brand-preview__surface-dark { background: #111827; color: #f8fafc; }
.brand-preview__fallback { display: flex; align-items: center; gap: 9px; }.brand-preview__fallback span { width: 34px; height: 34px; display: grid; place-items: center; border: 1px solid currentColor; border-radius: 9px; font-weight: 800; }.brand-preview__fallback strong { font-size: 13px; }
.favicon-preview { display: flex; align-items: center; gap: 9px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); color: var(--muted); font-size: 10px; }.favicon-preview img,.favicon-preview i { width: 24px; height: 24px; display: grid; place-items: center; border-radius: 5px; object-fit: contain; background: var(--surface); color: var(--text); font-style: normal; font-weight: 800; }
.section-actions { display: flex; justify-content: flex-end; gap: 8px; padding: 0 16px 16px; }
.copy-preview { display: grid; gap: 7px; margin: 0 16px 16px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.copy-preview span { color: var(--primary); font-size: 10px; font-weight: 700; }.copy-preview strong { max-width: 760px; font-size: 18px; line-height: 1.35; }.copy-preview p { max-width: 760px; margin: 0; color: var(--muted); font-size: 11px; line-height: 1.55; }
.footer-preview { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; margin: 0 16px 16px; padding: 13px 15px; border-radius: 9px; background: #111827; color: #f8fafc; }.footer-preview strong { font-size: 12px; }.footer-preview span { color: #cbd5e1; font-size: 9px; }
.site-config-layout-seo { grid-template-columns: minmax(0, 1fr) minmax(280px, .7fr); }
.search-preview { display: grid; gap: 4px; margin: 16px 16px 16px 0; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }.search-preview small { margin-bottom: 7px; color: var(--muted); font-size: 9px; }.search-preview span { color: var(--success); font-size: 10px; overflow-wrap: anywhere; }.search-preview strong { color: var(--primary); font-size: 16px; font-weight: 500; line-height: 1.3; }.search-preview p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.5; }
.security-header { color: var(--success); font-size: 20px; }.security-list { display: grid; gap: 4px; }.security-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 10px 0; }.security-list > div + div { border-top: 1px solid var(--line); }.security-list > div > span:first-child { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--success); background: var(--success-soft); }.security-list strong { font-size: 12px; }.security-list p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }
.config-count { color: var(--muted); font-size: 12px; }.config-list { display: grid; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }.policy-editors { display: grid; gap: 16px; padding: 16px; }
:deep(.config-row) { display: grid; grid-template-columns: minmax(260px, 1.2fr) minmax(220px, .8fr) auto; align-items: center; gap: 16px; padding: 14px; background: var(--surface); }:deep(.config-row + .config-row) { border-top: 1px solid var(--line); }
@media (max-width: 900px) { .site-config-layout,.site-config-layout-seo { grid-template-columns: 1fr; }.site-config-layout .public-fields { padding-right: 16px; }.brand-preview,.search-preview { margin: 0 16px 16px; } }
@media (max-width: 850px) { .settings-panel { padding: 14px; }:deep(.config-row) { grid-template-columns: 1fr; align-items: stretch; }.section-actions { flex-wrap: wrap; } }
</style>
