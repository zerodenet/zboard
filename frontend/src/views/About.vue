<template>
  <section class="standard-page about-page">
    <PageHeader title="关于 Zboard" description="查看当前运行版本、开源许可和 ZeroDeNet 项目资源。" eyebrow="Open Source">
      <template #actions>
        <PageRefreshButton label="刷新运行信息" :loading="loading" @click="loadInfo" />
      </template>
    </PageHeader>

    <PageAlert v-if="error" tone="danger" title="运行信息加载失败">{{ error }}</PageAlert>

    <div v-if="info" class="section-grid about-summary">
      <UiSection class="span-7 project-card" title="Zboard" description="面向代理服务运营场景的一体化控制面。">
        <template #meta><StatusBadge tone="success">Open Source</StatusBadge></template>
        <div class="panel-body project-copy">
          <p>Zboard 将节点资产、协议服务、订阅交付、流量统计与日常运营集中在一个可审计的管理后台中，并保持与 ZeroDeNet 其他项目清晰的集成边界。</p>
          <div class="project-actions">
            <a class="button button-sm" :href="repositoryURL" target="_blank" rel="noopener noreferrer">查看源码</a>
            <a class="button button-secondary button-sm" :href="docsURL" target="_blank" rel="noopener noreferrer">阅读文档</a>
          </div>
        </div>
      </UiSection>

      <UiSection class="span-5" title="当前版本" description="本实例正在运行的 Zboard 构建。">
        <template #meta><StatusBadge :tone="channelTone">{{ channelLabel }}</StatusBadge></template>
        <div class="panel-body runtime-version">
          <strong>{{ info.version || 'unknown' }}</strong>
          <p>许可：{{ info.license.name }}（{{ info.license.spdx }}）</p>
          <a class="update-link" :href="info.update_url" target="_blank" rel="noopener noreferrer"><UiIcon name="activity" />查看 Releases / 检查更新</a>
        </div>
      </UiSection>
    </div>

    <div v-if="info" class="section-grid about-runtime">
      <UiSection class="span-6" title="运行信息" description="当前管理服务的只读运行事实。">
        <div class="panel-body fact-list">
          <div><span>服务启动时间</span><strong>{{ formatDateTime(info.started_at) }}</strong></div>
          <div><span>本次运行时长</span><strong>{{ formatDuration(info.uptime_seconds) }}</strong></div>
          <div><span>系统安装时间</span><strong>{{ formatDateTime(info.installed_at) }}</strong></div>
          <div><span>发行通道</span><strong>{{ channelLabel }}</strong></div>
        </div>
      </UiSection>

      <UiSection class="span-6" title="开源许可" description="本项目采用 Mozilla Public License 2.0。">
        <div class="panel-body license-copy">
          <div class="license-mark"><UiIcon name="shield" /><div><strong>{{ info.license.spdx }}</strong><span>{{ info.license.edition === 'open-source' ? '开源版本' : info.license.edition }}</span></div></div>
          <p>你可以查看、修改和参与 Zboard。分发修改过的受 MPL 覆盖文件时，需要遵守 MPL-2.0 对源代码公开的要求；具体权利与义务以仓库 LICENSE 为准。</p>
          <a :href="licenseURL" target="_blank" rel="noopener noreferrer">查看 LICENSE</a>
        </div>
      </UiSection>
    </div>

    <UiSection v-if="info" title="项目与社区" description="源码、文档、版本发布和问题反馈的官方入口。">
      <div class="resource-grid">
        <a v-for="link in info.links" :key="link.url" class="resource-card" :href="link.url" target="_blank" rel="noopener noreferrer">
          <span>{{ link.label }}</span><UiIcon name="chevron" />
        </a>
      </div>
    </UiSection>

    <UiSection v-if="info" class="contribute-card" title="参与 ZeroDeNet" description="Zboard 仍在持续演进，真实部署反馈会直接影响下一轮设计。">
      <div class="panel-body contribute-copy">
        <p>遇到问题可以从 Issues 提交可复现信息；如果你正在部署或扩展 Zboard，也欢迎通过源码仓库参与实现、文档和兼容性验证。</p>
        <a class="button button-secondary button-sm" :href="issuesURL" target="_blank" rel="noopener noreferrer">提交问题</a>
      </div>
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchAdminSystemInfo, type AdminSystemInfo } from '../api/system'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'

const loading = ref(false)
const error = ref('')
const info = ref<AdminSystemInfo | null>(null)

const repositoryURL = computed(() => info.value?.links.find(item => item.label === '源码仓库')?.url || 'https://github.com/zerodenet/zboard')
const docsURL = computed(() => info.value?.links.find(item => item.label === '项目文档')?.url || 'https://docs.zerodenet.org')
const issuesURL = computed(() => info.value?.links.find(item => item.label === '问题反馈')?.url || 'https://github.com/zerodenet/zboard/issues')
const licenseURL = computed(() => `${repositoryURL.value}/blob/develop/LICENSE`)
const channelLabel = computed(() => ({
  development: '开发版本',
  'release-candidate': '候选版本',
  preview: '预览版本',
  stable: '稳定版本',
}[info.value?.release_channel || ''] || '未知通道'))
const channelTone = computed(() => info.value?.release_channel === 'stable' ? 'success' : 'warning')

async function loadInfo() {
  loading.value = true
  error.value = ''
  try { info.value = await fetchAdminSystemInfo() }
  catch (cause: any) { error.value = cause?.message || '无法加载系统信息。' }
  finally { loading.value = false }
}

function formatDateTime(value: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

function formatDuration(totalSeconds: number) {
  const seconds = Math.max(0, Math.floor(Number(totalSeconds) || 0))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days} 天 ${hours} 小时`
  if (hours > 0) return `${hours} 小时 ${minutes} 分钟`
  return `${minutes} 分钟`
}

onMounted(loadInfo)
</script>

<style scoped>
.about-page { display: grid; gap: 16px; }.about-summary,.about-runtime { margin: 0; }.project-copy,.runtime-version,.license-copy,.contribute-copy { display: grid; gap: 14px; }.project-copy p,.license-copy p,.contribute-copy p { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.7; }.project-actions { display: flex; flex-wrap: wrap; gap: 8px; }.runtime-version > strong { font-size: 28px; letter-spacing: -.03em; }.runtime-version > p { margin: -8px 0 0; color: var(--muted); font-size: 11px; }.update-link,.license-copy > a { width: fit-content; display: inline-flex; align-items: center; gap: 6px; color: var(--primary); font-size: 11px; font-weight: 700; text-decoration: none; }.fact-list { display: grid; }.fact-list > div { display: flex; justify-content: space-between; gap: 18px; padding: 11px 0; }.fact-list > div + div { border-top: 1px solid var(--line); }.fact-list span { color: var(--muted); font-size: 11px; }.fact-list strong { text-align: right; font-size: 11px; }.license-mark { display: flex; align-items: center; gap: 12px; }.license-mark > .ui-icon { width: 38px; height: 38px; padding: 9px; border-radius: 10px; color: var(--success); background: var(--success-soft); }.license-mark div { display: grid; gap: 2px; }.license-mark strong { font-size: 16px; }.license-mark span { color: var(--muted); font-size: 10px; }.resource-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; padding: 20px; }.resource-card { min-height: 56px; display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 13px 14px; border: 1px solid var(--line); border-radius: 10px; color: var(--text); background: var(--surface); font-size: 12px; font-weight: 700; text-decoration: none; transition: border-color .15s ease, background .15s ease; }.resource-card:hover { border-color: var(--primary); background: var(--primary-soft); }.resource-card .ui-icon { color: var(--muted); }.contribute-card { margin-top: 0; }@media (max-width: 900px) { .resource-grid { grid-template-columns: 1fr 1fr; } }@media (max-width: 620px) { .resource-grid { grid-template-columns: 1fr; }.fact-list > div { display: grid; gap: 4px; }.fact-list strong { text-align: left; } }
</style>
