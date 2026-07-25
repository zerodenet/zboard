<template>
  <UiNumberInput v-model="model" :min="1" :max="65535" :step="1" inputmode="numeric" />
</template>

<script setup lang="ts">
import UiNumberInput from './UiNumberInput.vue'

const model = defineModel<number>({ default: 0 })
</script>
