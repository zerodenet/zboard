<template>
  <section class="plugin-detail">
    <h2>数据与迁移</h2>
    <p>插件私有数据与核心数据分开管理。用户、第三方身份绑定和审计记录归核心所有。</p>
    <dl class="plugin-facts">
      <div><dt>私有存储</dt><dd>{{ plugin.manifest.capabilities.includes('zboard.storage.v1') ? '加密 JSON · 128 个键 / 256 KiB' : '未声明私有存储能力' }}</dd></div>
      <div><dt>已应用数据版本</dt><dd>{{ plugin.data.version }} / 目标 {{ plugin.data.target_version }} · 修订 {{ plugin.data.revision }}</dd></div>
      <div><dt>迁移状态</dt><dd>{{ plugin.data.migration_required ? '有待应用的迁移' : plugin.data.compatible ? '数据版本兼容' : '当前程序与数据不兼容' }}</dd></div>
    </dl>
    <template v-if="pending.length">
      <h3>待应用的变更</h3>
      <div v-for="step in pending" :key="step.version" class="plugin-migration-step"><strong>数据版本 {{ step.version }}</strong><p v-if="!step.changes.length">初始化版本记录。</p><ul v-else><li v-for="(change, index) in step.changes" :key="index">{{ change.target === 'config' ? '自身配置' : '私有数据' }}：{{ operationLabel[change.operation] || change.operation }} {{ change.key }}<span v-if="change.to"> → {{ change.to }}</span></li></ul></div>
      <PageAlert v-if="plugin.enabled || !plugin.authorization?.reviewed" tone="warning">迁移前需确认当前包的授权并停用插件。</PageAlert>
      <UiButton :disabled="busy || plugin.enabled || !plugin.authorization?.reviewed || plugin.state === 'uninstalled'" @click="$emit('action', 'migrate')">执行数据迁移</UiButton>
    </template>
    <h3>迁移记录</h3>
    <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton @click="load">重试</UiButton></template></PageAlert>
    <p v-if="loading">正在读取迁移记录…</p><p v-else-if="!records.length && !error">暂无已提交的迁移。失败或中断的尝试可在“操作记录”查看。</p>
    <details v-for="record in records" :key="record.id"><summary>数据版本 {{ record.version }} · 数据周期 {{ record.epoch }} · {{ formatDateTime(record.created_at) }}</summary><p>{{ record.actor }}</p><p class="plugin-digest">迁移校验和：{{ record.checksum }}</p></details>
    <div v-if="plugin.state === 'uninstalled'" class="plugin-removal"><div><h3>永久清除插件数据</h3><p>同时删除自身配置与私有数据，保留核心账户、身份绑定和操作、迁移记录。再次导入后需重新初始化。</p></div><UiButton variant="danger" :disabled="busy" @click="$emit('action', 'purge_data')">清除配置与私有数据</UiButton></div>
    <p v-else>卸载默认保留配置和私有数据，卸载后可单独执行永久清除。</p>
  </section>
</template>
<script setup lang="ts">
import { computed, watch } from 'vue'
import PageAlert from '../components/PageAlert.vue'
import UiButton from '../components/UiButton.vue'
import { fetchPluginMigrations, type Plugin, type PluginMigration } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { formatDateTime } from '../utils/format'
const props = defineProps<{ plugin: Plugin; busy: boolean }>()
defineEmits<{ action: [action: string] }>()
const pending = computed(() => props.plugin.manifest.data?.migrations.filter(m => m.version > props.plugin.data.version) || [])
const operationLabel: Record<string, string> = { set_default: '补充默认键', rename: '重命名键', remove: '移除键' }
const { data: records, loading, error, load, reset } = useRemoteResource<PluginMigration[]>({ initial: () => [], fetch: ({ signal }) => fetchPluginMigrations(props.plugin.id, signal), errorMessage: '迁移记录读取失败。' })
watch(() => [props.plugin.id, props.plugin.data.version, props.plugin.data.epoch], () => { reset(); void load() }, { immediate: true })
</script>
