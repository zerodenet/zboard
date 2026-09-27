<template>
  <label class="ui-file-upload" :class="{ 'ui-file-upload-disabled': disabled }">
    <span>{{ chooseLabel }}</span>
    <input :key="generation" v-bind="inputAttrs" type="file" :disabled="disabled" @change="selected" />
  </label>
</template>

<script setup lang="ts">
import { computed, ref, useAttrs } from 'vue'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ disabled?: boolean; chooseLabel?: string; maxFileSize?: number }>(), {
  disabled: false, chooseLabel: '选择文件', maxFileSize: undefined,
})
const attrs = useAttrs()
const inputAttrs = computed(() => {
  const { chooseLabel: _chooseLabel, maxFileSize: _maxFileSize, ...rest } = attrs
  return rest
})
const generation = ref(0)
const emit = defineEmits<{ select: [files: File[]] }>()
function selected(event: Event) {
  const files = Array.from((event.target as HTMLInputElement).files || [])
  const maxFileSize = props.maxFileSize
  if (files.length && (!maxFileSize || files.every(file => file.size <= maxFileSize))) emit('select', files)
  generation.value++
}
</script>
