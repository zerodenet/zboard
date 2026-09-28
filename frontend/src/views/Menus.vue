<template>
  <section class="standard-page">
    <PageHeader title="菜单管理" description="管理后台、用户中心和公开站点的菜单名称、层级与显示状态。" :show-section-navigation="false">
      <template #actions><PageRefreshButton label="刷新菜单" :loading="loading" :disabled="saving" @click="reload" /></template>
    </PageHeader>
    <TransientFeedback :success="notice" />
    <SettingsPageLayout>
      <div class="menu-toolbar">
        <div class="menu-surface"><UiSelect v-model="surface" :options="surfaces" :disabled="dirty || saving" aria-label="菜单范围" /></div>
        <div class="menu-toolbar-actions">
          <UiButton variant="secondary" :disabled="saving || loading || !snapshot" @click="addGroup">新增分组</UiButton>
          <UiButton variant="secondary" :disabled="saving || loading || !snapshot" @click="addLink">新增入口</UiButton>
        </div>
        <div class="menu-save-actions">
          <UiButton variant="ghost" :disabled="!dirty || saving" @click="discard">撤销修改</UiButton>
          <UiButton :disabled="!dirty || saving" :loading="saving" @click="save">保存菜单</UiButton>
        </div>
      </div>
      <PageAlert v-if="error" tone="danger" title="菜单未更新">
        {{ error }}
        <template #actions><UiButton variant="secondary" size="sm" :disabled="saving" :loading="loading" @click="reload">重新加载</UiButton></template>
      </PageAlert>
      <div class="settings-panel">
        <UiSection title="菜单结构" description="保存后生效。隐藏页面后，直接访问会提示页面不可用。">
          <template #meta><StatusBadge v-if="dirty" tone="warning" icon="edit">有未保存修改</StatusBadge><span v-else-if="snapshot" class="menu-count">{{ ordered.length }} 项</span></template>
          <div v-if="loading" class="menu-loading" role="status" aria-label="正在加载菜单">
            <span class="sr-only">正在加载菜单…</span>
            <Skeleton v-for="index in 6" :key="index" class="menu-skeleton-line" />
          </div>
          <div v-else-if="snapshot" class="menu-editor" :inert="saving" :aria-busy="saving">
            <article v-for="{ node, depth } in ordered" :key="node.id" class="menu-row" :data-node-id="node.id" :style="{ '--menu-indent': `${Math.min(depth, 4) * 16}px` }">
              <div class="menu-row-heading">
                <div class="menu-identity"><strong>{{ node.label }}</strong><span class="menu-meta">{{ node.path ? '入口' : '分组' }} · {{ node.owner === 'core' ? '内置' : node.owner === 'plugin' ? node.plugin_id : '自定义' }}</span><StatusBadge v-if="node.hidden" tone="neutral">已隐藏</StatusBadge></div>
                <div class="menu-row-actions">
                  <UiButton variant="ghost" size="sm" :aria-expanded="editing === node.id" :aria-controls="`menu-fields-${node.id}`" @click="editing = editing === node.id ? '' : node.id">{{ editing === node.id ? '收起' : '编辑' }}</UiButton>
                  <UiButton v-if="node.owner === 'custom'" variant="ghost" size="sm" :disabled="snapshot.nodes.some(item => item.parent_id === node.id)" @click="remove(node.id)">删除</UiButton>
                </div>
              </div>
              <div v-if="editing === node.id" :id="`menu-fields-${node.id}`" class="menu-fields form-grid">
                <FormField v-slot="{ controlAttrs }" label="名称" :name="`${node.id}-label`"><UiInput v-bind="controlAttrs" v-model="node.label" :aria-label="`${node.id} 名称`" maxlength="160" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="父分组" :name="`${node.id}-parent`"><UiSelect v-bind="controlAttrs" v-model="node.parent_id" :options="parents(node)" :aria-label="`${node.id} 父分组`" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="顺序" :name="`${node.id}-position`" hint="同级菜单按数字从小到大排列。"><UiNumberInput v-bind="controlAttrs" :model-value="node.position" @update:model-value="value => { if (value !== null) node.position = value }" :aria-label="`${node.id} 顺序`" :min="-100000" :max="100000" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="图标" :name="`${node.id}-icon`"><UiInput v-bind="controlAttrs" v-model="node.icon" :aria-label="`${node.id} 图标`" maxlength="64" /></FormField>
                <FormField v-if="node.path || node.owner === 'custom'" v-slot="{ controlAttrs }" label="路径" :name="`${node.id}-path`" :hint="node.owner === 'custom' ? '留空作为分组，或填写 / 开头的站内路径。' : '内置和插件入口的路径由对应页面管理。'"><UiInput v-bind="controlAttrs" v-model="node.path" :disabled="node.owner !== 'custom'" :aria-label="`${node.id} 路径`" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="隐藏菜单" :name="`${node.id}-hidden`" hint="隐藏分组时，其下入口也会隐藏。"><UiCheckbox v-model="node.hidden" v-bind="controlAttrs" :aria-label="`${node.id} 隐藏`" /></FormField>
              </div>
            </article>
            <EmptyState v-if="!ordered.length" icon="menu" title="尚无菜单" description="可以新增分组或入口，再保存菜单。" />
          </div>
        </UiSection>
      </div>
    </SettingsPageLayout>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { fetchMenus, saveMenus, type MenuNode, type MenuSnapshot } from '../api/menus'
import type { Surface } from '../api/plugins'
import { NAVIGATION_CHANGED } from '../utils/navigationEvents'
import { useUnsavedChangesGuard } from '../composables/useFormState'
import { confirmAction } from '../utils/feedback'
import PageHeader from '../components/PageHeader.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import SettingsPageLayout from '../components/SettingsPageLayout.vue'
import FormField from '../components/FormField.vue'
import UiSection from '../components/UiSection.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import PageAlert from '../components/PageAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import EmptyState from '../components/EmptyState.vue'
import Skeleton from '../components/ui/skeleton/Skeleton.vue'

const surfaces = [{ value: 'admin', label: '管理后台' }, { value: 'account', label: '用户中心' }, { value: 'public', label: '公开站点' }]
const surface = ref<Surface>('admin')
const snapshot = ref<MenuSnapshot | null>(null)
const baseline = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const editing = ref('')
const dirty = computed(() => !!snapshot.value && JSON.stringify(snapshot.value) !== baseline.value)
const ordered = computed(() => {
  const children = new Map<string, MenuNode[]>()
  for (const node of [...(snapshot.value?.nodes || [])].sort((a, b) => a.position - b.position || a.id.localeCompare(b.id))) children.set(node.parent_id, [...(children.get(node.parent_id) || []), node])
  const result: { node: MenuNode; depth: number }[] = []
  const seen = new Set<string>()
  function walk(parent: string, depth: number) {
    if (depth > 8) return
    for (const node of children.get(parent) || []) { if (seen.has(node.id)) continue; seen.add(node.id); result.push({ node, depth }); walk(node.id, depth + 1) }
  }
  walk('', 0)
  // Invalid draft parents stay editable until the server validates the tree.
  for (const node of snapshot.value?.nodes || []) if (!seen.has(node.id)) result.push({ node, depth: 0 })
  return result
})
useUnsavedChangesGuard(() => dirty.value, () => confirmAction({ title: '放弃菜单修改？', message: '当前菜单包含未保存的修改。', confirmText: '放弃修改', tone: 'danger' }))
let request = 0
let controller: AbortController | undefined
function accept(value: MenuSnapshot) { snapshot.value = value; baseline.value = JSON.stringify(value) }
async function load() {
  const sequence = ++request
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''; notice.value = ''
  try { const value = await fetchMenus(surface.value, controller.signal); if (sequence === request) accept(value) }
  catch (cause: any) { if (sequence === request && !controller.signal.aborted) error.value = cause?.response?.data?.message || '菜单加载失败' }
  finally { if (sequence === request) loading.value = false }
}
async function reload() {
  if (dirty.value && !await confirmAction({ title: '重新加载菜单？', message: '重新加载会丢弃当前未保存的修改。', confirmText: '重新加载', tone: 'danger' })) return
  await load()
}
async function save() {
  if (!snapshot.value || saving.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    accept(await saveMenus(surface.value, snapshot.value))
    notice.value = '菜单已保存'
    window.dispatchEvent(new Event(NAVIGATION_CHANGED))
  } catch (cause: any) { error.value = cause?.response?.status === 409 ? '菜单已被其他操作修改。请重新加载后调整。' : cause?.response?.data?.message || '保存失败，请检查分组和路径' }
  finally { saving.value = false }
}
function parents(node: MenuNode) {
  return [{ value: '', label: '顶层' }, ...(snapshot.value?.nodes || []).filter(item => !item.path && item.id !== node.id).map(item => ({ value: item.id, label: `${item.label} (${item.id})` }))]
}
function add(path: string) {
  const id = `custom:${crypto.randomUUID()}`
  editing.value = id
  snapshot.value?.nodes.push({ id, parent_id: '', surface: surface.value, label: path ? '新入口' : '新分组', icon: 'plans', path, position: 1000, hidden: false, owner: 'custom', plugin_id: '', page_id: '', condition: '' })
  void nextTick(() => { const field = document.querySelector<HTMLInputElement>(`[data-node-id="${id}"] input`); field?.scrollIntoView({ block: 'center' }); field?.focus({ preventScroll: true }) })
}
function addGroup() { add('') }
function addLink() { add(surface.value === 'public' ? '/' : `/${surface.value}`) }
function remove(id: string) { if (snapshot.value) snapshot.value.nodes = snapshot.value.nodes.filter(node => node.id !== id) }
function discard() { if (baseline.value) snapshot.value = JSON.parse(baseline.value) }
watch(surface, () => { snapshot.value = null; void load() })
onMounted(load)
onBeforeUnmount(() => { ++request; controller?.abort() })
</script>
<style scoped>
.menu-toolbar, .menu-toolbar-actions, .menu-save-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.menu-surface { flex: 0 1 180px; min-width: 160px; }
.menu-surface :deep(.ui-select) { width: 100%; }
.menu-save-actions { margin-left: auto; }
.menu-count, .menu-meta { color: var(--muted-foreground); font-size: 12px; }
.menu-count { white-space: nowrap; }
.menu-row + .menu-row { border-top: 1px solid var(--border); }
.menu-row-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 44px; padding: 6px 0 6px var(--menu-indent); }
.menu-identity { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-width: 0; }
.menu-identity strong { font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
.menu-meta { overflow-wrap: anywhere; }
.menu-row-actions { display: flex; flex: none; gap: 4px; }
.menu-fields { margin: 4px 0 16px; padding-left: var(--menu-indent); }
.menu-loading { display: grid; gap: 16px; padding-block: 12px; }
.menu-skeleton-line { height: 28px; }
@media (max-width: 760px) {
  .menu-surface { flex-basis: 100%; }
  .menu-save-actions { margin-left: 0; }
  .menu-fields { grid-template-columns: minmax(0, 1fr); padding-left: 0; }
  .menu-row-heading { padding-left: min(var(--menu-indent), 24px); }
  .menu-identity { gap: 4px 8px; }
  .menu-meta { flex-basis: 100%; }
}
</style>
