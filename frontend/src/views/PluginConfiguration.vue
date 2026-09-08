<template>
  <div class="plugins-page plugin-configuration-page">
    <nav class="plugin-breadcrumb" aria-label="面包屑"><RouterLink to="/admin/plugins">插件管理</RouterLink><span>/</span><RouterLink :to="pluginDetailPath(String(route.params.pluginId))">{{ plugin?.name || '插件详情' }}</RouterLink><span>/</span><span>配置</span></nav>
    <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton variant="secondary" @click="load">重试</UiButton></template></PageAlert>
    <p v-if="loading && !plugin" role="status">正在读取插件…</p>
    <section v-else-if="!plugin && !error" class="plugins-empty"><h1>未找到插件</h1><p>请返回插件管理检查。</p></section>
    <template v-if="plugin">
      <header class="plugins-heading">
        <div><p class="eyebrow">插件配置</p><h1>{{ plugin.name }}</h1><p>设置插件所需的参数，保存后由系统管理配置。</p></div>
        <div v-if="configurable" class="plugins-actions">
          <UiButton v-if="plugin.manifest.components.server" variant="secondary" :loading="testing" :disabled="testing || jsonOpen" @click="test">测试已保存配置</UiButton>
          <UiButton variant="ghost" :disabled="testing || jsonOpen" @click="jsonOpen = true">高级 JSON 配置</UiButton>
        </div>
      </header>
      <TransientFeedback :success="message" />
      <PageAlert v-if="testError" tone="danger">{{ testError }}</PageAlert>
      <PageAlert v-if="!configurable" tone="warning">{{ plugin.state === 'uninstalled' ? '插件已卸载，重新安装后可继续配置。' : !plugin.admission?.accepted ? '插件尚未通过宿主校验，请查看插件详情。' : !plugin.compatibility.compatible ? '此插件与当前宿主不兼容，暂时无法配置。' : '此插件未声明配置能力。' }}</PageAlert>
      <template v-else>
        <div v-if="configPage" v-show="!jsonOpen"><PluginFrame :key="`${plugin.id}:${frameRevision}`" :plugin-id="plugin.id" :page-id="configPage.id" surface="admin" configuration :title="`${plugin.name}配置页面`" /></div>
        <section v-else-if="!jsonOpen" class="plugins-empty"><h2>此插件使用 JSON 配置</h2><p>插件未提供可视化配置页面，请依据发布者文档填写完整配置。</p><UiButton @click="jsonOpen = true">编辑配置</UiButton></section>
        <PluginConfigDialog v-if="jsonOpen" :key="plugin.id" :plugin-id="plugin.id" @close="jsonOpen = false" @saved="saved" />
      </template>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import UiButton from '../components/UiButton.vue'
import PageAlert from '../components/PageAlert.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import PluginFrame from '../plugins/PluginFrame.vue'
import PluginConfigDialog from '../plugins/PluginConfigDialog.vue'
import { usePluginDetail, pluginDetailPath, hasPluginConfig, hasPluginCapability } from '../plugins/usePluginManagement'
import { testPluginConfig } from '../api/plugins'
import '../styles/plugins.css'
const route = useRoute()
const { data: plugin, loading, error, load } = usePluginDetail()
const jsonOpen = ref(false), testing = ref(false), testError = ref(''), message = ref(''), frameRevision = ref(0)
const configurable = computed(() => plugin.value && hasPluginConfig(plugin.value) && plugin.value.compatibility.compatible)
const configPage = computed(() => plugin.value && hasPluginCapability(plugin.value, 'zboard.ui.page.v1') ? plugin.value.manifest.contributions.pages.find(p => p.surface === 'admin' && p.purpose === 'configuration') : undefined)
let generation = 0
watch(() => route.params.pluginId, () => { generation++; jsonOpen.value = false; testing.value = false; testError.value = ''; message.value = '' })
onScopeDispose(() => { generation++ })
function saved() { jsonOpen.value = false; frameRevision.value++; message.value = '配置已加密保存。' }
async function test() {
  if (!plugin.value || testing.value) return
  const current = generation
  testing.value = true; testError.value = ''; message.value = ''
  try { await testPluginConfig(plugin.value.id); if (current === generation) message.value = '插件配置测试通过。' }
  catch (cause: any) { if (current === generation) testError.value = cause?.response?.data?.message || '配置测试失败，请检查已保存的参数。' }
  finally { if (current === generation) testing.value = false }
}
</script>
