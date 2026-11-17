<template>
  <section class="subscription-preview" aria-label="订阅渲染预览" :aria-busy="loading">
    <header class="subscription-preview-heading"><h2>渲染预览</h2><UiButton variant="ghost" size="sm" :disabled="loading || Boolean(invalid)" aria-label="重新渲染预览" @click="emit('retry')"><UiIcon name="refresh" /></UiButton></header>
    <UiTabs :model-value="mode" :items="[{ label: '分组效果', value: 'groups' }, { label: 'Raw', value: 'raw' }]" label="预览视图" @update:model-value="setMode" />
    <div class="subscription-preview-status" role="status"><span>{{ loading ? '更新中…' : stale ? '上次结果' : preview ? '最新结果' : '等待渲染' }}</span><span v-if="preview">{{ preview.sample_label || '示例节点' }}</span></div>
    <PageAlert v-if="error" tone="warning" title="预览未更新">{{ error }}<UiButton v-if="!invalid" variant="ghost" size="sm" @click="emit('retry')">重试</UiButton></PageAlert>
    <div v-if="preview && mode === 'groups'" class="subscription-preview-groups">
      <template v-if="groups.length">
        <FormField v-slot="{ controlAttrs }" label="查看策略组" name="preview-policy-group"><UiSelect v-model="selectedGroup" v-bind="controlAttrs" :options="groups.map(group => ({ label: group.name, value: group.name }))" /></FormField>
        <template v-if="group">
          <div class="subscription-preview-group-summary"><span>{{ typeLabel(group.type) }}</span><span>{{ group.members.length }} 个成员</span></div>
          <ul class="subscription-preview-members"><li v-for="(member, index) in group.members.slice(0, visibleMembers)" :key="`${index}:${member}`" :class="{ selected: member === group.default }" :aria-label="member === group.default ? `${member}，默认选中` : member"><span class="member-indicator"><UiIcon v-if="member === group.default" name="check" /></span><span>{{ member }}</span></li></ul>
          <UiButton v-if="group.members.length > visibleMembers" variant="ghost" size="sm" @click="visibleMembers += 200">显示更多成员</UiButton>
          <p v-if="!group.members.length" class="muted">没有匹配成员。</p>
        </template>
      </template>
      <div v-else class="subscription-preview-empty">此格式没有可展示的策略组。<UiButton variant="ghost" size="sm" @click="setMode('raw')">查看 Raw</UiButton></div>
    </div>
    <div v-else-if="preview" class="subscription-preview-raw">
      <UiSelect v-if="preview.delivery_encoding === 'base64'" v-model="rawMode" aria-label="Raw 正文类型" :options="[{ label: '配置正文 · JSON', value: 'config' }, { label: '订阅正文 · Base64', value: 'delivery' }]" />
      <div class="subscription-preview-raw-toolbar"><span>{{ rawType }} · {{ rawBytes.toLocaleString('zh-CN') }} 字节</span><UiButton variant="ghost" size="sm" :disabled="stale || Boolean(error) || preview.truncated" @click="copy">{{ copied ? '已复制' : '复制' }}</UiButton><UiButton variant="ghost" size="sm" :disabled="stale || Boolean(error) || preview.truncated" @click="download">下载</UiButton></div>
      <div class="subscription-preview-search"><UiInput v-model="query" type="search" aria-label="查找 Raw 正文" placeholder="查找正文" @keydown.enter.prevent="findNext" /><UiButton variant="ghost" size="sm" :disabled="!query" @click="findNext">下一个</UiButton><span role="status">{{ searchMessage }}</span></div>
      <textarea ref="rawElement" class="subscription-preview-code" :value="raw" :aria-label="`原始输出 ${rawType}`" readonly spellcheck="false" wrap="off" />
      <p v-if="preview.truncated" role="alert">服务端只返回了部分正文，无法复制或下载完整结果，请更新服务端后重试。</p>
      <p v-if="actionError" role="alert">{{ actionError }}</p>
    </div>
    <div v-else class="subscription-preview-empty">{{ loading ? '正在生成预览…' : '修改配置后自动更新。' }}</div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { SubscriptionTemplatePreview } from '../api/client'
import UiTabs from './UiTabs.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiSelect from './UiSelect.vue'
import UiInput from './UiInput.vue'
import PageAlert from './PageAlert.vue'
import FormField from './FormField.vue'
import { subscriptionPreviewDelivery, subscriptionPreviewExtension } from '../utils/subscriptionPreviewRaw'
const props = defineProps<{ preview: SubscriptionTemplatePreview | null; loading: boolean; stale: boolean; error: string; invalid?: string; focusGroup?: string }>()
const emit = defineEmits<{ retry: [] }>()
function initialMode() { try { return localStorage.getItem('zboard.template-preview.view') === 'raw' ? 'raw' : 'groups' } catch { return 'groups' } }
const mode = ref(initialMode()), rawMode = ref('config'), selectedGroup = ref(''), visibleMembers = ref(200)
const rawElement = ref<HTMLTextAreaElement | null>(null), query = ref(''), searchMessage = ref(''), copied = ref(false), actionError = ref('')
let copyTimer: ReturnType<typeof setTimeout> | undefined
const groups = computed(() => props.preview?.groups || [])
const group = computed(() => groups.value.find(item => item.name === selectedGroup.value) || groups.value[0])
watch(group, () => { visibleMembers.value = 200 })
watch([() => props.preview, () => props.focusGroup], () => { copied.value = false; actionError.value = ''; searchMessage.value = ''; if (props.preview?.delivery_encoding !== 'base64') rawMode.value = 'config'; if (groups.value.some(item => item.name === props.focusGroup)) selectedGroup.value = props.focusGroup!; else if (!groups.value.some(item => item.name === selectedGroup.value)) selectedGroup.value = groups.value[0]?.name || '' }, { immediate: true })
function setMode(value: string) { mode.value = value; try { localStorage.setItem('zboard.template-preview.view', value) } catch {} }
const raw = computed(() => rawMode.value === 'delivery' ? subscriptionPreviewDelivery(props.preview?.content || '', props.preview?.delivery_encoding) : props.preview?.content || '')
const rawType = computed(() => rawMode.value === 'delivery' ? 'Base64' : props.preview?.content_type?.includes('json') ? 'JSON' : props.preview?.content_type?.includes('yaml') ? 'YAML' : '文本')
const rawBytes = computed(() => new TextEncoder().encode(raw.value).length)
watch(raw, () => { searchMessage.value = ''; copied.value = false })
function typeLabel(type: string) { return ({ select: '手动选择', selector: '手动选择', urltest: '自动测速', url_test: '自动测速', 'url-test': '自动测速', fallback: '故障转移' } as Record<string, string>)[type] || type }
function findNext() {
  if (!query.value || !rawElement.value) return
  const element = rawElement.value, value = element.value
  const start = value.indexOf(query.value, element.selectionEnd)
  const index = start >= 0 ? start : value.indexOf(query.value)
  searchMessage.value = index < 0 ? '未找到' : ''
  if (index < 0) return
  element.focus(); element.setSelectionRange(index, index + query.value.length)
  const line = value.slice(0, index).split('\n').length - 1
  element.scrollTop = Math.max(0, line * (parseFloat(getComputedStyle(element).lineHeight) || 20) - element.clientHeight / 2)
}
async function copy() { const value = raw.value; try { await navigator.clipboard.writeText(value); if (value !== raw.value) return; copied.value = true; clearTimeout(copyTimer); copyTimer = setTimeout(() => { copied.value = false }, 1600) } catch { if (value === raw.value) actionError.value = '复制失败，请在正文中选择并复制。' } }
function download() {
  const type = rawMode.value === 'delivery' ? props.preview?.delivery_content_type || 'text/plain' : props.preview?.content_type || 'text/plain'
  const url = URL.createObjectURL(new Blob([raw.value], { type: `${type};charset=utf-8` }))
  const link = document.createElement('a'); link.href = url; link.download = `subscription-preview.${subscriptionPreviewExtension(type)}`; link.click(); URL.revokeObjectURL(url)
}
onBeforeUnmount(() => clearTimeout(copyTimer))
</script>
<style scoped>
.subscription-preview { min-width: 0; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); }
.subscription-preview-heading, .subscription-preview-status, .subscription-preview-group-summary, .subscription-preview-raw-toolbar, .subscription-preview-search { display: flex; align-items: center; gap: 8px; }
.subscription-preview-heading { justify-content: space-between; margin-bottom: 8px; }.subscription-preview-heading h2 { margin: 0; font-size: 16px; }
.subscription-preview-status { min-height: 40px; color: var(--muted-foreground); font-size: 12px; flex-wrap: wrap; }.subscription-preview-status span:last-child { overflow-wrap: anywhere; }
.subscription-preview-groups, .subscription-preview-raw { display: grid; gap: 12px; min-width: 0; }.subscription-preview-group-summary { justify-content: space-between; font-size: 12px; color: var(--muted-foreground); }
.subscription-preview-members { margin: 0; padding: 0; list-style: none; max-height: 440px; overflow: auto; }.subscription-preview-members li { display: flex; align-items: center; gap: 8px; padding: 10px 8px; font-size: 13px; border-radius: var(--radius-control); }.subscription-preview-members li > span { min-width: 0; overflow-wrap: anywhere; }.subscription-preview-members li > .ui-icon { width: 14px; flex: none; color: var(--muted-foreground); }.subscription-preview-members li.selected { background: var(--primary-soft); }.subscription-preview-members small { margin-left: auto; flex: none; color: var(--primary); }
.subscription-preview-raw-toolbar { font-size: 12px; }.subscription-preview-raw-toolbar > span { margin-right: auto; color: var(--muted-foreground); }.subscription-preview-search > .ui-input { min-width: 0; flex: 1; }.subscription-preview-search > span { font-size: 12px; }
.subscription-preview-code { width: 100%; height: min(54dvh, 640px); min-height: 280px; resize: vertical; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--background); color: var(--foreground); font: 12px/20px var(--font-mono); tab-size: 2; overflow: auto; white-space: pre; }
.subscription-preview-code:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }.subscription-preview-empty { padding: 24px 0; font-size: 13px; color: var(--muted-foreground); }.subscription-preview :deep(.page-alert) { margin-bottom: 12px; }
.member-indicator { width: 16px; height: 16px; display: inline-flex; flex: none; color: var(--primary); }
@media(max-width:700px) { .subscription-preview { padding: 16px; }.subscription-preview-code { height: 55dvh; } }
</style>
