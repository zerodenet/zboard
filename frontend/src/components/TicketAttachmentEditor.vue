<template>
  <div class="attachment-editor">
    <div class="attachment-actions"><UiFileUpload accept=".png,.jpg,.jpeg,.gif,.webp,.ico,.svg,.pdf,.txt,.log,.zip" choose-label="上传附件" :disabled="disabled || busy || modelValue.length >= 5" @select="selected" /><small>{{ removing ? '正在移除附件…' : busy ? `正在上传 ${progress}%` : '最多 5 个附件，单文件最大 5 MiB；支持图片、PDF、文本、日志、ZIP。' }}</small></div>
    <div class="attachment-link"><UiInput v-model.trim="url" aria-label="附件 URL" placeholder="或填写外部 HTTP/HTTPS 附件链接" :disabled="disabled || busy" /><UiButton type="button" variant="secondary" :disabled="disabled || busy || !url || modelValue.length >= 5" @click="addURL">添加链接</UiButton></div>
    <ul v-if="modelValue.length"><li v-for="(item, index) in modelValue" :key="item.file_id || item.url"><span>{{ item.name }}<small v-if="item.size"> · {{ formatBytes(item.size) }}</small></span><UiButton type="button" variant="ghost" size="sm" :disabled="disabled || busy" @click="remove(index)">移除</UiButton></li></ul>
    <p v-if="error" class="field-error" role="alert">{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { deleteFile, uploadFile, type TicketAttachment } from '../api/files'
import { isHttpUrl } from '../utils/validation'
import { formatBytes } from '../utils/format'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import UiFileUpload from './UiFileUpload.vue'
const props = withDefaults(defineProps<{ modelValue: TicketAttachment[]; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: TicketAttachment[]]; 'update:busy': [value: boolean] }>()
const busy = ref(false), progress = ref(0), error = ref(''), url = ref('')
const removing = ref(false)
const stagedIDs = new Set<string>()
let disposed = false
async function selected(files: File[]) {
  if (props.disabled || busy.value || !files[0] || props.modelValue.length >= 5) return
  error.value = ''
  if (files[0].size > 5 * 1024 * 1024) { error.value = '附件不得超过 5 MiB。'; return }
  busy.value = true; emit('update:busy', true); progress.value = 0
  try {
    const file = await uploadFile(files[0], 'ticket', percent => progress.value = percent)
    if (disposed) { await deleteFile(file.id); return }
    stagedIDs.add(file.id)
    emit('update:modelValue', [...props.modelValue, { file_id: file.id, name: file.name, size: file.size, content_type: file.content_type }])
  } catch (cause: any) { if (!disposed) error.value = cause?.response?.data?.message || '附件上传失败，请重试。' }
  finally { busy.value = false; emit('update:busy', false) }
}
function addURL() {
  if (props.disabled || busy.value || props.modelValue.length >= 5) return
  error.value = ''
  if (!isHttpUrl(url.value) || url.value.length > 2048) { error.value = '请输入完整 HTTP/HTTPS 附件链接。'; return }
  if (props.modelValue.some(item => item.url === url.value)) { error.value = '该附件链接已添加。'; return }
  emit('update:modelValue', [...props.modelValue, { name: new URL(url.value).pathname.split('/').pop() || '链接附件', url: url.value }])
  url.value = ''
}
async function remove(index: number) {
  if (props.disabled || busy.value) return
  error.value = ''
  const item = props.modelValue[index]
  busy.value = true; removing.value = true; emit('update:busy', true)
  try {
    if (item.file_id) { await deleteFile(item.file_id); stagedIDs.delete(item.file_id) }
    emit('update:modelValue', props.modelValue.filter(candidate => candidate !== item))
  } catch { error.value = '附件移除失败，请重试。' }
  finally { busy.value = false; removing.value = false; emit('update:busy', false) }
}
watch(() => props.modelValue, items => {
  const retained = new Set(items.map(item => item.file_id))
  for (const id of stagedIDs) {
    if (!retained.has(id)) void deleteFile(id).then(() => stagedIDs.delete(id)).catch(cause => {
      if (cause?.response?.status === 409) stagedIDs.delete(id)
    })
  }
})
onBeforeUnmount(() => {
  disposed = true
  // The server protects files committed into messages. Draft uploads can be
  // removed when a modal/navigation discards them, including late responses.
  for (const id of stagedIDs) void deleteFile(id).catch(() => undefined)
})
</script>
<style scoped>
.attachment-editor{display:grid;gap:10px;min-width:0}.attachment-actions{display:flex;align-items:center;flex-wrap:wrap;gap:10px}.attachment-actions small{font-size:11px;color:var(--muted)}.attachment-link{display:flex;gap:8px}.attachment-link>.ui-input{min-width:0;flex:1}.attachment-editor ul{display:grid;gap:4px;margin:0;padding:0;list-style:none}.attachment-editor li{display:flex;align-items:center;justify-content:space-between;gap:10px}.attachment-editor li span{overflow-wrap:anywhere;font-size:12px}
</style>
