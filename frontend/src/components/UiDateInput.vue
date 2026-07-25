<template>
  <UiInput v-model="model" v-bind="$attrs" type="date" />
</template>

<script setup lang="ts">
import UiInput from './UiInput.vue'

defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })
</script>
