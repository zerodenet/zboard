<template>
  <FormField v-slot="{ controlAttrs }" label="销售周期" :name="`${idPrefix}-cycle`" :hint="hint" :error="unitError || valueError" required>
    <UiSelect :model-value="custom ? 'custom' : preset" v-bind="controlAttrs" :options="presets" @update:model-value="selectPreset(String($event))" />
  </FormField>
  <template v-if="custom || preset === 'custom'">
    <FormField v-slot="{ controlAttrs }" label="有效期单位" :name="`${idPrefix}-billing-unit`" :error="unitError">
      <UiSelect :model-value="billingUnit" v-bind="controlAttrs" :options="units" @update:model-value="emit('update:billingUnit', String($event))" />
    </FormField>
    <FormField v-if="billingUnit !== 'once'" v-slot="{ controlAttrs }" label="周期数量" :name="`${idPrefix}-billing-value`" :error="valueError" required>
      <UiNumberInput :model-value="billingValue" v-bind="controlAttrs" :min="1" inputmode="numeric" @update:model-value="emit('update:billingValue', $event ?? 0)" />
    </FormField>
  </template>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import FormField from './FormField.vue'
import UiSelect from './UiSelect.vue'
import UiNumberInput from './UiNumberInput.vue'
const props = defineProps<{ idPrefix: string; billingUnit: string; billingValue: number; hint?: string; unitError?: string; valueError?: string }>()
const emit = defineEmits<{ 'update:billingUnit': [value: string]; 'update:billingValue': [value: number] }>()
const custom = ref(false)
const cycles: Record<string, [string, number]> = { month: ['month', 1], quarter: ['month', 3], halfYear: ['month', 6], year: ['year', 1], once: ['once', 1] }
const presets = [{ label: '月付 · 1 个月', value: 'month' }, { label: '季付 · 3 个月', value: 'quarter' }, { label: '半年付 · 6 个月', value: 'halfYear' }, { label: '年付 · 1 年', value: 'year' }, { label: '一次性 · 流量用完为止', value: 'once' }, { label: '自定义周期', value: 'custom' }]
const units = [{ label: '天', value: 'day' }, { label: '月', value: 'month' }, { label: '年', value: 'year' }, { label: '永久（流量用完为止）', value: 'once' }]
const preset = computed(() => Object.keys(cycles).find(key => cycles[key][0] === props.billingUnit && cycles[key][1] === props.billingValue) || 'custom')
function selectPreset(value: string) {
  custom.value = value === 'custom'
  if (!cycles[value]) return
  emit('update:billingUnit', cycles[value][0])
  emit('update:billingValue', cycles[value][1])
}
</script>
