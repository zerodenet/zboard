<template>
  <section class="path-editor" aria-label="代理路径配置">
    <header><strong>代理节点</strong><p>A 通过这个代理节点连接落地 B。连接信息仅用于 A 的转发路径。</p></header>
    <div class="path-fields">
      <FormField label="配置方式"><UiSelect v-model="mode" aria-label="路径配置方式" :options="[{ label: '单节点表单', value: 'form' }, { label: 'Raw 覆盖', value: 'raw' }]" @change="seedRaw" /></FormField>
      <template v-if="mode === 'form'">
        <FormField label="代理协议"><UiSelect v-model="form.protocol" aria-label="代理协议" :options="pathProtocolOptions" @change="changeProtocol" /></FormField>
        <FormField label="代理节点地址" required><UiInput v-model.trim="form.server" aria-label="代理节点地址" placeholder="proxy.example.com" /></FormField>
        <FormField label="代理节点端口" required><UiInput v-model.number="form.port" aria-label="代理节点端口" type="number" min="1" max="65535" /></FormField>
        <FormField v-if="['socks5', 'mieru'].includes(form.protocol)" label="用户名"><UiInput v-model="form.username" aria-label="代理用户名" autocomplete="off" /></FormField>
        <FormField v-if="['vless', 'vmess'].includes(form.protocol)" label="UUID" required><UiInput v-model.trim="form.id" aria-label="代理 UUID" autocomplete="off" /></FormField>
        <FormField v-else label="密码" :required="form.protocol !== 'socks5'"><UiInput v-model="form.password" aria-label="代理密码" type="password" autocomplete="new-password" /></FormField>
        <FormField v-if="['shadowsocks', 'vmess'].includes(form.protocol)" label="加密方式" required hint="按代理节点配置填写，由 Zero 校验支持情况。"><UiInput v-model.trim="form.cipher" aria-label="代理加密方式" /></FormField>
        <template v-if="['vless', 'vmess'].includes(form.protocol)">
          <FormField label="连接安全"><UiSelect v-model="form.security" aria-label="代理连接安全" :options="securityOptions" /></FormField>
          <FormField label="传输方式"><UiSelect v-model="form.transport" aria-label="代理传输方式" :options="[{ label: 'TCP', value: 'tcp' }, { label: 'WebSocket', value: 'ws' }, { label: 'gRPC', value: 'grpc' }]" /></FormField>
          <FormField v-if="form.protocol === 'vless'" label="Flow"><UiSelect v-model="form.flow" aria-label="代理 Flow" :options="[{ label: '无', value: '' }, { label: 'xtls-rprx-vision', value: 'xtls-rprx-vision' }]" /></FormField>
          <template v-if="form.transport === 'ws'">
            <FormField label="WebSocket 路径"><UiInput v-model="form.path" aria-label="WebSocket 路径" placeholder="/" /></FormField>
            <FormField label="WebSocket Host"><UiInput v-model.trim="form.host" aria-label="WebSocket Host" /></FormField>
          </template>
          <FormField v-if="form.transport === 'grpc'" label="gRPC 服务名" required hint="多个服务名用逗号分隔。"><UiInput v-model="form.serviceName" aria-label="gRPC 服务名" /></FormField>
        </template>
        <template v-if="tlsVisible">
          <FormField v-if="form.protocol !== 'hysteria2'" label="TLS / Reality 服务名"><UiInput v-model.trim="form.serverName" aria-label="代理服务名" placeholder="可留空使用代理节点域名" /></FormField>
          <FormField label="客户端指纹"><UiInput v-model.trim="form.fingerprint" aria-label="代理客户端指纹" placeholder="例如 chrome；可留空" /></FormField>
          <FormField v-if="form.security !== 'reality'" label="证书校验"><label class="check-field"><UiCheckbox v-model="form.insecure" /><span>跳过证书校验</span></label></FormField>
        </template>
        <template v-if="form.protocol === 'vless' && form.security === 'reality'">
          <FormField label="Reality 公钥" required><UiInput v-model.trim="form.publicKey" aria-label="Reality 公钥" /></FormField>
          <FormField label="Reality Short ID"><UiInput v-model.trim="form.shortID" aria-label="Reality Short ID" /></FormField>
        </template>
        <p class="path-note">更多传输参数、多跳代理链、测速或选择组可切换到 Raw 覆盖，保留 Zero 支持的完整配置。</p>
      </template>
      <FormField v-else label="Raw 覆盖配置" full hint="此对象完整替代表单。接受单个协议节点、单个 outbound，或含 outbounds、outbound_groups、target 的完整路径。保存时由 Zero 校验。">
        <UiTextarea v-model="raw" aria-label="Raw 覆盖配置" rows="16" spellcheck="false" />
      </FormField>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import UiCheckbox from './UiCheckbox.vue'
import { buildPathNode, emptyPathNode, parseRawPath, pathProtocolOptions } from '../utils/networkEntryPath'

const form = reactive(emptyPathNode())
const mode = ref('form'), raw = ref('')
const securityOptions = computed(() => [{ label: '无', value: 'none' }, { label: 'TLS', value: 'tls' }, ...(form.protocol === 'vless' ? [{ label: 'Reality', value: 'reality' }] : [])])
const tlsVisible = computed(() => ['trojan', 'hysteria2'].includes(form.protocol) || (['vless', 'vmess'].includes(form.protocol) && form.security !== 'none'))
function changeProtocol() {
  form.cipher = form.protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : 'aes-128-gcm'
  form.security = 'none'; form.transport = 'tcp'; form.flow = ''
}
function seedRaw() { if (mode.value === 'raw' && !raw.value.trim()) raw.value = JSON.stringify(buildPathNode(form, false), null, 2) }
function build() { return mode.value === 'raw' ? parseRawPath(raw.value) : buildPathNode(form) }
defineExpose({ build })
</script>

<style scoped>
.path-editor{grid-column:1/-1;padding:16px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}header p,.path-note{color:var(--muted);font-size:12px;line-height:1.6}.path-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.path-note{grid-column:1/-1;margin:0}.path-fields :deep(textarea){font-family:var(--font-mono);width:100%}.check-field{display:flex;align-items:center;gap:8px}@media(max-width:640px){.path-fields{grid-template-columns:1fr}}
</style>
