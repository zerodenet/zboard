<template>
  <div class="legal-items-editor">
    <div v-if="items.length" class="legal-items-list">
      <article v-for="(item, index) in items" :key="index" class="legal-item-card">
        <div class="legal-item-card__header">
          <div><strong>公开条目 {{ index + 1 }}</strong><small>例如 Company No.、VAT、当地备案号或监管注册信息。</small></div>
          <UiButton variant="ghost" size="sm" type="button" :disabled="saving" @click="removeItem(index)"><UiIcon name="close" />移除</UiButton>
        </div>
        <div class="form-grid legal-item-fields">
          <FormField label="标签" :name="`legal-item-${index}-label`" hint="访客看到的类型名称，最多 120 个 UTF-8 字节。" required>
            <template #default="{ controlAttrs }"><UiInput v-bind="controlAttrs" :model-value="item.label" placeholder="Company No." @update:model-value="updateItem(index, 'label', String($event ?? ''))" /></template>
          </FormField>
          <FormField label="值" :name="`legal-item-${index}-value`" hint="注册号、许可号或其他公开值，最多 512 个 UTF-8 字节。" required>
            <template #default="{ controlAttrs }"><UiInput v-bind="controlAttrs" :model-value="item.value" placeholder="12345678" @update:model-value="updateItem(index, 'value', String($event ?? ''))" /></template>
          </FormField>
          <FormField class="legal-item-url" label="查询或监管链接（可选）" :name="`legal-item-${index}-url`" hint="填写后 Footer 中该条目可以直接打开对应查询页面。" full>
            <template #default="{ controlAttrs }"><UiInput v-bind="controlAttrs" type="url" :model-value="item.url || ''" placeholder="https://registry.example/…" @update:model-value="updateItem(index, 'url', String($event ?? ''))" /></template>
          </FormField>
        </div>
      </article>
    </div>

    <div v-else class="legal-items-empty">
      <UiIcon name="audit" />
      <div><strong>没有额外注册信息</strong><p>这部分完全可选。没有当地注册号、税务编号或备案信息时无需添加。</p></div>
    </div>

    <div class="legal-items-actions">
      <UiButton variant="secondary" size="sm" type="button" :disabled="saving || items.length >= maxItems" @click="addItem"><UiIcon name="plus" />添加公开条目</UiButton>
      <small>{{ items.length }} / {{ maxItems }} 项</small>
      <span class="legal-items-spacer"></span>
      <UiButton v-if="conflict" variant="ghost" size="sm" type="button" :disabled="saving" @click="emit('reload')"><UiIcon name="refresh" />重新载入</UiButton>
      <UiButton type="button" :loading="saving" :disabled="!dirty" @click="emit('save')">保存法律信息</UiButton>
    </div>

    <p v-if="error" class="legal-items-error" role="alert"><UiIcon name="alert" />{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { SiteLegalItem } from '../utils/siteProfile'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'

const props = withDefaults(defineProps<{
  modelValue?: unknown
  dirty?: boolean
  saving?: boolean
  error?: string
  conflict?: boolean
}>(), {
  dirty: false,
  saving: false,
  error: '',
  conflict: false,
})

const emit = defineEmits<{ 'update:modelValue': [value: string]; save: []; reload: [] }>()
const maxItems = 32
const items = ref<SiteLegalItem[]>([])

function parseItems(value: unknown): SiteLegalItem[] {
  let source = value
  if (typeof source === 'string') {
    try { source = JSON.parse(source) } catch { return [] }
  }
  if (!Array.isArray(source)) return []
  return source.flatMap(item => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return []
    const row = item as Record<string, unknown>
    return [{
      label: typeof row.label === 'string' ? row.label : '',
      value: typeof row.value === 'string' ? row.value : '',
      ...(typeof row.url === 'string' && row.url ? { url: row.url } : {}),
    }]
  })
}

watch(() => props.modelValue, value => {
  const next = parseItems(value)
  if (JSON.stringify(next) !== JSON.stringify(items.value)) items.value = next
}, { immediate: true })

function publish() {
  emit('update:modelValue', JSON.stringify(items.value, null, 2))
}

function addItem() {
  if (items.value.length >= maxItems) return
  items.value.push({ label: '', value: '' })
  publish()
}

function removeItem(index: number) {
  items.value.splice(index, 1)
  publish()
}

function updateItem(index: number, key: 'label' | 'value' | 'url', value: string) {
  const current = items.value[index]
  if (!current) return
  if (key === 'url') {
    if (value) current.url = value
    else delete current.url
  } else current[key] = value
  publish()
}
</script>

<style scoped>
.legal-items-editor { padding: 16px; }
.legal-items-list { display: grid; gap: 12px; }
.legal-item-card { border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); overflow: hidden; }
.legal-item-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 12px 14px; border-bottom: 1px solid var(--line); background: var(--surface); }
.legal-item-card__header > div { display: grid; gap: 3px; }
.legal-item-card__header strong { font-size: 12px; }
.legal-item-card__header small { color: var(--muted); font-size: 9px; line-height: 1.45; }
.legal-item-fields { padding: 14px; }
.legal-item-url { grid-column: 1 / -1; }
.legal-items-empty { display: flex; align-items: flex-start; gap: 11px; padding: 18px; border: 1px dashed var(--line); border-radius: 10px; color: var(--muted); background: var(--surface-soft); }
.legal-items-empty > :first-child { margin-top: 2px; color: var(--primary); font-size: 19px; }
.legal-items-empty strong { color: var(--text); font-size: 12px; }
.legal-items-empty p { margin: 4px 0 0; font-size: 10px; line-height: 1.5; }
.legal-items-actions { display: flex; align-items: center; gap: 8px; margin-top: 14px; }.legal-items-actions small { color: var(--muted); font-size: 9px; }
.legal-items-spacer { flex: 1; }
.legal-items-error { display: flex; align-items: flex-start; gap: 5px; margin: 12px 0 0; color: var(--danger); font-size: 10px; }
@media (max-width: 700px) { .legal-item-card__header { flex-direction: column; }.legal-items-actions { flex-wrap: wrap; }.legal-items-spacer { display: none; width: 100%; } }
</style>
