<template>
  <div class="kernel-upload">
    <div class="upload-file">
      <UiFileUpload :disabled="pending || disabled" choose-label="选择内核文件" @select="selectFile" />
      <span>{{ file ? `${file.name} · ${(file.size / 1048576).toFixed(1)} MB` : 'Linux x86_64 · zero 或 tar.gz · 最大 128 MB' }}</span>
    </div>
    <FormField v-slot="{ controlAttrs }" label="文件版本" name="kernel-upload-version" required>
      <UiInput v-model.trim="version" v-bind="controlAttrs" :disabled="pending || disabled" placeholder="例如 0.0.3-dev.202610060104" />
    </FormField>
    <div class="upload-action">
      <UiButton :disabled="pending || disabled || !file || !validVersion" :loading="pending" @click="install">{{ pending ? `上传中 ${progress}%` : '上传并安装' }}</UiButton>
      <small>通过已验证 SSH 的 SCP 传输，失败自动回滚。</small>
    </div>
    <PageAlert v-if="error" tone="danger" :title="error" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UiFileUpload from './UiFileUpload.vue'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
import { uploadNodeKernel } from '../api/nodeKernelUpload'
import { compareKernelVersions } from '../utils/kernelVersion'
import { confirmAction } from '../utils/feedback'
import type { AdminTask } from '../api/client'

const props = defineProps<{ nodeId: number; installedVersion?: string; disabled?: boolean }>()
const emit = defineEmits<{ accepted: [task: AdminTask, nodeId: number, target: string]; busy: [value: boolean] }>()
const file = ref<File | null>(null)
const version = ref('')
const pending = ref(false)
const progress = ref(0)
const error = ref('')
let requestKey = ''
const validVersion = computed(() => /^\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?$/.test(version.value))
watch(version, () => { requestKey = '' })
function selectFile(files: File[]) {
  if (pending.value || props.disabled) return
  error.value = ''
  file.value = null
  requestKey = ''
  if (files.length !== 1 || files[0].size <= 0 || files[0].size > 128 * 1048576) { error.value = '请选择不超过 128 MB 的内核文件。'; return }
  file.value = files[0]
}
async function install() {
  if (!file.value || !validVersion.value || pending.value || props.disabled) return
  const binary = file.value, target = version.value, nodeId = props.nodeId
  const downgrade = Boolean(props.installedVersion && compareKernelVersions(props.installedVersion, target) > 0)
  pending.value = true
  emit('busy', true)
  try {
    const accepted = await confirmAction({
      title: downgrade ? '确认降级 Zero 内核' : '安装本地内核',
      message: `将${downgrade ? '降级' : '安装'} ${target} 到当前节点，重启 Zero 服务。安装前校验文件版本与配置，验收失败自动回滚。`,
      confirmText: downgrade ? '确认降级' : '上传并安装', tone: downgrade ? 'danger' : 'primary',
    })
    if (!accepted) return
    error.value = ''; progress.value = 0
    requestKey ||= crypto.randomUUID()
    const task = await uploadNodeKernel(nodeId, binary, target, downgrade, requestKey, percent => { progress.value = percent })
    emit('accepted', task, nodeId, target)
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '上传结果未确认，请重试；同一请求不会重复创建任务。'
  } finally {
    pending.value = false
    emit('busy', false)
  }
}
</script>

<style scoped>
.kernel-upload{display:grid;gap:14px;padding:16px;border:1px solid var(--line);border-radius:10px;background:var(--surface-neutral)}
.upload-file,.upload-action{display:flex;align-items:center;flex-wrap:wrap;gap:12px}
.upload-file span,.upload-action small{color:var(--muted);font-size:12px;overflow-wrap:anywhere}
</style>
