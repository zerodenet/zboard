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
        <p v-if="detail.installed">本地版本 v{{ detail.installed.version }} · {{ pluginStateLabel(detail.installed.state) }}</p>
        <p v-if="detail.notice" role="status">{{ detail.notice }}</p>
        <template v-if="detail.release">
          <p>市场版本 v{{ detail.release.version }} · 服务器平台 {{ detail.platform }}</p>
          <div class="plugin-card-actions">
            <UiButton :disabled="!hostArtifact || samePackage || loading" @click="installOpen = true">{{ samePackage ? '当前版本已安装' : '在线安装' }}</UiButton>
            <RouterLink v-if="detail.installed" class="button button-secondary" :to="`/admin/plugins/${encodeURIComponent(id)}`">管理插件</RouterLink>
          </div>
          <p>安装前会展示包内清单、兼容性与签名指纹。新安装默认停用，随后可进入管理页配置、启用或卸载。</p>
        </template>
        <RouterLink v-else-if="detail.installed" class="button button-secondary" :to="`/admin/plugins/${encodeURIComponent(id)}`">管理插件</RouterLink>
        <UiButton v-if="detail.notice" variant="secondary" :disabled="loading" @click="load">刷新发行信息</UiButton>
      </section>
      <section v-if="detail.release" class="plugin-card">
        <h2>下载安装包</h2>
        <label class="package-platform">目标平台<UiSelect v-model="platform" :options="platformOptions" aria-label="安装包平台" /></label>
        <template v-if="download">
          <p>{{ download.size ? `${(download.size / 1024 / 1024).toFixed(2)} MiB · ` : '' }}v{{ detail.release.version }}</p>
          <p class="package-digest">SHA-256：{{ download.sha256 }}</p>
          <a class="button button-secondary" :href="download.url" target="_blank" rel="noopener noreferrer">下载 .zbplugin</a>
          <p>用于其他服务器或离线导入。在线安装始终选择当前服务器适用的安装包。</p>
        </template>
      </section>
      <a v-if="detail.entry.repository" class="plugin-text-link" :href="detail.entry.repository" target="_blank" rel="noopener noreferrer">查看源码与发行记录</a>
    </template>
    <PluginImportDialog :open="installOpen" :market-id="id" @close="installOpen = false" @imported="installed" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageAlert from '../components/PageAlert.vue'
import UiButton from '../components/UiButton.vue'
import UiSelect from '../components/UiSelect.vue'
import PluginImportDialog from '../plugins/PluginImportDialog.vue'
import { fetchMarketDetail, pluginStateLabel, type MarketDetail, type Plugin } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import '../styles/plugins.css'
const route = useRoute(), router = useRouter()
const id = computed(() => String(route.params.pluginId || ''))
const installOpen = ref(false), platform = ref('')
const resource = useRemoteResource<MarketDetail | null>({ initial: () => null, fetch: ({ signal }) => fetchMarketDetail(id.value, signal), errorMessage: '无法读取插件发行信息，请重试。' })
const { data: detail, loading, error, load } = resource
const hostArtifact = computed(() => detail.value?.release?.artifacts.find(a => a.platform === detail.value?.platform) || detail.value?.release?.artifacts.find(a => a.platform === 'any'))
const samePackage = computed(() => detail.value?.installed?.state !== 'uninstalled' && !!hostArtifact.value && detail.value?.installed?.digest === hostArtifact.value.sha256)
const platformOptions = computed(() => detail.value?.release?.artifacts.map(a => ({ label: a.platform, value: a.platform })) || [])
const download = computed(() => detail.value?.release?.artifacts.find(a => a.platform === platform.value))
watch(detail, value => { platform.value = hostArtifact.value?.platform || value?.release?.artifacts[0]?.platform || '' })
watch(id, () => { installOpen.value = false; resource.reset(); void load() }, { immediate: true })
function installed(plugin: Plugin) { installOpen.value = false; void router.push({ path: `/admin/plugins/${encodeURIComponent(plugin.id)}`, query: { imported: '1' } }) }
</script>
<style scoped>
.package-platform { display: grid; gap: .5rem; max-width: 22rem; }
.package-digest { overflow-wrap: anywhere; font-family: monospace; }
</style>
