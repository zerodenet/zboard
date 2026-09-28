<template>
  <section class="standard-page">
    <PageHeader title="菜单管理" description="统一管理站点、用户中心和管理后台的菜单。隐藏内置或插件页面后，直接访问也会提示页面不可用。" />
    <div class="menu-toolbar">
      <UiSelect v-model="surface" :options="surfaces" :disabled="dirty || saving" aria-label="菜单范围" />
      <UiButton variant="secondary" :disabled="saving || !snapshot" @click="addGroup">新增分组</UiButton>
      <UiButton variant="secondary" :disabled="saving || !snapshot" @click="addLink">新增入口</UiButton>
      <UiButton variant="ghost" :disabled="!dirty || saving" @click="discard">撤销修改</UiButton>
      <UiButton :disabled="!dirty || saving" :loading="saving" @click="save">保存菜单</UiButton>
    </div>
    <p v-if="error" role="alert" class="menu-feedback">{{ error }} <UiButton variant="ghost" :disabled="saving" @click="reload">重新加载</UiButton></p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在加载菜单…</p>
    <div v-else-if="snapshot" class="menu-editor" :inert="saving">
      <article v-for="{ node, depth } in ordered" :key="node.id" class="menu-row" :data-node-id="node.id" :style="{ marginLeft: `${Math.min(depth, 4) * 12}px` }">
        <header><strong>{{ node.label }}</strong><span>{{ node.owner === 'core' ? '内置' : node.owner === 'plugin' ? node.plugin_id : '自定义' }} · {{ node.path ? '入口' : '分组' }}{{ node.hidden ? ' · 已隐藏' : '' }}</span>
          <UiButton variant="ghost" @click="editing = editing === node.id ? '' : node.id">{{ editing === node.id ? '收起' : '编辑' }}</UiButton>
          <UiButton v-if="node.owner === 'custom'" variant="ghost" :disabled="snapshot.nodes.some(item => item.parent_id === node.id)" @click="remove(node.id)">删除</UiButton>
        </header>
        <div v-if="editing === node.id" class="menu-fields">
          <label>名称<UiInput v-model="node.label" :aria-label="`${node.id} 名称`" maxlength="160" /></label>
          <label>父分组<UiSelect v-model="node.parent_id" :options="parents(node)" :aria-label="`${node.id} 父分组`" /></label>
          <label>顺序<UiNumberInput :model-value="node.position" @update:model-value="value => { if (value !== null) node.position = value }" :aria-label="`${node.id} 顺序`" :min="-100000" :max="100000" /></label>
          <label>图标<UiInput v-model="node.icon" :aria-label="`${node.id} 图标`" maxlength="64" /></label>
          <label v-if="node.path || node.owner === 'custom'">路径<UiInput v-model="node.path" :disabled="node.owner !== 'custom'" :aria-label="`${node.id} 路径`" placeholder="留空作为分组，或填写 / 开头的站内路径" /></label>
          <label class="menu-visibility"><UiCheckbox v-model="node.hidden" :aria-label="`${node.id} 隐藏`" />隐藏</label>
        </div>
      </article>
      <p v-if="!ordered.length">尚无菜单，可以添加分组或入口。</p>
    </div>
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
.menu-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 18px; }
.menu-toolbar > .ui-select { width: 180px; min-width: 160px; }
.menu-editor { display: grid; gap: 6px; }
.menu-row { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; background: var(--card); }
.menu-row header { display: flex; align-items: center; gap: 12px; margin-bottom: 0; }
.menu-row header span { color: var(--muted-foreground); font-size: 12px; overflow-wrap: anywhere; }
.menu-row header .ui-button { margin-left: auto; }
.menu-fields { margin-top: 12px; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.menu-fields label { display: grid; gap: 5px; font-size: 12px; color: var(--muted-foreground); }
.menu-fields .menu-visibility { display: flex; align-items: center; gap: 8px; }
.menu-feedback { color: var(--destructive); }
@media (max-width: 760px) { .menu-toolbar > .ui-select { width: 100%; } .menu-fields { grid-template-columns: minmax(0, 1fr); } .menu-row header { flex-wrap: wrap; } }
</style>
