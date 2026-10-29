<template>
  <section class="standard-page">
    <PageHeader title="系统维护" description="统一控制整站维护、数据库迁移与切换后的恢复步骤。" eyebrow="Operations" :show-section-navigation="false">
      <template #actions><PageRefreshButton :loading="loading" label="刷新维护状态" @click="reload" /></template>
    </PageHeader>
    <TransientFeedback :success="success" :error="error" />
    <PageAlert v-if="draftRecovery.message.value" tone="info" title="维护文案草稿">{{ draftRecovery.message.value }}
      <template #actions><UiButton variant="ghost" @click="retryDraftRecovery">重新检查草稿恢复</UiButton></template>
    </PageAlert>
    <PageAlert v-if="recoveryConflict" tone="warning" title="维护文案草稿基于旧版本">标题与说明已保留，但当前配置版本或原值已变化，不能直接覆盖。请重新载入服务器文案后再编辑。
      <template #actions><UiButton variant="secondary" @click="reload">重新载入服务器文案</UiButton></template>
    </PageAlert>
    <PageAlert v-if="datasourceEdited" tone="warning" title="目标连接信息仅保留在当前页面">已编辑目标连接信息时，第三方登录跳转会被阻止以保留本页。可使用页内重新认证，或明确放弃目标连接草稿。目标连接、维护开关、预检及迁移确认不会随文案草稿恢复。
      <template #actions><UiButton variant="secondary" @click="discardConnectionDraft">放弃目标连接草稿</UiButton></template>
    </PageAlert>

    <p class="muted">第三方重新登录仅恢复维护标题与说明；维护开关、目标连接、预检及迁移确认需重新核对。</p>
    <SettingsPageLayout>
      <UiSection title="整站维护开关" description="开启后普通用户、节点上报与业务接口统一返回维护状态；管理员控制台、健康检查和状态接口继续可用。">
        <template #meta><StatusBadge :tone="maintenance.enabled ? 'warning' : 'success'">{{ maintenance.enabled ? '维护中' : '正常服务' }}</StatusBadge></template>
        <div class="panel-body stack">
          <label class="toggle-row"><UiCheckbox v-model="maintenance.enabled" :disabled="maintenance.migration_in_progress" /><span><strong>开启整站维护</strong><small>数据库迁移运行时无法关闭。</small></span></label>
          <div class="form-grid">
            <FormField v-slot="{ controlAttrs }" label="维护页标题" name="maintenance-title"><UiInput v-model.trim="maintenance.title" v-bind="controlAttrs" maxlength="160" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="维护说明" name="maintenance-message"><UiTextarea v-model="maintenance.message" v-bind="controlAttrs" maxlength="4000" rows="4" /></FormField>
          </div>
          <div class="form-actions"><UiButton type="button" :loading="saving" :disabled="recoveryConflict || !recoveryReady || recovering" @click="saveMaintenance">保存维护设置</UiButton></div>
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
    </SettingsPageLayout>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchDatabaseMigrationStatus, fetchSystemConfigs, preflightDatabaseMigration, startDatabaseMigration, updateMaintenanceSettings, type DatabaseMigrationStatus, type MaintenanceState, type SystemConfig } from '../api/client'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import SettingsPageLayout from '../components/SettingsPageLayout.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import DatabaseMigrationDatasource from '../components/DatabaseMigrationDatasource.vue'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { useReauthenticationDraft } from '../composables/useReauthenticationDraft'
import { clearReauthenticationDrafts } from '../utils/reauthenticationDrafts'
import { maintenanceConfigBaseline, maintenanceRecoveryDraft, maintenanceRecoveryKeys, sameMaintenanceBaseline, type MaintenanceRecoveryBaseline } from '../utils/maintenanceReauthenticationDraft'
import { confirmAction } from '../utils/feedback'

const app = useAppStore()
const route = useRoute()
const loading = ref(false), saving = ref(false), preflighting = ref(false), starting = ref(false)
const success = ref(''), error = ref('')
const maintenance = reactive<MaintenanceState>({ enabled: false, title: '系统维护中', message: '系统正在维护，请稍后再试。', migration_in_progress: false, migration_cutover_pending: false })
const migration = reactive<DatabaseMigrationStatus>({ source_driver: 'mysql', maintenance })
const targetDriver = ref<'mysql' | 'sqlite'>('sqlite')
const targetDatasource = ref('')
const confirmed = ref(false)
const preflight = ref<null | { target: string; tables: number }>(null)
let timer = 0, resourceSequence = 0
let disposed = false
const recoveryReady = ref(false), recovering = ref(false), recoveryConflict = ref(false)
const datasourceEdited = ref(false)
const serverBaseline = ref<MaintenanceRecoveryBaseline | null>(null)
const recoveryOrigin = ref<MaintenanceRecoveryBaseline | null>(null)
const textBaseline = ref({ title: maintenance.title, message: maintenance.message })
const textDirty = computed(() => maintenance.title !== textBaseline.value.title || maintenance.message !== textBaseline.value.message)
watch(targetDatasource, () => { datasourceEdited.value = true }, { flush: 'sync' })
const draftRecovery = useReauthenticationDraft({
  scope: 'admin.maintenance.text', permission: 'admin', ready: () => recoveryReady.value,
  read: () => {
    // A marker, never a DSN, makes shared full-page capture fail and leaves the
    // mounted page intact. Execution selections are never a recovery payload.
    if (datasourceEdited.value) return { blocked: true }
    if (!textDirty.value && !recoveryOrigin.value && !recoveryConflict.value) return null
    const original = recoveryOrigin.value || (!recoveryConflict.value && serverBaseline.value)
    return original ? { title: maintenance.title, message: maintenance.message, baseline: original } : { blocked: true }
  },
  sanitize: maintenanceRecoveryDraft,
  restore: async (value, context) => {
    const sequence = ++resourceSequence
    const localTitle = maintenance.title, localMessage = maintenance.message
    recovering.value = true
    try {
      const [status, values] = await Promise.all([fetchDatabaseMigrationStatus(), fetchSystemConfigs()])
      if (!context.isCurrent() || sequence !== resourceSequence || maintenance.title !== localTitle || maintenance.message !== localMessage) return false
      applyResources(status, values, false)
      recoveryOrigin.value = value.baseline
      recoveryConflict.value = !serverBaseline.value || !sameMaintenanceBaseline(value.baseline, serverBaseline.value)
      maintenance.title = value.title; maintenance.message = value.message
    } finally { if (sequence === resourceSequence) recovering.value = false }
  },
})

function currentRequest() {
  const token = app.token, actor = app.user?.id, epoch = app.sessionEpoch, path = route.fullPath
  return () => !disposed && Boolean(token) && token === app.token && actor === app.user?.id && epoch === app.sessionEpoch && path === route.fullPath && !app.reauthRequired
}
function applyResources(status: DatabaseMigrationStatus, values: SystemConfig[], keepText: boolean) {
  const title = maintenance.title, message = maintenance.message
  const original = recoveryOrigin.value || serverBaseline.value
  Object.assign(migration, status); Object.assign(maintenance, status.maintenance)
  serverBaseline.value = maintenanceConfigBaseline(values) || null
  textBaseline.value = { title: status.maintenance.title, message: status.maintenance.message }
  targetDriver.value = status.source_driver === 'mysql' ? 'sqlite' : 'mysql'
  if (keepText) {
    maintenance.title = title; maintenance.message = message
    recoveryOrigin.value = original
    recoveryConflict.value = !original || !serverBaseline.value || !sameMaintenanceBaseline(original, serverBaseline.value)
  } else {
    recoveryOrigin.value = null; recoveryConflict.value = false
  }
}
const taskRunning = computed(() => migration.task?.status === 0 || migration.task?.status === 1)
const taskLabel = computed(() => taskRunning.value ? '迁移运行中' : migration.task?.status === 2 ? '迁移完成待切换' : migration.task?.status === 3 ? '迁移失败' : '未执行')
const taskTone = computed(() => taskRunning.value ? 'warning' : migration.task?.status === 2 ? 'success' : migration.task?.status === 3 ? 'danger' : 'neutral')
const targetOptions = computed(() => [{ label: migration.source_driver === 'mysql' ? 'SQLite' : 'MySQL', value: migration.source_driver === 'mysql' ? 'sqlite' : 'mysql' }])

async function load(discardText = false) {
  if (recovering.value) return
  const sequence = ++resourceSequence, isCurrent = currentRequest()
  recoveryReady.value = false
  loading.value = true; error.value = ''
  try {
    const [status, values] = await Promise.all([fetchDatabaseMigrationStatus(), fetchSystemConfigs()])
    if (!isCurrent() || sequence !== resourceSequence) return
    await app.loadSystemStatus(true)
    if (!isCurrent() || sequence !== resourceSequence) return
    applyResources(status, values, !discardText && (textDirty.value || Boolean(recoveryOrigin.value) || recoveryConflict.value))
    recoveryReady.value = true
  } catch (cause: any) {
    if (!isCurrent() || sequence !== resourceSequence) return
    if (cause?.response?.status === 403) clearReauthenticationDrafts()
    error.value = normalizeApiErrorMessage(cause, '维护状态加载失败。')
  } finally { if (sequence === resourceSequence) loading.value = false }
}
async function reload() {
  if ((textDirty.value || recoveryConflict.value) && !await confirmAction({ title: '重新载入维护文案？', message: '未保存的标题与说明将被服务器当前文案替换。', confirmText: '重新载入', tone: 'danger' })) return
  await load(true)
}
async function retryDraftRecovery() { await load(); await draftRecovery.restore() }
async function saveMaintenance() {
  if (saving.value || recovering.value || !recoveryReady.value || recoveryConflict.value || !app.token || app.reauthRequired) return
  const isCurrent = currentRequest(), original = recoveryOrigin.value || serverBaseline.value
  if (!original) { error.value = '维护配置依据尚未完整加载，请重新载入后再保存。'; return }
  const submitted = { ...maintenance }
  saving.value = true; error.value = ''; success.value = ''
  try {
    const [status, values] = await Promise.all([fetchDatabaseMigrationStatus(), fetchSystemConfigs()])
    if (!isCurrent()) return
    const current = maintenanceConfigBaseline(values)
    if (!current || !sameMaintenanceBaseline(original, current)) {
      recoveryOrigin.value = original; recoveryConflict.value = true
      error.value = '维护配置已变化，标题与说明已保留，请重新载入服务器文案后再编辑。'; return
    }
    if (maintenance.title !== submitted.title || maintenance.message !== submitted.message || maintenance.enabled !== submitted.enabled) {
      error.value = '核对期间内容已修改，请再次确认后保存。'; return
    }
    const expected_revisions: Record<string, number> = {}
    for (const key of maintenanceRecoveryKeys) expected_revisions[key] = original[key].revision
    await updateMaintenanceSettings({ ...submitted, migration_in_progress: status.maintenance.migration_in_progress, migration_cutover_pending: status.maintenance.migration_cutover_pending, expected_revisions })
    if (!isCurrent()) return
    success.value = '维护设置已保存。'
    // Do not replace text edited while the explicit write was in flight.
    if (maintenance.title === submitted.title && maintenance.message === submitted.message) await load(true)
  } catch (cause: any) {
    if (!isCurrent()) return
    if (cause?.response?.status === 409) { recoveryOrigin.value = original; recoveryConflict.value = true }
    if (cause?.response?.status === 403) clearReauthenticationDrafts()
    error.value = normalizeApiErrorMessage(cause, '维护设置保存失败。')
  } finally { saving.value = false }
}
async function discardConnectionDraft() {
  const actor = app.user?.id, epoch = app.sessionEpoch, path = route.fullPath
  if (!await confirmAction({ title: '放弃目标连接草稿？', message: '目标连接信息将清除，预检及迁移确认同时失效；维护文案会保留，不会启动任何操作。', confirmText: '放弃连接草稿', tone: 'danger' })) return
  if (disposed || actor !== app.user?.id || epoch !== app.sessionEpoch || path !== route.fullPath) return
  targetDatasource.value = ''; datasourceEdited.value = false; preflight.value = null; confirmed.value = false
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
    targetDatasource.value = ''; datasourceEdited.value = false; preflight.value = null; confirmed.value = false; success.value = '迁移任务已启动，整站维护模式已开启。'; await load()
  } catch (cause) { error.value = normalizeApiErrorMessage(cause, '数据库迁移启动失败。') }
  finally { starting.value = false }
}
onMounted(() => { void load(); timer = window.setInterval(() => { if (taskRunning.value) void load() }, 5000) })
onBeforeUnmount(() => { disposed = true; resourceSequence++; window.clearInterval(timer) })
</script>

<style scoped>
.toggle-row { display: flex; align-items: flex-start; gap: 12px; }.toggle-row span { display: grid; gap: 3px; }.toggle-row small { color: var(--muted); }
.migration-summary { display: flex; flex-wrap: wrap; align-items: baseline; gap: 10px; padding: 12px 14px; border-radius: 10px; background: var(--surface-soft); }.migration-summary span { color: var(--muted); }.migration-summary strong { font-size: 20px; }
</style>
