<template>
  <FormField
    v-slot="{ controlAttrs }"
    :label="config.name"
    :name="`${config.config_key}-control`"
    :hint="config.description"
    :error="error"
    :required="input.required"
    :full="full"
  >
    <UiTextarea
      v-if="input.control === 'textarea'"
      v-bind="controlAttrs"
      :model-value="draft as any"
      :rows="rows"
      @update:model-value="emit('update:draft', $event)"
    />
    <UiInput
      v-else
      v-bind="controlAttrs"
      :type="inputType"
      :model-value="draft as any"
      :placeholder="input.placeholder || ''"
      autocomplete="off"
      @update:model-value="emit('update:draft', $event)"
    />
  </FormField>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SystemConfig } from '../api/client'
import { resolveSystemConfigInput } from '../utils/systemConfig'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiTextarea from './UiTextarea.vue'

const props = withDefaults(defineProps<{
  config: SystemConfig
  draft?: unknown
  error?: string
  full?: boolean
  rows?: number
}>(), {
  error: '',
  full: false,
  rows: 4,
})

const emit = defineEmits<{ 'update:draft': [value: unknown] }>()
const input = computed(() => resolveSystemConfigInput(props.config))
const assetKeys = new Set(['site_logo', 'site_logo_dark', 'site_favicon'])
const inputType = computed(() => {
  if (assetKeys.has(props.config.config_key)) return 'text'
  if (input.value.control === 'email') return 'email'
  if (input.value.control === 'url') return 'url'
  return 'text'
})
</script>
