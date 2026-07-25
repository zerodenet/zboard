<template>
  <PrimeInputText v-if="hasModelBinding" v-model="model" v-bind="attrs" fluid />
  <PrimeInputText v-else v-bind="attrs" fluid />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, useAttrs } from 'vue'
import PrimeInputText from 'primevue/inputtext'

defineOptions({ inheritAttrs: false })
const instance = getCurrentInstance()
const attrs = useAttrs()
const [model, modifiers] = defineModel<any>({
  set(value) {
    if (modifiers.trim && typeof value === 'string') value = value.trim()
    if (modifiers.number && typeof value === 'string' && value !== '') {
      const number = Number(value)
      if (!Number.isNaN(number)) value = number
    }
    return value
  },
})
const hasModelBinding = computed(() => {
  const props = instance?.vnode.props || {}
  return Object.prototype.hasOwnProperty.call(props, 'modelValue') ||
    Object.prototype.hasOwnProperty.call(props, 'model-value')
})
</script>
