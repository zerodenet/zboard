<template>
  <CheckboxRoot v-bind="$attrs" :model-value="indeterminate ? 'indeterminate' : model" class="ui-checkbox" :disabled="disabled" @update:model-value="model = Boolean($event)">
    <span class="ui-checkbox-indicator" aria-hidden="true">{{ indeterminate ? '−' : '✓' }}</span>
  </CheckboxRoot>
</template>

<script setup lang="ts">
import { CheckboxRoot } from 'reka-ui'

defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ indeterminate?: boolean; disabled?: boolean }>(), { indeterminate: false, disabled: false })
const model = defineModel<boolean>({ default: false })
</script>
