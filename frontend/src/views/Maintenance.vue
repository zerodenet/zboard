<template>
  <section class="standard-page">
    <PageHeader title="系统维护" description="统一控制整站维护、数据库迁移与切换后的恢复步骤。" eyebrow="Operations">
      <template #actions><PageRefreshButton :loading="loading" label="刷新维护状态" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :success="success" :error="error" />

    <UiSection title="整站维护开关" description="开启后普通用户、节点上报与业务接口统一返回维护状态；管理员控制台、健康检查和状态接口继续可用。">
      <template #meta><StatusBadge :tone="maintenance.enabled ? 'warning' : 'success'">{{ maintenance.enabled ? '维护中' : '正常服务' }}</StatusBadge></template>
      <div class="panel-body stack">
        <label class="toggle-row"><UiCheckbox v-model="maintenance.enabled" :disabled="maintenance.migration_in_progress" /><span><strong>开启整站维护</strong><small>数据库迁移运行时无法关闭。</small></span></label>
        <div class="form-grid">
          <FormField v-slot="{ controlAttrs }" label="维护页标题" name="maintenance-title"><UiInput v-model.trim="maintenance.title" v-bind="controlAttrs" maxlength="160" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="维护说明" name="maintenance-message"><UiTextarea v-model="maintenance.message" v-bind="controlAttrs" maxlength="4000" rows="4" /></FormField>
        </div>
        <div class="form-actions"><UiButton type="button" :loading="saving" @click="saveMaintenance">保存维护设置</UiButton></div>
      </div>
    </UiSection>

    <UiSection title="MySQL / SQLite 数据迁移" description="将当前数据库完整复制到另一种驱动。系统先预检空目标库，再自动进入维护、复制所有业务和观测表并逐表校验数量。">
      <template #meta><StatusBadge :tone="taskTone">{{ taskLabel }}</StatusBadge></template>
      <div class="panel-body stack">
        <PageAlert tone="warning" title="迁移不会自动热切换">
          完成后维护模式会保持开启。请修改部署环境中的 ZBOARD_DATABASE_DRIVER 与 ZBOARD_DATA_SOURCE，重启并验证新数据库，再手动关闭维护模式。MySQL 作为源库时，迁移账号还需要 RELOAD 权限以获取一致性只读锁。
        </PageAlert>
        <div class="migration-summary"><span>当前驱动</span><strong>{{ migration.source_driver.toUpperCase() }}</strong><span v-if="migration.task">最近任务 #{{ migration.task.id }} · {{ migration.task.current }}/{{ migration.task.total }}</span></div>
        <div class="form-grid">
          <FormField v-slot="{ controlAttrs }" label="目标驱动" name="migration-driver"><UiSelect v-model="targetDriver" v-bind="controlAttrs" :options="targetOptions" /></FormField>
          <DatabaseMigrationDatasource v-model="targetDatasource" :driver="targetDriver" />
        </div>
        <PageAlert v-if="preflight" tone="success" title="预检通过">目标 {{ preflight.target }} 可用且为空，将迁移 {{ preflight.tables }} 张逻辑表。</PageAlert>
        <PageAlert v-if="migration.task?.errors" tone="danger" title="迁移失败">{{ migration.task.errors }}</PageAlert>
        <PageAlert v-if="migration.next_step" tone="info" title="下一步">{{ migration.next_step }}</PageAlert>
        <label class="toggle-row"><UiCheckbox v-model="confirmed" /><span><strong>我已备份当前数据库并确认目标库为空</strong><small>迁移开始后将立即开启整站维护。</small></span></label>
        <div class="form-actions">
          <UiButton variant="secondary" type="button" :loading="preflighting" :disabled="taskRunning || !targetDatasource" @click="runPreflight">连接预检</UiButton>
          <UiButton type="button" :loading="starting" :disabled="taskRunning || !preflight || !confirmed" @click="startMigration">开始迁移</UiButton>
        </div>
      </div>
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { fetchDatabaseMigrationStatus, fetchSystemConfigs, preflightDatabaseMigration, startDatabaseMigration, updateMaintenanceSettings, type DatabaseMigrationStatus, type MaintenanceState, type SystemConfig } from '../api/client'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import DatabaseMigrationDatasource from '../components/DatabaseMigrationDatasource.vue'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'

const app = useAppStore()
const loading = ref(false), saving = ref(false), preflighting = ref(false), starting = ref(false)
const success = ref(''), error = ref('')
const configs = ref<SystemConfig[]>([])
const maintenance = reactive<MaintenanceState>({ enabled: false, title: '系统维护中', message: '系统正在维护，请稍后再试。', migration_in_progress: false, migration_cutover_pending: false })
const migration = reactive<DatabaseMigrationStatus>({ source_driver: 'mysql', maintenance })
const targetDriver = ref<'mysql' | 'sqlite'>('sqlite')
const targetDatasource = ref('')
const confirmed = ref(false)
const preflight = ref<null | { target: string; tables: number }>(null)
let timer = 0
const taskRunning = computed(() => migration.task?.status === 0 || migration.task?.status === 1)
const taskLabel = computed(() => taskRunning.value ? '迁移运行中' : migration.task?.status === 2 ? '迁移完成待切换' : migration.task?.status === 3 ? '迁移失败' : '未执行')
const taskTone = computed(() => taskRunning.value ? 'warning' : migration.task?.status === 2 ? 'success' : migration.task?.status === 3 ? 'danger' : 'neutral')
const targetOptions = computed(() => [{ label: migration.source_driver === 'mysql' ? 'SQLite' : 'MySQL', value: migration.source_driver === 'mysql' ? 'sqlite' : 'mysql' }])

function config(key: string) { return configs.value.find(item => item.config_key === key) }
async function load() {
  loading.value = true; error.value = ''
  try {
    const [status, values] = await Promise.all([fetchDatabaseMigrationStatus(), fetchSystemConfigs()])
    Object.assign(migration, status); Object.assign(maintenance, status.maintenance); configs.value = values
    targetDriver.value = status.source_driver === 'mysql' ? 'sqlite' : 'mysql'
    await app.loadSystemStatus(true)
  } catch (cause) { error.value = normalizeApiErrorMessage(cause, '维护状态加载失败。') }
  finally { loading.value = false }
}
async function saveMaintenance() {
  saving.value = true; error.value = ''; success.value = ''
  try {
	const expected_revisions: Record<string, number> = {}
	for (const key of ['maintenance_title', 'maintenance_message', 'maintenance_enabled']) {
	  const item = config(key); if (!item) throw new Error(`缺少系统配置 ${key}`)
	  expected_revisions[key] = item.revision
	}
	await updateMaintenanceSettings({ ...maintenance, expected_revisions })
    success.value = '维护设置已保存。'; await load()
  } catch (cause) { error.value = normalizeApiErrorMessage(cause, cause instanceof Error ? cause.message : '维护设置保存失败。') }
  finally { saving.value = false }
}
async function runPreflight() {
  preflighting.value = true; preflight.value = null; error.value = ''; confirmed.value = false
  try { preflight.value = await preflightDatabaseMigration({ target_driver: targetDriver.value, target_datasource: targetDatasource.value }) }
  catch (cause) { error.value = normalizeApiErrorMessage(cause, '目标数据库预检失败。') }
  finally { preflighting.value = false }
}
async function startMigration() {
  starting.value = true; error.value = ''
  try {
    await startDatabaseMigration({ target_driver: targetDriver.value, target_datasource: targetDatasource.value, confirm: true })
    targetDatasource.value = ''; preflight.value = null; confirmed.value = false; success.value = '迁移任务已启动，整站维护模式已开启。'; await load()
  } catch (cause) { error.value = normalizeApiErrorMessage(cause, '数据库迁移启动失败。') }
  finally { starting.value = false }
}
onMounted(() => { void load(); timer = window.setInterval(() => { if (taskRunning.value) void load() }, 5000) })
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<style scoped>
.toggle-row { display: flex; align-items: flex-start; gap: 12px; }.toggle-row span { display: grid; gap: 3px; }.toggle-row small { color: var(--muted); }
.migration-summary { display: flex; flex-wrap: wrap; align-items: baseline; gap: 10px; padding: 12px 14px; border-radius: 10px; background: var(--surface-soft); }.migration-summary span { color: var(--muted); }.migration-summary strong { font-size: 20px; }
</style>
