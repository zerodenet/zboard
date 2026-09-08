<template>
  <ModalDialog :open="true" title="插件能力授权" :description="`${plugin.name} · v${plugin.version}`" :busy="busy" :dirty="dirty" @close="$emit('close')">
    <div class="plugin-authorization-form">
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
      <PageAlert tone="info">授权仅对当前插件包生效。保存后插件保持停用，已有页面和未完成的登录授权将失效。</PageAlert>
      <label v-for="cap in plugin.manifest.capabilities" :key="cap" class="plugin-permission-option">
        <UiCheckbox :model-value="selected.includes(cap)" :disabled="busy" @update:model-value="toggle(cap, $event)" />
        <span><strong>{{ capabilityLabel(cap) }}</strong><small>{{ descriptions[cap] || cap }}</small></span>
      </label>
      <template v-if="plugin.manifest.components.server">
        <PageAlert tone="warning">此插件包含原生服务进程。目前没有操作系统沙箱，接口授权不能限制该进程账号的文件和网络权限。只应运行经过审查、信任的插件。</PageAlert>
        <label class="plugin-permission-option"><UiCheckbox v-model="nativeTrusted" :disabled="busy" /><span>我信任此包的原生程序，并允许运行</span></label>
      </template>
      <p class="plugin-permission-note">核心规则始终优先：关闭站点注册后，未绑定的第三方身份不能创建账户或登录。宿主不开放直接修改用户权限、凭证、订单或节点的接口。</p>
    </div>
    <template #footer="{ requestClose }"><UiButton variant="secondary" :disabled="busy" @click="requestClose">取消</UiButton><UiButton :loading="busy" @click="save">保存授权并保持停用</UiButton></template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { onScopeDispose, ref } from 'vue'
import ModalDialog from '../components/ModalDialog.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiButton from '../components/UiButton.vue'
import PageAlert from '../components/PageAlert.vue'
import { useDirtyForm } from '../composables/useFormState'
import { capabilityLabel, savePluginAuthorization, type Plugin } from '../api/plugins'
const props = defineProps<{ plugin: Plugin }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const selected = ref(props.plugin.authorization?.reviewed ? [...props.plugin.authorization.granted] : [])
const nativeTrusted = ref(!!props.plugin.authorization?.reviewed && props.plugin.authorization.native_trusted)
const { dirty, markClean } = useDirtyForm(() => [selected.value, nativeTrusted.value])
const busy = ref(false), error = ref('')
let disposed = false
onScopeDispose(() => { disposed = true })
const descriptions: Record<string, string> = {
  'zboard.ui.page.v1': '只能在声明的位置展示隔离页面，页面位置不授予业务权限。',
  'zboard.config.v1': '读取公开配置投影、提交自身配置和检测参数。',
  'zboard.identity.provider.v1': '提供第三方身份验证结果；登录、注册、绑定和会话由核心裁定。',
  'zboard.storage.v1': '只访问自身的加密 JSON 数据；最多 128 个键、256 KiB，不提供 SQL。',
}
function toggle(cap: string, checked: boolean) { selected.value = checked ? [...selected.value, cap] : selected.value.filter(c => c !== cap) }
async function save() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await savePluginAuthorization(props.plugin, selected.value, nativeTrusted.value); if (!disposed) { markClean(); emit('saved') } }
  catch (cause: any) { if (!disposed) error.value = cause?.response?.data?.message || '授权保存失败，请刷新插件详情后重试。' }
  finally { busy.value = false }
}
</script>
