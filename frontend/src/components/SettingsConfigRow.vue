<template>
  <article class="config-row" :class="{ 'config-row-switch': input.control === 'switch' }" :aria-labelledby="labelID">
    <div class="config-copy">
      <div class="config-heading"><strong :id="labelID">{{ config.name }}</strong><StatusBadge v-if="dirty" tone="warning" icon="edit">已修改</StatusBadge>
        <details class="config-meta"><summary :aria-label="`${config.name}的设置说明`"><UiIcon name="info" /><span class="sr-only">设置说明</span></summary><div><p v-if="config.description" :id="descriptionID" class="config-description">{{ config.description }}</p><code>{{ config.config_key }}</code><span>{{ controlLabel }}</span><span>版本 {{ config.revision }}</span><TimeBadge :value="config.updated_at" mode="relative" /></div></details>
      </div>
    </div>
    <div class="config-control">
      <label v-if="input.control === 'switch'" class="config-switch">
        <UiSwitch v-bind="controlAttrs" :model-value="Boolean(draft)" @update:model-value="emit('update:draft', $event)" />
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
import UiSwitch from './UiSwitch.vue'
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
  'aria-describedby': [props.config.description ? descriptionID.value : '', props.error ? errorID.value : ''].filter(Boolean).join(' ') || undefined,
  'aria-invalid': props.error ? 'true' : undefined,
  required: input.value.required || undefined,
}))

watch(() => props.config.value, value => {
  if (props.config.config_key === 'system_timezone') setDisplayTimeZone(value)
}, { immediate: true })
</script>

<style scoped>
.config-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(180px, .7fr) auto; align-items: center; gap: 16px; padding: 14px 16px; }
.config-row + .config-row { border-top: 1px solid var(--line); }
.config-copy { min-width: 0; }
.config-copy > div { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.config-copy strong { font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
.config-copy p { margin: 4px 0 0; color: var(--muted-foreground); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.config-meta { margin-left: auto; color: var(--muted-foreground); font-size: 12px; }
.config-meta summary { width: 28px; min-height: 28px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer; list-style: none; border-radius: 4px; }
.config-meta summary::-webkit-details-marker { display: none; }
.config-meta summary:hover { color: var(--foreground); background: var(--muted-surface); }
.config-meta summary > .ui-icon { width: 14px; height: 14px; }
.config-meta[open] { flex-basis: 100%; margin-left: 0; }
.config-meta summary:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }
.config-meta .config-description { flex: 0 0 100%; margin: 0; }
.config-meta > div { display: flex; flex-wrap: wrap; gap: 6px 10px; padding-top: 6px; }
.config-meta code { overflow-wrap: anywhere; }
.config-control { min-width: 0; display: grid; gap: 5px; }
.config-switch { display: flex; align-items: center; justify-content: flex-end; }
.config-textarea { min-height: 92px; resize: vertical; }
.config-json { font-family: var(--font-mono); font-size: 12px; }
.config-error { display: flex; align-items: flex-start; gap: 4px; color: var(--destructive); font-size: 12px; line-height: 1.4; }
.config-actions { display: flex; align-items: center; justify-content: flex-end; gap: 5px; }
@media (max-width: 850px) { .config-row { grid-template-columns: minmax(0, 1fr) auto; } .config-control { grid-column: 1 / -1; grid-row: 2; } .config-switch { justify-content: flex-start; } }
</style>
