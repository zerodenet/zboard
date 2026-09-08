<template>
  <ModalDialog :open="open" title="离线导入插件" description="选择发布者提供的签名插件包，系统会检查签名和兼容版本。" :busy="busy" @close="$emit('close')">
    <div class="plugin-import-content">
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
      <UiFileUpload choose-label="选择插件包" accept=".zbplugin" :max-file-size="32 * 1024 * 1024" :disabled="busy" @select="select" />
      <div v-if="file" class="plugin-import-file"><strong>{{ file.name }}</strong><span>{{ (file.size / 1024 / 1024).toFixed(2) }} MiB</span></div>
      <p v-else>支持 .zbplugin 文件，最大 32 MiB。</p>
      <p>导入后保持停用。你可以先完成配置，再启用插件。</p>
    </div>
    <template #footer>
      <UiButton variant="secondary" :disabled="busy" @click="$emit('close')">取消</UiButton>
      <UiButton :disabled="!file || busy" :loading="busy" @click="submit">验证并导入</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import ModalDialog from '../components/ModalDialog.vue'
import UiFileUpload from '../components/UiFileUpload.vue'
import UiButton from '../components/UiButton.vue'
import PageAlert from '../components/PageAlert.vue'
import { importPlugin, type Plugin } from '../api/plugins'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; imported: [plugin: Plugin] }>()
const file = ref<File | null>(null), error = ref(''), busy = ref(false)
let disposed = false
onScopeDispose(() => { disposed = true })
watch(() => props.open, () => { file.value = null; error.value = '' })
function select(files: File[]) {
  const chosen = files[0]
  error.value = ''; file.value = null
  if (!chosen) return
  if (!chosen.name.toLowerCase().endsWith('.zbplugin') || chosen.size > 32 * 1024 * 1024) {
    error.value = '请选择不超过 32 MiB 的 .zbplugin 文件。'; return
  }
  file.value = chosen
}
async function submit() {
  if (!file.value || busy.value) return
  busy.value = true; error.value = ''
  try { const plugin = await importPlugin(file.value); if (!disposed) emit('imported', plugin) }
  catch (cause: any) { if (!disposed) error.value = cause?.response?.data?.message || '导入失败，请检查插件包和发布者签名后重试。' }
  finally { busy.value = false }
}
</script>
