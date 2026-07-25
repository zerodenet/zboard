<template>
  <div class="subscription-rule-set-lookup">
    <UiAutocomplete
      v-model="selection"
      v-bind="attrs"
      :suggestions="suggestions"
      option-label="name"
      :disabled="disabled"
      :loading="loading"
      placeholder="输入名称、标识或地址搜索规则集"
      force-selection
      dropdown
      fluid
      @complete="search"
      @item-select="selectItem"
      @clear="clear"
    >
      <template #option="{ option }">
        <div class="rule-set-option">
          <strong>{{ option.name }}</strong>
          <small>{{ option.tag }} · {{ option.format }} · 已用于 {{ option.usage_count }} 个模板</small>
        </div>
      </template>
    </UiAutocomplete>
    <small v-if="loadError" class="lookup-error" role="alert">{{ loadError }}</small>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, useAttrs } from 'vue'
import {
  fetchSubscriptionRuleSetsPage,
  type SubscriptionRenderer,
  type SubscriptionRuleSet,
} from '../api/client'
import UiAutocomplete from './UiAutocomplete.vue'

defineOptions({ inheritAttrs: false })
const attrs = useAttrs()
const props = withDefaults(defineProps<{
  renderer: Exclude<SubscriptionRenderer, 'unsupported'>
  excludeIds?: number[]
  disabled?: boolean
}>(), { excludeIds: () => [], disabled: false })
const emit = defineEmits<{ select: [value: SubscriptionRuleSet] }>()
const selection = ref<SubscriptionRuleSet | null>(null)
const suggestions = ref<SubscriptionRuleSet[]>([])
const loading = ref(false)
const loadError = ref('')
let requestSequence = 0
let controller: AbortController | null = null

async function load(query = '') {
  controller?.abort()
  controller = new AbortController()
  const currentController = controller
  const sequence = ++requestSequence
  loading.value = true
  loadError.value = ''
  try {
    const page = await fetchSubscriptionRuleSetsPage({
      q: query.trim() || undefined,
      renderer: props.renderer,
      active: true,
      limit: 25,
    }, { signal: currentController.signal })
    if (sequence !== requestSequence) return
    const excluded = new Set(props.excludeIds)
    suggestions.value = page.items.filter(item => !excluded.has(item.id))
  } catch {
    if (sequence === requestSequence && !currentController.signal.aborted) {
      suggestions.value = []
      loadError.value = '规则集检索失败，请稍后重试。'
    }
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function search(event: { query: string }) {
  void load(event.query)
}

function selectItem(event: { value: SubscriptionRuleSet }) {
  emit('select', event.value)
  selection.value = null
}

function clear() {
  selection.value = null
}

onBeforeUnmount(() => controller?.abort())
</script>

<style scoped>
.subscription-rule-set-lookup{display:grid;gap:4px}
.rule-set-option{display:grid;gap:2px}
.rule-set-option strong{color:var(--text-strong);font-size:11px}
.rule-set-option small,.lookup-error{color:var(--muted);font-size:9px}
.lookup-error{color:var(--danger)}
</style>
