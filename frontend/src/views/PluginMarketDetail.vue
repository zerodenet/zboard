<template>
  <div class="plugins-page">
    <header class="plugins-heading">
      <div><p class="eyebrow">扩展中心 / 插件市场</p><h1>{{ detail?.entry.name || '插件详情' }}</h1><p>{{ detail?.entry.description }}</p></div>
      <RouterLink class="button button-secondary" to="/admin/plugin-market">返回市场</RouterLink>
    </header>
    <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton @click="load">重试</UiButton></template></PageAlert>
    <p v-if="loading" role="status">正在读取发行信息…</p>
    <template v-if="detail">
      <section class="plugin-card">
        <h2>发行与安装</h2>
        <p>{{ detail.entry.id }} · 发布者 {{ detail.entry.publisher }}</p>
        <p v-if="detail.entry.license">许可证 {{ detail.entry.license }}<template v-if="detail.entry.maintainers?.length"> · 维护者 {{ detail.entry.maintainers.join('、') }}</template></p>
        <div class="plugin-card-actions">
          <a v-if="detail.entry.homepage" class="plugin-text-link" :href="detail.entry.homepage" target="_blank" rel="noopener noreferrer">项目主页</a>
          <a v-if="detail.entry.documentation" class="plugin-text-link" :href="detail.entry.documentation" target="_blank" rel="noopener noreferrer">使用文档</a>
          <a v-if="detail.entry.security" class="plugin-text-link" :href="detail.entry.security" target="_blank" rel="noopener noreferrer">安全联系</a>
        </div>
        <p v-if="detail.installed">本地版本 v{{ detail.installed.version }} · {{ pluginStateLabel(detail.installed.state) }}</p>
        <p v-if="detail.notice" role="status">{{ detail.notice }}</p>
        <template v-if="selectedRelease">
          <div class="release-selectors">
            <label>版本通道<UiSelect v-model="channel" :options="channelOptions" aria-label="版本通道" /></label>
            <label>插件版本<UiSelect v-model="version" :options="versionOptions" aria-label="插件版本" /></label>
          </div>
          <p>已选 {{ channelLabel(selectedRelease.channel) }} v{{ selectedRelease.version }} · 服务器平台 {{ detail.platform }}</p>
          <p v-if="!hostArtifact" role="status">此版本没有适用于当前服务器的安装包，可以下载其他平台的安装包。</p>
          <div class="plugin-card-actions">
            <UiButton :disabled="!hostArtifact || samePackage || loading" @click="installOpen = true">{{ samePackage ? '当前版本已安装' : detail.installed ? '切换到此版本' : '在线安装' }}</UiButton>
            <RouterLink v-if="detail.installed" class="button button-secondary" :to="`/admin/plugins/${encodeURIComponent(id)}`">管理插件</RouterLink>
          </div>
          <p>安装前会展示包内清单、兼容性与签名指纹。新安装默认停用，随后可进入管理页配置、启用或卸载。</p>
        </template>
        <RouterLink v-else-if="detail.installed" class="button button-secondary" :to="`/admin/plugins/${encodeURIComponent(id)}`">管理插件</RouterLink>
        <UiButton v-if="detail.notice" variant="secondary" :disabled="loading" @click="load">刷新发行信息</UiButton>
      </section>
      <section v-if="selectedRelease" class="plugin-card">
        <h2>{{ selectedRelease.title || `v${selectedRelease.version}` }}</h2>
        <p v-if="selectedRelease.published_at">发布于 {{ formatDateTime(selectedRelease.published_at) }}</p>
        <MarkdownContent v-if="selectedRelease.notes" class="release-notes" :content="selectedRelease.notes" />
        <p v-else>发布者未提供版本说明。</p>
        <a v-if="selectedRelease.url" class="plugin-text-link" :href="selectedRelease.url" target="_blank" rel="noopener noreferrer">查看 GitHub Release</a>
      </section>
      <section v-if="selectedRelease" class="plugin-card">
        <h2>下载安装包</h2>
        <label class="package-platform">目标平台<UiSelect v-model="platform" :options="platformOptions" aria-label="安装包平台" /></label>
        <template v-if="download">
          <p>{{ download.size ? `${(download.size / 1024 / 1024).toFixed(2)} MiB · ` : '' }}v{{ selectedRelease.version }}</p>
          <p class="package-digest">SHA-256：{{ download.sha256 }}</p>
          <a class="button button-secondary" :href="download.url" target="_blank" rel="noopener noreferrer">下载 .zbplugin</a>
          <p>用于其他服务器或离线导入。在线安装始终选择当前服务器适用的安装包。</p>
        </template>
      </section>
      <a v-if="detail.entry.repository" class="plugin-text-link" :href="detail.entry.repository" target="_blank" rel="noopener noreferrer">查看源码与发行记录</a>
    </template>
    <PluginImportDialog :open="installOpen" :market-id="id" :market-version="version" @close="installOpen = false" @imported="installed" />
  </div>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { formatDateTime } from '../utils/format'
import { useRoute, useRouter } from 'vue-router'
import PageAlert from '../components/PageAlert.vue'
import MarkdownContent from '../components/MarkdownContent.vue'
import UiButton from '../components/UiButton.vue'
import UiSelect from '../components/UiSelect.vue'
import PluginImportDialog from '../plugins/PluginImportDialog.vue'
import { fetchMarketDetail, pluginStateLabel, type MarketDetail, type MarketReleaseChannel, type Plugin } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import '../styles/plugins.css'
const route = useRoute(), router = useRouter()
const id = computed(() => String(route.params.pluginId || ''))
const installOpen = ref(false), platform = ref(''), channel = ref<MarketReleaseChannel>('stable'), version = ref('')
const versionLoading = ref(false), versionError = ref('')
let versionController: AbortController | undefined
const resource = useRemoteResource<MarketDetail | null>({ initial: () => null, fetch: ({ signal }) => fetchMarketDetail(id.value, '', signal), errorMessage: '无法读取插件发行信息，请重试。' })
const { data: detail, loading: detailLoading, error: detailError, load } = resource
const loading = computed(() => detailLoading.value || versionLoading.value)
const error = computed(() => detailError.value || versionError.value)
const releases = computed(() => detail.value?.releases?.length ? detail.value.releases : (detail.value?.release ? [detail.value.release] : []))
const selectedRelease = computed(() => releases.value.find(release => release.version === version.value) || detail.value?.release)
const hostArtifact = computed(() => selectedRelease.value?.artifacts.find(a => a.platform === detail.value?.platform) || selectedRelease.value?.artifacts.find(a => a.platform === 'any'))
const samePackage = computed(() => detail.value?.installed?.state !== 'uninstalled' && !!hostArtifact.value && detail.value?.installed?.digest === hostArtifact.value.sha256)
const channelOptions = computed(() => (['stable', 'rc', 'dev'] as MarketReleaseChannel[]).filter(value => releases.value.some(release => release.channel === value)).map(value => ({ label: channelLabel(value), value })))
const versionOptions = computed(() => releases.value.filter(release => release.channel === channel.value).map(release => ({ label: `v${release.version}`, value: release.version })))
const platformOptions = computed(() => selectedRelease.value?.artifacts.map(a => ({ label: a.platform, value: a.platform })) || [])
const download = computed(() => selectedRelease.value?.artifacts.find(a => a.platform === platform.value))
watch(detail, value => {
  const selected = value?.release || value?.releases?.[0]
  channel.value = selected?.channel || 'stable'; version.value = selected?.version || ''
})
watch(channel, value => {
  const next = releases.value.find(release => release.channel === value)
  if (next && next.version !== version.value) version.value = next.version
})
watch(version, async value => {
  versionController?.abort()
  versionController = undefined; versionLoading.value = false; versionError.value = ''
  if (!value || value === detail.value?.release?.version) return
  const controller = new AbortController()
  versionController = controller; versionLoading.value = true
  try {
    const selected = await fetchMarketDetail(id.value, value, controller.signal)
    if (!controller.signal.aborted && version.value === value) resource.replace(selected)
  } catch (cause: any) {
    if (!controller.signal.aborted && version.value === value) versionError.value = cause?.response?.data?.message || '无法读取所选版本的发行信息，请重试。'
  } finally {
    if (versionController === controller) { versionController = undefined; versionLoading.value = false }
  }
})
watch(selectedRelease, value => {
  installOpen.value = false
  platform.value = hostArtifact.value?.platform || value?.artifacts[0]?.platform || ''
})
watch(id, () => { versionController?.abort(); versionError.value = ''; installOpen.value = false; resource.reset(); void load() }, { immediate: true })
onScopeDispose(() => versionController?.abort())
function installed(plugin: Plugin) { installOpen.value = false; void router.push({ path: `/admin/plugins/${encodeURIComponent(plugin.id)}`, query: { imported: '1' } }) }
function channelLabel(value: MarketReleaseChannel) { return ({ stable: '正式版 / Latest', rc: 'RC', dev: 'Dev' })[value] }
</script>
<style scoped>
.package-platform { display: grid; gap: .5rem; max-width: 22rem; }
.release-selectors { display: grid; grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr)); gap: 1rem; max-width: 36rem; }
.release-selectors label { display: grid; gap: .5rem; }
.package-digest { overflow-wrap: anywhere; font-family: monospace; }
.release-notes { white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
