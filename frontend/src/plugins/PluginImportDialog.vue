<template>
  <ModalDialog :open="open" :title="marketId ? '在线安装插件' : '离线导入插件'" description="先查看插件来源和所需能力，再确认导入。" :busy="busy" @close="$emit('close')">
    <div class="plugin-import-content">
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
      <UiFileUpload v-if="!marketId" choose-label="选择插件包" accept=".zbplugin" :max-file-size="32 * 1024 * 1024" :disabled="busy" @select="select" />
      <div v-if="file" class="plugin-import-file"><strong>{{ file.name }}</strong><span>{{ (file.size / 1024 / 1024).toFixed(2) }} MiB</span></div>
      <p v-else-if="!marketId">支持 .zbplugin 文件，最大 32 MiB。</p>
      <p v-if="marketId && busy && !preview" role="status">正在下载并校验安装包…</p>
      <details v-if="!marketId && !preview"><summary>旧版插件包兼容选项</summary><label>发布者公钥（.pub 文件内容）<input v-model="publicKey" :disabled="busy" autocomplete="off" placeholder="仅旧包未附带公钥时需要" /></label></details>
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
        <PageAlert v-if="preview.compatibility.warning" tone="warning">{{ preview.compatibility.warning }}</PageAlert>
        <label v-if="!preview.trusted"><input v-model="trust" type="checkbox" :disabled="busy" /> 我已确认来源，信任此插件的签名密钥</label>
      </div>
      <p>新安装保持停用，配置后即可启用。升级自动迁移数据并保留原启停状态，失败时保留原版本和数据。</p>
    </div>
    <template #footer>
      <UiButton variant="secondary" :disabled="busy" @click="$emit('close')">取消</UiButton>
      <UiButton :disabled="(!file && !marketId) || busy || (!!preview && (!preview.compatibility.compatible || (!preview.trusted && !trust)))" :loading="busy" @click="submit">{{ preview ? (marketId ? '确认安装' : '确认导入') : '查看插件' }}</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import ModalDialog from '../components/ModalDialog.vue'
import UiFileUpload from '../components/UiFileUpload.vue'
import UiButton from '../components/UiButton.vue'
import PageAlert from '../components/PageAlert.vue'
import { importPlugin, previewPlugin, previewMarketPlugin, confirmMarketPlugin, capabilityLabel, surfaceLabel, type ImportPreview, type Plugin } from '../api/plugins'
const props = defineProps<{ open: boolean; marketId?: string; marketVersion?: string }>()
const emit = defineEmits<{ close: []; imported: [plugin: Plugin] }>()
const file = ref<File | null>(null), error = ref(''), busy = ref(false)
const preview = ref<ImportPreview | null>(null), publicKey = ref(''), trust = ref(false)
let generation = 0
let controller: AbortController | undefined
onScopeDispose(() => { generation++; controller?.abort() })
watch(() => [props.open, props.marketId, props.marketVersion], () => {
  generation++; controller?.abort(); busy.value = false
  file.value = null; error.value = ''; preview.value = null; publicKey.value = ''; trust.value = false
  if (props.open && props.marketId) void submit()
}, { immediate: true })
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
  if ((!file.value && !props.marketId) || busy.value) return
  const request = generation
  controller = new AbortController()
  busy.value = true; error.value = ''
  try {
    if (!preview.value) { const result = props.marketId ? await previewMarketPlugin(props.marketId, props.marketVersion || '', controller.signal) : await previewPlugin(file.value!, publicKey.value); if (request === generation) preview.value = result; return }
    if (!preview.value.compatibility.compatible || (!preview.value.trusted && !trust.value)) return
    const plugin = props.marketId ? await confirmMarketPlugin(props.marketId, props.marketVersion || '', preview.value, trust.value) : await importPlugin(file.value!, preview.value, trust.value); if (request === generation) emit('imported', plugin)
  }
  catch (cause: any) { if (request === generation) error.value = cause?.response?.data?.message || '导入失败，请检查插件包和发布者签名后重试。' }
  finally { if (request === generation) busy.value = false }
}
</script>

<style scoped>
.plugin-key-fingerprint { overflow-wrap: anywhere; font-family: monospace; }
.plugin-import-preview { display: grid; gap: .5rem; }
details label { display: grid; gap: .5rem; margin-top: .75rem; }
</style>
