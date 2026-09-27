<template>
  <RadioGroupRoot v-model="model" class="ui-radio-group" :disabled="disabled" :aria-label="label">
    <label v-for="option in options" :key="option.value" class="ui-radio-option">
      <RadioGroupItem class="ui-radio-item" :value="option.value" :disabled="option.disabled">
        <RadioGroupIndicator class="ui-radio-indicator" />
      </RadioGroupItem>
      <span>{{ option.label }}</span>
    </label>
  </RadioGroupRoot>
</template>

<script setup lang="ts">
import { RadioGroupIndicator, RadioGroupItem, RadioGroupRoot } from 'reka-ui'
withDefaults(defineProps<{
  options: Array<{ value: string; label: string; disabled?: boolean }>
  label: string
  disabled?: boolean
}>(), { disabled: false })
const model = defineModel<string>()
</script>
