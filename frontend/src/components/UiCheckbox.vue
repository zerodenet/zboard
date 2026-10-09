<template>
  <CheckboxRoot v-bind="$attrs" :model-value="indeterminate ? 'indeterminate' : model" class="ui-checkbox" :disabled="disabled" @update:model-value="model = Boolean($event)">
    <CheckboxIndicator class="ui-checkbox-indicator" aria-hidden="true"><UiIcon :name="indeterminate ? 'minus' : 'check'" /></CheckboxIndicator>
  </CheckboxRoot>
</template>

<script setup lang="ts">
import { CheckboxIndicator, CheckboxRoot } from 'reka-ui'
import UiIcon from './UiIcon.vue'

defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ indeterminate?: boolean; disabled?: boolean }>(), { indeterminate: false, disabled: false })
const model = defineModel<boolean>({ default: false })
</script>
