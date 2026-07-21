<template>
  <section>
    <PageHeader title="系统设置" description="管理站点公开信息和运行配置。部署密钥仍由服务器环境负责，不在页面中暴露。" eyebrow="Configuration">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="loadAll"><UiIcon name="refresh" />刷新</button></template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="section-grid settings-top">
      <article class="panel span-7">
        <header class="panel-header"><div><h2>站点信息</h2><p>公开名称、访问地址和注册策略。</p></div><StatusBadge tone="info">公开配置</StatusBadge></header>
        <form class="panel-body stack" @submit.prevent="saveSiteSettings">
          <div class="form-grid">
            <label class="field"><span>站点名称</span><input v-model.trim="form.site_name" maxlength="80" required /></label>
            <label class="field"><span>公开访问地址</span><input v-model.trim="form.site_url" type="url" required /><small class="field-hint">用于订阅链接和外部页面跳转。</small></label>
          </div>
          <label class="registration-switch"><span class="switch-copy"><strong>开放用户注册</strong><small>允许访客通过登录页创建普通账户。</small></span><input v-model="form.allow_registration" type="checkbox" role="switch" /></label>
          <div class="form-actions"><button class="button" :disabled="savingSite" type="submit">{{ savingSite ? '保存中…' : '保存站点设置' }}</button></div>
        </form>
      </article>

      <article class="panel span-5">
        <header class="panel-header"><div><h2>安全边界</h2><p>下列敏感项不属于浏览器配置。</p></div><UiIcon class="security-header" name="shield" /></header>
        <div class="panel-body security-list">
          <div><span><UiIcon name="database" /></span><div><strong>数据库凭证</strong><p>由部署环境变量管理</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
          <div><span><UiIcon name="key" /></span><div><strong>JWT 签名密钥</strong><p>服务启动时校验强度</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
          <div><span><UiIcon name="shield" /></span><div><strong>凭证加密密钥</strong><p>用于节点与协议敏感配置</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
        </div>
      </article>
    </div>

    <article class="panel">
      <header class="panel-header"><div><h2>运行配置</h2><p>修改后立即写入数据库；密钥类配置不会回显原值。</p></div><span class="config-count">{{ operationalConfigs.length }} 项</span></header>
      <div v-if="operationalConfigs.length" class="config-groups">
        <section v-if="emailConfigs.length" class="config-section">
          <header><span class="config-section-icon"><UiIcon name="audit" /></span><div><h3>邮件与通知</h3><p>先完成 SMTP 配置，再启用邮件任务。</p></div></header>
          <div class="config-list"><ConfigRow v-for="config in emailConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :saving="savingKey === config.config_key" @update:draft="drafts[config.config_key] = $event" @save="saveConfig(config)" /></div>
        </section>
        <section v-if="otherConfigs.length" class="config-section">
          <header><span class="config-section-icon"><UiIcon name="settings" /></span><div><h3>其他运行参数</h3><p>系统行为和公开展示相关的动态配置。</p></div></header>
          <div class="config-list"><ConfigRow v-for="config in otherConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :saving="savingKey === config.config_key" @update:draft="drafts[config.config_key] = $event" @save="saveConfig(config)" /></div>
        </section>
      </div>
      <EmptyState v-else icon="settings" title="没有动态配置" description="系统当前未返回可在页面中修改的运行配置。" />
    </article>
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, type SystemConfig } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'

const ConfigRow = defineComponent({
  name: 'ConfigRow',
  props: { config: { type: Object as () => SystemConfig, required: true }, draft: { required: false }, saving: Boolean },
  emits: ['update:draft', 'save'],
  setup(props, { emit }) {
    return () => h('article', { class: 'config-row' }, [
      h('div', { class: 'config-copy' }, [h('div', [h('strong', props.config.name), h('span', { class: 'value-type' }, props.config.value_type)]), h('code', props.config.config_key), h('p', props.config.description || '—')]),
      props.config.value_type === 'bool'
        ? h('label', { class: 'config-switch' }, [h('span', props.draft ? '已启用' : '已关闭'), h('input', { type: 'checkbox', checked: Boolean(props.draft), onChange: (event: Event) => emit('update:draft', (event.target as HTMLInputElement).checked) })])
        : h('input', { class: 'config-input', type: props.config.is_secret ? 'password' : props.config.value_type === 'int' ? 'number' : 'text', value: props.draft as any, placeholder: props.config.is_secret && props.config.configured ? '已配置；输入新值以轮换' : '', autocomplete: 'off', onInput: (event: Event) => emit('update:draft', (event.target as HTMLInputElement).value) }),
      h('button', { class: 'button button-secondary button-sm', type: 'button', disabled: props.saving, onClick: () => emit('save') }, props.saving ? '保存中…' : '保存')
    ])
  }
})

const app = useAppStore()
const loading = ref(false)
const savingSite = ref(false)
const savingKey = ref('')
const message = ref('')
const error = ref('')
const configs = ref<SystemConfig[]>([])
const drafts = reactive<Record<string, any>>({})
const form = reactive({ site_name: '', site_url: '', allow_registration: true })
const hiddenSiteKeys = new Set(['site_name', 'site_url', 'register_switch'])
const operationalConfigs = computed(() => configs.value.filter(item => !hiddenSiteKeys.has(item.config_key)).sort((a, b) => a.id - b.id))
const emailConfigs = computed(() => operationalConfigs.value.filter(item => /smtp|email/i.test(item.config_key)))
const otherConfigs = computed(() => operationalConfigs.value.filter(item => !/smtp|email/i.test(item.config_key)))

async function loadConfigs() { configs.value = await fetchSystemConfigs(); for (const config of configs.value) drafts[config.config_key] = config.is_secret ? '' : config.value }
async function loadAll() {
  loading.value = true; error.value = ''
  try { const settings = await app.loadSetupStatus(true); form.site_name = settings?.site_name || 'zboard'; form.site_url = settings?.site_url || window.location.origin; form.allow_registration = settings?.allow_registration ?? true; await loadConfigs() }
  catch (e: any) { error.value = e?.response?.data?.message || '系统设置加载失败。' }
  finally { loading.value = false }
}
async function saveSiteSettings() { savingSite.value = true; message.value = ''; error.value = ''; try { await updateSiteSettings(form); await Promise.all([app.loadSetupStatus(true), loadConfigs()]); message.value = '站点设置已保存。' } catch (e: any) { error.value = e?.response?.data?.message || '站点设置保存失败。' } finally { savingSite.value = false } }
async function saveConfig(config: SystemConfig) { savingKey.value = config.config_key; message.value = ''; error.value = ''; try { let value: unknown = drafts[config.config_key]; if (config.is_secret && config.configured && value === '') throw new Error('请输入替换后的新密钥。'); if (config.value_type === 'int') value = Number(value); if (config.value_type === 'json' && typeof value === 'string') value = JSON.parse(value); const updated = await updateSystemConfig(config.config_key, value, config.revision); const index = configs.value.findIndex(item => item.config_key === config.config_key); if (index >= 0) configs.value[index] = updated; drafts[config.config_key] = updated.is_secret ? '' : updated.value; message.value = `${config.name} 已保存。` } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '运行配置保存失败。' } finally { savingKey.value = '' } }
onMounted(loadAll)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.settings-top { margin-bottom: 16px; }.registration-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.switch-copy { display: grid; gap: 3px; }.switch-copy strong { font-size: 13px; }.switch-copy small { color: var(--muted); font-size: 11px; }.registration-switch input, .config-switch input { width: 38px; height: 21px; accent-color: var(--primary); }.security-header { color: var(--success); font-size: 20px; }.security-list { display: grid; gap: 4px; }.security-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 10px 0; }.security-list > div + div { border-top: 1px solid var(--line); }.security-list > div > span:first-child { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--success); background: var(--success-soft); }.security-list strong { font-size: 12px; }.security-list p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }.config-count { color: var(--muted); font-size: 12px; }.config-groups { display: grid; }.config-section { padding: 20px; }.config-section + .config-section { border-top: 1px solid var(--line); }.config-section > header { display: flex; gap: 10px; margin-bottom: 14px; }.config-section-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.config-section h3 { margin: 1px 0 2px; font-size: 14px; }.config-section header p { margin: 0; color: var(--muted); font-size: 10px; }.config-list { display: grid; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
:deep(.config-row) { display: grid; grid-template-columns: minmax(260px, 1.2fr) minmax(220px, .8fr) auto; align-items: center; gap: 16px; padding: 14px; background: #fff; }:deep(.config-row + .config-row) { border-top: 1px solid var(--line); }:deep(.config-copy) { min-width: 0; display: grid; gap: 4px; }:deep(.config-copy > div) { display: flex; align-items: center; gap: 7px; }:deep(.config-copy strong) { font-size: 12px; }:deep(.value-type) { padding: 2px 5px; border-radius: 4px; color: var(--primary); background: var(--primary-soft); font-size: 9px; font-weight: 700; }:deep(.config-copy code) { color: #6941c6; font-size: 10px; }:deep(.config-copy p) { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.45; }:deep(.config-input) { min-height: 36px !important; }:deep(.config-switch) { display: flex; align-items: center; justify-content: flex-end; gap: 8px; color: var(--muted); font-size: 11px; }
@media (max-width: 850px) { :deep(.config-row) { grid-template-columns: 1fr; align-items: stretch; }:deep(.config-switch) { justify-content: flex-start; } }
</style>
