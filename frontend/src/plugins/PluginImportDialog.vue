<template>
  <ModalDialog :open="open" title="离线导入插件" description="先查看插件来源和所需能力，再确认导入。" :busy="busy" @close="$emit('close')">
    <div class="plugin-import-content">
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
      <UiFileUpload choose-label="选择插件包" accept=".zbplugin" :max-file-size="32 * 1024 * 1024" :disabled="busy" @select="select" />
      <div v-if="file" class="plugin-import-file"><strong>{{ file.name }}</strong><span>{{ (file.size / 1024 / 1024).toFixed(2) }} MiB</span></div>
      <p v-else>支持 .zbplugin 文件，最大 32 MiB。</p>
      <details v-if="!preview"><summary>旧版插件包兼容选项</summary><label>发布者公钥（.pub 文件内容）<input v-model="publicKey" :disabled="busy" autocomplete="off" placeholder="仅旧包未附带公钥时需要" /></label></details>
      <div v-if="preview" class="plugin-import-preview">
        <strong>{{ preview.manifest.name }} · v{{ preview.manifest.version }}</strong>
        <p>插件：{{ preview.manifest.id }} · 发布者：{{ preview.publisher }}</p>
        <p>{{ preview.manifest.description }}</p>
        <p>使用范围：{{ preview.manifest.surfaces.map(surfaceLabel).join('、') || '后台服务' }}</p>
        <p>所需能力：{{ preview.manifest.capabilities.map(capabilityLabel).join('、') }}</p>
        <p class="plugin-key-fingerprint">签名指纹：{{ preview.fingerprint }}</p>
        <PageAlert v-if="!preview.compatibility.compatible" tone="danger">{{ preview.compatibility.reason }}</PageAlert>
        <PageAlert v-else-if="!preview.trusted" tone="warning">首次安装此来源。签名有效，但发布者身份尚未经本站确认。确认来源后，仅为此插件保存信任。</PageAlert>
        <p v-else>签名来源已受信任。</p>
        <label v-if="!preview.trusted"><input v-model="trust" type="checkbox" :disabled="busy" /> 我已确认来源，信任此插件的签名密钥</label>
      </div>
      <p>新安装保持停用，配置后即可启用。升级自动迁移数据并保留原启停状态，失败时保留原版本和数据。</p>
    </div>
    <template #footer>
      <UiButton variant="secondary" :disabled="busy" @click="$emit('close')">取消</UiButton>
      <UiButton :disabled="!file || busy || (!!preview && (!preview.compatibility.compatible || (!preview.trusted && !trust)))" :loading="busy" @click="submit">{{ preview ? '确认导入' : '查看插件' }}</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import ModalDialog from '../components/ModalDialog.vue'
import UiFileUpload from '../components/UiFileUpload.vue'
import UiButton from '../components/UiButton.vue'
import PageAlert from '../components/PageAlert.vue'
import { importPlugin, previewPlugin, capabilityLabel, surfaceLabel, type ImportPreview, type Plugin } from '../api/plugins'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; imported: [plugin: Plugin] }>()
const file = ref<File | null>(null), error = ref(''), busy = ref(false)
const preview = ref<ImportPreview | null>(null), publicKey = ref(''), trust = ref(false)
let disposed = false
onScopeDispose(() => { disposed = true })
watch(() => props.open, () => { file.value = null; error.value = ''; preview.value = null; publicKey.value = ''; trust.value = false })
function select(files: File[]) {
  const chosen = files[0]
  error.value = ''; file.value = null; preview.value = null; trust.value = false
  if (!chosen) return
  if (!chosen.name.toLowerCase().endsWith('.zbplugin') || chosen.size > 32 * 1024 * 1024) {
    error.value = '请选择不超过 32 MiB 的 .zbplugin 文件。'; return
  }
  file.value = chosen
}
async function submit() {
  if (!file.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    if (!preview.value) { const result = await previewPlugin(file.value, publicKey.value); if (!disposed) preview.value = result; return }
    if (!preview.value.compatibility.compatible || (!preview.value.trusted && !trust.value)) return
    const plugin = await importPlugin(file.value, preview.value, trust.value); if (!disposed) emit('imported', plugin)
  }
  catch (cause: any) { if (!disposed) error.value = cause?.response?.data?.message || '导入失败，请检查插件包和发布者签名后重试。' }
  finally { busy.value = false }
}
</script>

<style scoped>
.plugin-key-fingerprint { overflow-wrap: anywhere; font-family: monospace; }
.plugin-import-preview { display: grid; gap: .5rem; }
details label { display: grid; gap: .5rem; margin-top: .75rem; }
</style>
