<template>
  <PrimeFileUpload :key="generation" :disabled="props.disabled" mode="basic" custom-upload auto v-bind="$attrs" @uploader="selected" />
</template>
<script setup lang="ts">
import { ref } from 'vue'
import PrimeFileUpload, { type FileUploadUploaderEvent } from 'primevue/fileupload'
defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ disabled?: boolean }>(), { disabled: false })
const generation = ref(0)
const emit = defineEmits<{ select: [files: File[]] }>()
function selected(event: FileUploadUploaderEvent) {
  emit('select', Array.isArray(event.files) ? event.files : [event.files])
  generation.value++
}
</script>
