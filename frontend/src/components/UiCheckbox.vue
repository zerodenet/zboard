<template>
  <PrimeCheckbox
    v-model="model"
    v-bind="attrs"
    binary
    :indeterminate="indeterminate"
    :disabled="disabled"
  />
</template>

<script setup lang="ts">
import { useAttrs } from 'vue'
import PrimeCheckbox from 'primevue/checkbox'

defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ indeterminate?: boolean; disabled?: boolean }>(), { indeterminate: false, disabled: false })
const attrs = useAttrs()
const model = defineModel<boolean>({ default: false })
</script>
