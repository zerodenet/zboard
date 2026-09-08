<template>
  <div class="plugins-page">
    <nav class="plugin-breadcrumb" aria-label="面包屑"><RouterLink to="/admin/plugins">插件管理</RouterLink><span>/</span><span>{{ plugin?.name || '插件详情' }}</span></nav>
    <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton variant="secondary" @click="load">重试</UiButton></template></PageAlert>
    <p v-if="loading && !plugin" role="status">正在读取插件…</p>
    <section v-else-if="!plugin && !error" class="plugins-empty"><h1>未找到插件</h1><p>插件可能已移除，请返回列表检查。</p></section>
    <template v-if="plugin">
      <header class="plugins-heading">
        <div><p class="eyebrow">插件详情</p><h1>{{ plugin.name }}</h1><p>{{ plugin.id }} · v{{ plugin.version }} <span class="plugin-status" :class="plugin.state">{{ pluginStateLabel(plugin.state) }}</span></p></div>
        <div class="plugins-actions">
          <UiButton v-if="plugin.enabled" variant="secondary" :disabled="busy" @click="act(plugin, 'disable')">停用插件</UiButton>
          <UiButton v-else-if="plugin.state !== 'uninstalled'" variant="secondary" :disabled="busy || !plugin.compatibility.compatible || !canPluginRun(plugin)" @click="act(plugin, 'enable')">启用插件</UiButton>
          <RouterLink v-if="hasPluginConfig(plugin) && plugin.compatibility.compatible" class="button" :to="`${pluginDetailPath(plugin.id)}/configuration`">配置插件</RouterLink>
        </div>
      </header>
      <TransientFeedback :success="message" />
      <PageAlert v-if="!plugin.admission?.accepted" tone="warning">插件尚未通过宿主校验，无法运行。请查看操作记录。</PageAlert>
      <PageAlert v-if="route.query.imported === '1' && !plugin.enabled" tone="success">插件已安装，系统已完成能力校验和数据准备。完成配置后即可启用。</PageAlert>
      <PageAlert v-if="actionError || plugin.last_error" tone="danger">{{ actionError || plugin.last_error }}</PageAlert>
      <nav class="plugin-tabs" aria-label="插件详情栏目">
        <RouterLink v-for="item in tabs" :key="item.id" :to="{ path: pluginDetailPath(plugin.id), query: item.id === 'overview' ? {} : { tab: item.id } }" :class="{ selected: tab === item.id }" :aria-current="tab === item.id ? 'page' : undefined">{{ item.label }}</RouterLink>
      </nav>
      <section v-if="tab === 'overview'" class="plugin-detail">
        <h2>关于插件</h2><p>{{ plugin.manifest.description || '此插件未提供描述。' }}</p>
        <dl class="plugin-facts">
          <div><dt>签名发布者</dt><dd>{{ plugin.publisher }}</dd></div>
          <div><dt>页面位置</dt><dd>{{ plugin.manifest.surfaces.map(surfaceLabel).join('、') }}</dd></div>
          <div><dt>组件</dt><dd>{{ plugin.manifest.components.server ? '包含服务端组件' : '页面扩展' }}</dd></div>
          <div><dt>版本兼容性</dt><dd>{{ !plugin.compatibility.compatible ? '当前宿主不兼容' : plugin.compatibility.tested ? '发布者已测试当前版本' : '兼容范围内，尚未声明测试' }}<small v-if="plugin.compatibility.reason">{{ plugin.compatibility.reason }}</small></dd></div>
          <div><dt>业务作用</dt><dd>{{ pluginBusinessLabel(plugin) }}</dd></div>
          <div><dt>扩展能力</dt><dd>{{ plugin.manifest.capabilities.map(capabilityLabel).join('、') }}<small>由 ZBoard 根据插件声明、调用身份和站点策略约束。</small></dd></div>
          <div><dt>包摘要 SHA-256</dt><dd><code>{{ plugin.digest }}</code></dd></div>
        </dl>
        <PageAlert v-if="plugin.manifest.capabilities.includes('zboard.identity.provider.v1')" tone="info">第三方登录遵守核心账户规则。站点关闭注册时，未绑定的身份不能注册或登录；已绑定的正常账户仍可登录。</PageAlert>
        <div class="plugin-removal">
          <div><h3>{{ plugin.state === 'uninstalled' ? '保留的配置' : '卸载插件' }}</h3><p>{{ plugin.state === 'uninstalled' ? '卸载后保留的配置和私有数据可在重新安装时使用。' : '系统会停止运行并撤销会话，保留配置、私有数据与操作记录。' }}</p></div>
          <UiButton v-if="plugin.state === 'uninstalled'" variant="danger" :disabled="busy" @click="act(plugin, 'purge_data')">清除保留数据</UiButton>
          <UiButton v-else variant="secondary" :disabled="busy" @click="act(plugin, 'uninstall')">卸载插件</UiButton>
        </div>
      </section>
      <section v-else-if="tab === 'versions'" class="plugin-detail">
        <h2>版本管理</h2><p>安装和升级时由系统自动完成数据迁移。恢复旧版本前请先停用；系统会校验数据与配置，失败时保留当前版本。</p>
        <p v-if="!plugin.versions.length">暂无保留的版本。</p>
        <div v-for="version in plugin.versions" :key="version.id" class="plugin-history">
          <span><strong>v{{ version.version }}</strong><small>{{ formatDateTime(version.created_at) }}</small><small>{{ version.digest }}</small></span>
          <span v-if="version.id === plugin.version_id" class="plugin-status">当前版本</span>
          <UiButton v-else variant="secondary" :disabled="busy || plugin.enabled || plugin.state === 'uninstalled'" @click="act(plugin, 'rollback', version.id)">恢复此版本</UiButton>
        </div>
        <PluginDataPanel :plugin="plugin" />
      </section>
      <section v-else class="plugin-detail">
        <header><h2>最近操作</h2><UiButton variant="secondary" :disabled="opsLoading" @click="loadOperations">刷新记录</UiButton></header>
        <PageAlert v-if="opsError" tone="danger">{{ opsError }}</PageAlert>
        <p v-if="opsLoading" role="status">正在读取操作记录…</p><p v-else-if="!operations.length && !opsError">暂无操作记录。</p>
        <div v-for="op in visibleOperations" :key="op.id" class="plugin-history"><span><strong>{{ actionLabels[op.action] || op.action }}</strong> · {{ operationStateLabels[op.state] || op.state }}<small>{{ op.actor }} · {{ formatDateTime(op.created_at) }}</small></span><span>{{ op.message }}</span></div>
        <div v-if="operations.length > 10" class="plugins-actions plugin-operation-paging">
          <span>最近 {{ operations.length }} 条 · 第 {{ operationPage }} / {{ operationPages }} 页</span>
          <UiButton variant="secondary" :disabled="operationPage <= 1" @click="operationPage--">上一页</UiButton>
          <UiButton variant="secondary" :disabled="operationPage >= operationPages" @click="operationPage++">下一页</UiButton>
        </div>
      </section>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { formatDateTime } from '../utils/format'
import { useRoute } from 'vue-router'
import PluginDataPanel from '../plugins/PluginDataPanel.vue'
import PageAlert from '../components/PageAlert.vue'
import UiButton from '../components/UiButton.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import { fetchPluginOperations, pluginStateLabel, surfaceLabel, pluginBusinessLabel, capabilityLabel } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { usePluginDetail, usePluginActions, pluginDetailPath, hasPluginConfig, actionLabels, canPluginRun } from '../plugins/usePluginManagement'
import '../styles/plugins.css'
const route = useRoute()
const { data: plugin, loading, error, load } = usePluginDetail()
const tabs = [{ id: 'overview', label: '概览' }, { id: 'versions', label: '版本管理' }, { id: 'operations', label: '操作记录' }]
const tab = computed(() => route.query.tab === 'data' ? 'versions' : ['versions', 'operations'].includes(String(route.query.tab)) ? String(route.query.tab) : 'overview')
const { data: operations, loading: opsLoading, error: opsError, load: loadOperations, reset: resetOperations } = useRemoteResource({
  initial: (): Awaited<ReturnType<typeof fetchPluginOperations>> => [],
  fetch: ({ signal }) => fetchPluginOperations(String(route.params.pluginId), signal), errorMessage: '无法读取操作记录，请重试。',
})
const operationPage = ref(1)
const operationPages = computed(() => Math.max(1, Math.ceil(operations.value.length / 10)))
const visibleOperations = computed(() => operations.value.slice((operationPage.value - 1) * 10, operationPage.value * 10))
watch(operations, () => { operationPage.value = 1 })
const { busy, error: actionError, message, act } = usePluginActions(async () => { await load(); if (tab.value === 'operations') await loadOperations() })
watch(() => [route.params.pluginId, tab.value], () => { resetOperations(); if (tab.value === 'operations') void loadOperations() }, { immediate: true })
watch(() => route.params.pluginId, () => { actionError.value = ''; message.value = '' })
const operationStateLabels: Record<string, string> = { pending: '等待中', running: '进行中', succeeded: '已完成', completed: '已完成', failed: '失败', interrupted: '已中断' }
</script>
