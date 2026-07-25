<template>
  <PrimeTabs class="ui-tabs" :value="modelValue" scrollable @update:value="$emit('update:modelValue', String($event))">
    <PrimeTabList :aria-label="label">
      <PrimeTab v-for="item in items" :key="item.value" :value="item.value">
        <UiIcon v-if="item.icon" :name="item.icon" />
        {{ item.label }}
        <span v-if="item.count !== undefined" class="ui-tab-count">{{ item.count }}</span>
      </PrimeTab>
    </PrimeTabList>
  </PrimeTabs>
</template>

<script setup lang="ts">
import PrimeTab from 'primevue/tab'
import PrimeTabList from 'primevue/tablist'
import PrimeTabs from 'primevue/tabs'
import UiIcon from './UiIcon.vue'

defineProps<{
  modelValue: string
  items: Array<{ value: string; label: string; icon?: string; count?: number }>
  label?: string
}>()

defineEmits<{ 'update:modelValue': [value: string] }>()
</script>
