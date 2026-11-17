<template>
  <div class="form-field" :class="{ 'form-field-full': full, 'form-field-invalid': Boolean(error), 'form-field-quiet': !hint && !error }">
    <label class="form-field-label" :for="controlId">
      <span>{{ label }}</span>
      <span v-if="required" class="form-field-required" aria-hidden="true">*</span>
      <span v-if="required" class="sr-only">（必填）</span>
    </label>
    <div class="form-field-control">
      <slot :control-attrs="controlAttrs" :control-id="controlId" />
    </div>
    <div class="form-field-message" :class="{ 'sr-only': !hint && !error }" :id="messageId" :aria-live="error && !$slots.error ? 'polite' : undefined">
      <span v-if="error" class="form-field-error"><UiIcon name="alert" /><slot name="error">{{ error }}</slot></span>
      <span v-else-if="hint" class="form-field-hint">{{ hint }}</span>
      <span v-else aria-hidden="true">&nbsp;</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from './UiIcon.vue'

let nextFormFieldId = 0

const props = withDefaults(defineProps<{
  label: string
  name?: string
  hint?: string
  error?: string
  required?: boolean
  full?: boolean
}>(), {
  name: '',
  hint: '',
  error: '',
  required: false,
  full: false,
})

const generatedId = `form-field-${++nextFormFieldId}`
const controlId = computed(() => props.name || generatedId)
const messageId = computed(() => `${controlId.value}-message`)
const controlAttrs = computed(() => ({
  id: controlId.value,
  required: props.required || undefined,
  'aria-describedby': messageId.value,
  'aria-invalid': props.error ? 'true' : undefined,
}))
</script>

<style scoped>
.form-field-quiet { grid-template-rows: auto auto; }
</style>
