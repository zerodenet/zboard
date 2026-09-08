<template>
  <div class="plugins-page">
    <header class="plugins-heading">
      <div>
        <p class="eyebrow">扩展中心</p>
        <h1>插件管理</h1>
        <p>
          查看已安装插件，管理启停与配置。
        </p>
      </div>
      <div class="plugins-actions">
        <RouterLink class="button button-secondary" to="/admin/plugin-market"
          >浏览插件市场</RouterLink
        ><UiButton :disabled="busy" @click="importOpen = true">离线导入</UiButton>
      </div>
    </header>
    <TransientFeedback :success="message" />
    <p v-if="error || actionError" role="alert" class="plugins-error">
      {{ actionError || error }}
    </p>
    <div class="plugins-toolbar">
      <UiInput
        v-model="query"
        placeholder="搜索插件名称或 ID"
        aria-label="搜索插件"
      /><UiSelect v-model="surface" aria-label="页面位置" :options="surfaceOptions" /><button type="button" :disabled="loading || busy" @click="load">
        刷新</button
      ><span>{{ items.length }} 个插件</span>
    </div>
    <p v-if="loading && !items.length">正在读取插件…</p>
    <section v-else-if="!filtered.length" class="plugins-empty">
      <h2>{{ items.length ? "没有匹配的插件" : "还没有安装插件" }}</h2>
      <p>从插件市场选择扩展，或导入发布者提供的签名 .zbplugin 文件。</p>
      <p>离线导入不需要连接市场，安装后默认停用。</p>
    </section>
    <div class="plugin-grid">
      <article v-for="plugin in filtered" :key="plugin.id" class="plugin-card">
        <div class="plugin-card-top">
          <div class="plugin-monogram">{{ plugin.name.slice(0, 1) }}</div>
          <span class="plugin-status" :class="plugin.state">{{
            pluginStateLabel(plugin.state)
          }}</span>
        </div>
        <h2>{{ plugin.name }}</h2>
        <p class="plugin-id">{{ plugin.id }} · v{{ plugin.version }}</p>
        <p>{{ plugin.manifest.description || "此插件未提供描述。" }}</p>
        <p>{{ pluginBusinessLabel(plugin) }}</p>
        <p v-if="!plugin.admission?.accepted">宿主校验未通过，请查看详情。</p>
        <div class="plugin-tags">
          <span v-for="s in plugin.manifest.surfaces" :key="s">{{
            surfaceLabel(s)
          }}</span
          ><span>{{
            plugin.manifest.components.server ? "包含服务端组件" : "页面扩展"
          }}</span>
        </div>
        <p v-if="plugin.last_error" class="plugins-error">
          {{ plugin.last_error }}
        </p>
        <div class="plugin-card-actions">
          <UiButton
            v-if="plugin.enabled"
            variant="secondary"
            :disabled="busy"
            @click="act(plugin, 'disable')"
            >停用</UiButton
          ><UiButton
            v-else-if="plugin.state !== 'uninstalled'"
            :disabled="busy || !plugin.compatibility.compatible || !canPluginRun(plugin)"
            @click="act(plugin, 'enable')"
            >启用</UiButton
          >
          <RouterLink class="button button-secondary" :to="pluginDetailPath(plugin.id)">查看详情</RouterLink>
          <RouterLink v-if="hasPluginConfig(plugin) && plugin.compatibility.compatible" class="plugin-text-link" :to="`${pluginDetailPath(plugin.id)}/configuration`">配置</RouterLink>
        </div>
      </article>
    </div>
    <PluginImportDialog :open="importOpen" @close="importOpen = false" @imported="imported" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import PluginImportDialog from '../plugins/PluginImportDialog.vue'
import { useRemoteResource } from '../composables/useRemoteResource'
import { usePluginActions, pluginDetailPath, hasPluginConfig, canPluginRun } from '../plugins/usePluginManagement'
import { fetchPlugins, surfaceLabel, pluginStateLabel, pluginBusinessLabel, type Plugin, type Surface } from '../api/plugins'
import '../styles/plugins.css'
const router = useRouter(), route = useRoute()
const { data: items, loading, error, load } = useRemoteResource<Plugin[]>({
  initial: () => [], fetch: ({ signal }) => fetchPlugins(signal), errorMessage: '无法读取插件列表，请重试。',
})
const { busy, error: actionError, message, act } = usePluginActions(load)
const importOpen = ref(false)
const surfaceOptions = [{ label: '所有页面位置', value: '' }, { label: '公开前台', value: 'public' }, { label: '用户前台', value: 'account' }, { label: '管理后台', value: 'admin' }]
const query = computed({ get: () => String(route.query.q || ''), set: q => { void router.replace({ query: { ...route.query, q: q || undefined } }) } })
const surface = computed({ get: () => String(route.query.surface || ''), set: surface => { void router.replace({ query: { ...route.query, surface: surface || undefined } }) } })
const filtered = computed(() => items.value.filter(p => `${p.name} ${p.id}`.toLowerCase().includes(query.value.toLowerCase()) && (!surface.value || p.manifest.surfaces.includes(surface.value as Surface))))
function imported(plugin: Plugin) {
  importOpen.value = false
  void router.push({ path: pluginDetailPath(plugin.id), query: { imported: '1' } })
}
onMounted(load)
</script>
