<template>
 <FormField label="请求客户端" hint="订阅服务会根据所选客户端返回内容。" full>
  <UiSelect v-model="selected" :options="options" aria-label="订阅请求客户端" />
 </FormField>
 <FormField v-if="custom" label="自定义 User-Agent" hint="仅在订阅服务要求时填写；返回内容需使用受支持的订阅格式。" full>
  <UiInput :model-value="modelValue" aria-label="订阅 User-Agent" maxlength="255" @update:model-value="emit('update:modelValue', $event)" />
 </FormField>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const presets = [
 { label: 'sing-box', value: 'sing-box' },
 { label: 'v2rayN（节点链接）', value: 'v2rayN' },
 { label: 'Clash / Mihomo（Clash.Meta）', value: 'Clash.Meta' },
 { label: 'Zero / ZNet Sink（ZNet-Sink/0.0.1）', value: 'ZNet-Sink/0.0.1' },
]
const options = [...presets, { label: '自定义 User-Agent', value: 'custom' }]
const custom = ref(!presets.some(item => item.value === props.modelValue))
const selected = computed({
 get: () => custom.value ? 'custom' : props.modelValue,
 set: (value: string) => {
  custom.value = value === 'custom'
  if (!custom.value) emit('update:modelValue', value)
 },
})
</script>
