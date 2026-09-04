<template>
  <div class="raw-customizer">
    <PageAlert tone="info" title="Raw 模型与可视化双向同步">
      这里编辑完整的版本化模板模型，而不是最终含用户凭据的订阅结果。有效 JSON 会立即反向映射到可视化配置；协议节点仍由后端安全注入。
    </PageAlert>
    <div class="raw-heading">
      <div><strong>完整模板 JSON</strong><span>适合批量复制、审查和精确调整 DNS、TUN、运行模式、策略组与规则集。</span></div>
      <StatusBadge :tone="rawError ? 'danger' : 'success'" :icon="rawError ? 'warning' : 'check'">{{ rawError ? '等待修正' : '已同步' }}</StatusBadge>
    </div>
    <TemplateCodeEditor v-model="rawSource" aria-label="Raw 订阅模板 JSON" />
    <PageAlert v-if="rawError" tone="danger" title="Raw 模型无法反向渲染">{{ rawError }}</PageAlert>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { SubscriptionRenderer, SubscriptionTemplateCustomization } from '../api/client'
import { parseSubscriptionCustomizationRaw, serializeSubscriptionCustomization, type SupportedSubscriptionRenderer } from '../utils/subscriptionTemplateEditor'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'
import TemplateCodeEditor from './TemplateCodeEditor.vue'

const props = defineProps<{ renderer: SubscriptionRenderer | SupportedSubscriptionRenderer; active: boolean }>()
const emit = defineEmits<{ error: [value: string] }>()
const model = defineModel<SubscriptionTemplateCustomization>({ required: true })
const rawSource = ref('')
const rawError = ref('')
let applyingRaw = false

function loadModel() {
  rawSource.value = serializeSubscriptionCustomization(model.value)
  rawError.value = ''
  emit('error', '')
}

watch(() => props.active, active => { if (active) loadModel() }, { immediate: true })
watch(() => props.renderer, () => { if (props.active) loadModel() })
watch(rawSource, source => {
  if (!props.active) return
  try {
    const parsed = parseSubscriptionCustomizationRaw(props.renderer, source)
    rawError.value = ''
    emit('error', '')
    applyingRaw = true
    model.value = parsed
  } catch (error: any) {
    rawError.value = error?.message || 'Raw JSON 无效。'
    emit('error', rawError.value)
  }
})
watch(model, () => {
  if (applyingRaw) {
    applyingRaw = false
    return
  }
  if (props.active && !rawError.value) rawSource.value = serializeSubscriptionCustomization(model.value)
}, { deep: true })
</script>

<style scoped>
.raw-customizer{display:grid;gap:12px}.raw-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.raw-heading>div{display:grid;gap:3px}.raw-heading strong{font-size:11px;color:var(--text-strong)}.raw-heading span{font-size:9px;color:var(--muted);line-height:1.5}@media(max-width:700px){.raw-heading{align-items:flex-start;flex-direction:column}}
</style>
