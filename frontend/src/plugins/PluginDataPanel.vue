<template>
  <section class="plugin-data-status">
    <h3>数据状态</h3>
    <p>安装、升级和恢复均由 ZBoard 管理数据版本。插件数据不会覆盖核心用户、身份绑定和审计记录。</p>
    <dl class="plugin-facts">
      <div><dt>私有存储</dt><dd>{{ plugin.manifest.capabilities.includes('zboard.storage.v1') ? '加密 JSON · 128 个键 / 256 KiB' : '未声明私有存储能力' }}</dd></div>
      <div><dt>已应用数据版本</dt><dd>{{ plugin.data.version }} / 目标 {{ plugin.data.target_version }} · 修订 {{ plugin.data.revision }}</dd></div>
      <div><dt>迁移状态</dt><dd>{{ plugin.data.migration_required ? '有待应用的迁移' : plugin.data.compatible ? '数据版本兼容' : '当前程序与数据不兼容' }}</dd></div>
    </dl>
    <PageAlert v-if="plugin.data.migration_required || !plugin.data.compatible" tone="warning">数据准备尚未完成，系统会阻止启用。重新导入插件时会自动重试，失败原因见操作记录。</PageAlert>
    <h3>迁移记录</h3>
    <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton @click="load">重试</UiButton></template></PageAlert>
    <p v-if="loading">正在读取迁移记录…</p><p v-else-if="!records.length && !error">暂无已提交的迁移。失败或中断的尝试可在“操作记录”查看。</p>
    <details v-for="record in records" :key="record.id"><summary>数据版本 {{ record.version }} · 数据周期 {{ record.epoch }} · {{ formatDateTime(record.created_at) }}</summary><p>{{ record.actor }}</p><p class="plugin-digest">迁移校验和：{{ record.checksum }}</p></details>
    <p>卸载默认保留配置和私有数据；重新安装时自动校验并恢复使用。</p>
  </section>
</template>
<script setup lang="ts">
import { watch } from 'vue'
import PageAlert from '../components/PageAlert.vue'
import UiButton from '../components/UiButton.vue'
import { fetchPluginMigrations, type Plugin, type PluginMigration } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { formatDateTime } from '../utils/format'
const props = defineProps<{ plugin: Plugin }>()
const { data: records, loading, error, load, reset } = useRemoteResource<PluginMigration[]>({ initial: () => [], fetch: ({ signal }) => fetchPluginMigrations(props.plugin.id, signal), errorMessage: '迁移记录读取失败。' })
watch(() => [props.plugin.id, props.plugin.data.version, props.plugin.data.epoch], () => { reset(); void load() }, { immediate: true })
</script>
