<template>
  <textarea v-bind="attrs" class="ui-textarea" :value="displayValue" @input="onInput" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, useAttrs } from 'vue'

defineOptions({ inheritAttrs: false })
const instance = getCurrentInstance()
const attrs = useAttrs()
const [model, modifiers] = defineModel<any>({
  set(value) { return modifiers.trim && typeof value === 'string' ? value.trim() : value },
})
const hasModelBinding = computed(() => {
  const props = instance?.vnode.props || {}
  return 'modelValue' in props || 'model-value' in props
})
const displayValue = computed(() => hasModelBinding.value ? model.value ?? '' : attrs.value ?? '')
function onInput(event: Event) {
  if (hasModelBinding.value) model.value = (event.target as HTMLTextAreaElement).value
}
</script>
