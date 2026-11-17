<template>
  <div class="site-image-input">
    <UiInput v-bind="$attrs" :model-value="modelValue" type="text" placeholder="填写 HTTP/HTTPS URL，或上传本地图片" :disabled="busy" @update:model-value="emit('update:modelValue', String($event))" />
    <div class="site-image-actions"><UiFileUpload accept=".png,.jpg,.jpeg,.gif,.webp,.ico,.svg" choose-label="上传图片" :disabled="busy" @select="selected" /><span v-if="busy" role="status">正在上传 {{ progress }}%</span><small v-else>PNG、JPEG、GIF、WebP、ICO、SVG，最大 2 MiB。上传后需保存设置。</small></div>
    <p v-if="error" class="field-error" role="alert">{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { uploadFile } from '../api/files'
import UiInput from './UiInput.vue'
import UiFileUpload from './UiFileUpload.vue'
defineOptions({ inheritAttrs: false })
defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const busy = ref(false), progress = ref(0), error = ref('')
async function selected(files: File[]) {
  if (busy.value || !files[0]) return
  error.value = ''
  if (files[0].size > 2 * 1024 * 1024) { error.value = '图片不得超过 2 MiB。'; return }
  busy.value = true; progress.value = 0
  try { const file = await uploadFile(files[0], 'site', percent => progress.value = percent); emit('update:modelValue', file.url) }
  catch (cause: any) { error.value = cause?.response?.data?.message || '图片上传失败，请重试。' }
  finally { busy.value = false }
}
</script>
<style scoped>
.site-image-input{display:grid;gap:8px;min-width:0}.site-image-actions{display:flex;align-items:center;flex-wrap:wrap;gap:10px}.site-image-actions small,.site-image-actions span{color:var(--muted);font-size:11px}
</style>
