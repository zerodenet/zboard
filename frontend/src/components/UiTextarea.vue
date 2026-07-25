<template>
  <PrimeTextarea v-if="hasModelBinding" v-model="model" v-bind="$attrs" fluid />
  <PrimeTextarea v-else v-bind="$attrs" fluid />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from 'vue'
import PrimeTextarea from 'primevue/textarea'

defineOptions({ inheritAttrs: false })
const instance = getCurrentInstance()
const [model, modifiers] = defineModel<any>({
  set(value) {
    return modifiers.trim && typeof value === 'string' ? value.trim() : value
  },
})
const hasModelBinding = computed(() => {
  const props = instance?.vnode.props || {}
  return Object.prototype.hasOwnProperty.call(props, 'modelValue') ||
    Object.prototype.hasOwnProperty.call(props, 'model-value')
})
</script>
