<template>
  <article class="config-row" :aria-labelledby="labelID">
    <div class="config-copy">
      <div><strong :id="labelID">{{ config.name }}</strong><span class="value-type">{{ controlLabel }}</span><StatusBadge v-if="dirty" tone="warning" icon="edit">已修改</StatusBadge></div>
      <div class="config-meta"><code>{{ config.config_key }}</code><StatusBadge tone="neutral" icon="history">版本 {{ config.revision }}</StatusBadge><TimeBadge :value="config.updated_at" mode="relative" /></div>
      <p :id="descriptionID">{{ config.description || '暂无说明' }}</p>
    </div>
    <div class="config-control">
      <label v-if="input.control === 'switch'" class="config-switch">
        <span>{{ draft ? '已启用' : '已关闭' }}</span>
        <UiCheckbox v-bind="controlAttrs" role="switch" :model-value="Boolean(draft)" @update:model-value="emit('update:draft', $event)" />
      </label>
      <UiSelect v-else-if="input.control === 'select'" v-bind="controlAttrs" class="config-input" :options="input.options || []" :model-value="draft as any" :placeholder="placeholder" @update:model-value="emit('update:draft', $event)" />
      <PortInput v-else-if="input.control === 'port'" v-bind="controlAttrs" class="config-input" :model-value="Number(draft)" @update:model-value="emit('update:draft', $event)" />
      <UiNumberInput v-else-if="input.control === 'integer'" v-bind="controlAttrs" class="config-input" :model-value="Number(draft)" :min="input.min" :max="input.max" :step="input.step || 1" @update:model-value="emit('update:draft', $event)" />
      <UiTextarea v-else-if="input.control === 'textarea' || input.control === 'json'" v-bind="controlAttrs" class="config-input config-textarea" :class="{ 'config-json': input.control === 'json' }" :model-value="draft as any" :placeholder="placeholder" rows="5" @update:model-value="emit('update:draft', $event)" />
      <UiInput v-else v-bind="controlAttrs" class="config-input" :type="inputType" :model-value="draft as any" :placeholder="placeholder" :autocomplete="input.control === 'password' ? 'new-password' : 'off'" @update:model-value="emit('update:draft', $event)" />
      <span v-if="error" :id="errorID" class="config-error" role="alert"><UiIcon name="alert" />{{ error }}</span>
    </div>
    <div class="config-actions">
      <UiButton v-if="conflict" variant="ghost" size="sm" type="button" :disabled="saving" @click="emit('reload')"><UiIcon name="refresh" />重新载入</UiButton>
      <UiButton variant="secondary" size="sm" type="button" :loading="saving" :disabled="!dirty" @click="emit('save')">保存</UiButton>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import type { SystemConfig } from '../api/client'
import { resolveSystemConfigInput, systemConfigControlLabel } from '../utils/systemConfig'
import { setDisplayTimeZone } from '../utils/timeZone'
import PortInput from './PortInput.vue'
import StatusBadge from './StatusBadge.vue'
import TimeBadge from './TimeBadge.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import UiNumberInput from './UiNumberInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'

const props = defineProps<{ config: SystemConfig; draft?: unknown; saving?: boolean; dirty?: boolean; error?: string; conflict?: boolean }>()
const emit = defineEmits<{ 'update:draft': [value: unknown]; save: []; reload: [] }>()
const input = computed(() => resolveSystemConfigInput(props.config))
const controlLabel = computed(() => systemConfigControlLabel(props.config))
const labelID = computed(() => `${props.config.config_key}-label`)
const descriptionID = computed(() => `${props.config.config_key}-description`)
const errorID = computed(() => `${props.config.config_key}-error`)
const placeholder = computed(() => {
  if (props.config.is_secret && props.config.configured) return `已配置；${input.value.placeholder || '输入新值以轮换'}`
  return input.value.placeholder || ''
})
const inputType = computed(() => input.value.control === 'password' ? 'password' : input.value.control === 'email' ? 'email' : input.value.control === 'url' ? 'url' : 'text')
const controlAttrs = computed(() => ({
  id: `${props.config.config_key}-control`,
  'aria-labelledby': labelID.value,
  'aria-describedby': [descriptionID.value, props.error ? errorID.value : ''].filter(Boolean).join(' '),
  'aria-invalid': props.error ? 'true' : undefined,
  required: input.value.required || undefined,
}))

watch(() => props.config.value, value => {
  if (props.config.config_key === 'system_timezone') setDisplayTimeZone(value)
}, { immediate: true })
</script>

<style scoped>
.config-row { display: grid; grid-template-columns: minmax(240px,1fr) minmax(220px,.7fr) auto; align-items: center; gap: 16px; padding: 14px 16px; }.config-row+.config-row { border-top: 1px solid var(--line); }.config-copy>div { display: flex; align-items: center; gap: 8px; }.config-copy strong { font-size: 11px; }.value-type { padding: 2px 5px; border-radius: 4px; color: var(--primary); background: var(--primary-soft); font-size: 8px; font-weight: 700; }.config-meta { flex-wrap: wrap; margin-top: 4px; }.config-copy code { color: var(--code-text); font-size: 9px; }.config-copy p { margin: 4px 0 0; color: var(--muted); font-size: 9px; line-height: 1.45; }.config-control { min-width: 0; display: grid; gap: 5px; }.config-switch { display: flex; align-items: center; justify-content: flex-end; gap: 9px; color: var(--muted); font-size: 10px; }.config-textarea { min-height: 92px; resize: vertical; }.config-json { font-family: var(--font-mono); font-size: 10px; }.config-error { display: flex; align-items: flex-start; gap: 4px; color: var(--danger); font-size: 10px; line-height: 1.4; }.config-actions { display: flex; align-items: center; justify-content: flex-end; gap: 5px; }
@media(max-width:850px){.config-row{grid-template-columns:1fr auto}.config-control{grid-column:1 / -1}.config-copy{min-width:0}.config-switch{justify-content:flex-start}}
</style>
