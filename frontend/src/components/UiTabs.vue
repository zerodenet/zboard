<template>
  <TabsRoot class="ui-tabs" :model-value="modelValue" @update:model-value="$emit('update:modelValue', String($event))">
    <TabsList class="ui-tabs-list" :aria-label="label">
      <TabsTrigger v-for="item in items" :key="item.value" class="ui-tabs-trigger" :value="item.value">
        <UiIcon v-if="item.icon" :name="item.icon" />
        {{ item.label }}
        <span v-if="item.count !== undefined" class="ui-tab-count">{{ item.count }}</span>
      </TabsTrigger>
    </TabsList>
  </TabsRoot>
</template>

<script setup lang="ts">
import { TabsList, TabsRoot, TabsTrigger } from 'reka-ui'
import UiIcon from './UiIcon.vue'
defineProps<{
  modelValue: string
  items: Array<{ value: string; label: string; icon?: string; count?: number }>
  label?: string
}>()
defineEmits<{ 'update:modelValue': [value: string] }>()
</script>
