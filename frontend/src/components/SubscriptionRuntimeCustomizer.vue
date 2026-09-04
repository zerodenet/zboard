<template>
  <section class="runtime-settings">
    <div class="runtime-grid">
      <FormField label="初始运行模式" name="template-route-mode" hint="规则模式按规则集匹配；全局模式全部走主策略组；直连模式绕过代理。" required>
        <template #default="{ controlAttrs }"><UiSelect v-model="model.mode" v-bind="controlAttrs" :options="modeOptions" /></template>
      </FormField>
      <FormField v-if="model.mixed_enabled" label="本地混合代理端口" name="template-mixed-port" hint="在 127.0.0.1 同时提供 HTTP CONNECT 和 SOCKS 代理。" required>
        <template #default="{ controlAttrs }"><UiNumberInput v-model="model.mixed_port" v-bind="controlAttrs" :min="1" :max="65535" /></template>
      </FormField>
    </div>

    <label class="setting-switch">
      <span><strong>启用本地 HTTP/SOCKS 代理</strong><small>关闭后不监听本地混合代理端口；需同时启用 TUN 才能接管流量。</small></span>
      <UiCheckbox v-model="model.mixed_enabled" role="switch" @update:model-value="toggleMixed" />
    </label>

    <label v-if="renderer === 'sing-box' && model.mixed_enabled" class="setting-switch">
      <span><strong>自动设置系统 HTTP 代理</strong><small>启动 sing-box 时将系统代理指向上述混合端口，退出时自动清理。</small></span>
      <UiCheckbox v-model="model.system_proxy" role="switch" />
    </label>

    <template v-if="supportsRuntimeNetwork">
      <label class="setting-switch">
        <span><strong>启用 DNS</strong><small>生成客户端原生 DNS 服务、缓存、地址族策略与可选 Fake-IP。</small></span>
        <UiCheckbox v-model="model.dns.enabled" role="switch" />
      </label>
      <div v-if="model.dns.enabled" class="nested-settings">
        <div class="runtime-grid">
          <FormField label="默认 DNS" name="template-dns-default" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="model.dns.default_server" v-bind="controlAttrs" :options="dnsDefaultOptions" /></template>
          </FormField>
          <FormField label="地址族策略" name="template-dns-strategy" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="model.dns.strategy" v-bind="controlAttrs" :options="dnsStrategyOptions" /></template>
          </FormField>
        </div>
        <div class="dns-list-heading"><strong>DNS 服务器</strong><UiButton type="button" size="sm" variant="secondary" :disabled="model.dns.servers.length >= 8" @click="addDNSServer"><UiIcon name="plus" />添加</UiButton></div>
        <div class="dns-server-list">
          <article v-for="(server, index) in model.dns.servers" :key="index" class="dns-server-row">
            <div class="runtime-grid dns-server-grid">
              <FormField label="标识" :name="`template-dns-${index}-tag`" required><template #default="{ controlAttrs }"><UiInput v-model.trim="server.tag" v-bind="controlAttrs" maxlength="64" /></template></FormField>
              <FormField label="类型" :name="`template-dns-${index}-type`" required><template #default="{ controlAttrs }"><UiSelect v-model="server.type" v-bind="controlAttrs" :options="dnsTypeOptions" @change="normalizeDNSServer(server)" /></template></FormField>
              <template v-if="server.type !== 'system'">
                <FormField label="服务器 IP" :name="`template-dns-${index}-host`" hint="域名与自定义 bootstrap 请使用高级覆盖。" required><template #default="{ controlAttrs }"><UiInput v-model.trim="server.host" v-bind="controlAttrs" placeholder="1.1.1.1" /></template></FormField>
                <FormField label="端口" :name="`template-dns-${index}-port`" required><template #default="{ controlAttrs }"><UiNumberInput v-model="server.port" v-bind="controlAttrs" :min="1" :max="65535" /></template></FormField>
                <FormField v-if="server.type === 'doh'" label="DoH 路径" :name="`template-dns-${index}-path`"><template #default="{ controlAttrs }"><UiInput v-model.trim="server.path" v-bind="controlAttrs" placeholder="/dns-query" /></template></FormField>
                <FormField v-if="['doh','dot','doq'].includes(server.type)" label="TLS 服务器名" :name="`template-dns-${index}-sni`"><template #default="{ controlAttrs }"><UiInput v-model.trim="server.server_name" v-bind="controlAttrs" placeholder="cloudflare-dns.com" /></template></FormField>
              </template>
            </div>
            <UiButton type="button" size="sm" variant="ghost" :disabled="model.dns.servers.length <= 1" @click="removeDNSServer(index)">删除</UiButton>
          </article>
        </div>
        <div class="runtime-grid">
          <label class="setting-switch compact"><span><strong>DNS 缓存</strong><small>减少重复查询。</small></span><UiCheckbox v-model="model.dns.cache_enabled" role="switch" /></label>
          <FormField v-if="model.dns.cache_enabled" label="缓存容量" name="template-dns-cache"><template #default="{ controlAttrs }"><UiNumberInput v-model="model.dns.cache_capacity" v-bind="controlAttrs" :min="1" :max="65536" /></template></FormField>
          <label class="setting-switch compact"><span><strong>Fake-IP</strong><small>透明代理时保留域名映射。</small></span><UiCheckbox v-model="model.dns.fake_ip_enabled" role="switch" /></label>
          <template v-if="model.dns.fake_ip_enabled">
            <FormField label="Fake-IP IPv4 地址池" name="template-dns-fake-v4"><template #default="{ controlAttrs }"><UiInput v-model.trim="model.dns.fake_ipv4_range" v-bind="controlAttrs" /></template></FormField>
            <FormField label="Fake-IP IPv6 地址池" name="template-dns-fake-v6"><template #default="{ controlAttrs }"><UiInput v-model.trim="model.dns.fake_ipv6_range" v-bind="controlAttrs" /></template></FormField>
          </template>
        </div>
      </div>

      <label class="setting-switch">
        <span><strong>启用 TUN 全局接管</strong><small>关闭时只处理主动使用本地 HTTP/SOCKS 端口的应用；开启后由 TUN 接管系统流量。</small></span>
        <UiCheckbox v-model="model.tun.enabled" role="switch" @update:model-value="toggleTun" />
      </label>
      <div v-if="model.tun.enabled" class="nested-settings runtime-grid">
        <FormField label="TUN IPv4 地址" name="template-tun-v4" required><template #default="{ controlAttrs }"><UiInput v-model.trim="model.tun.addresses[0]" v-bind="controlAttrs" /></template></FormField>
        <FormField label="TUN IPv6 地址" name="template-tun-v6" hint="留空可仅启用 IPv4。"><template #default="{ controlAttrs }"><UiInput v-model.trim="model.tun.addresses[1]" v-bind="controlAttrs" /></template></FormField>
        <FormField label="MTU" name="template-tun-mtu"><template #default="{ controlAttrs }"><UiNumberInput v-model="model.tun.mtu" v-bind="controlAttrs" :min="576" :max="9000" /></template></FormField>
        <div class="tun-switches">
          <label><UiCheckbox v-model="model.tun.auto_route" />自动路由</label>
          <label><UiCheckbox v-model="model.tun.strict_route" />严格路由</label>
          <label><UiCheckbox v-model="model.tun.dns_hijack" @update:model-value="toggleDNSHijack" />DNS 劫持</label>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SubscriptionDNSServer, SubscriptionRenderer, SubscriptionTemplateCustomization } from '../api/client'
import { subscriptionRendererSupportsRuntimeNetwork, type SupportedSubscriptionRenderer } from '../utils/subscriptionTemplateEditor'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import UiNumberInput from './UiNumberInput.vue'
import UiSelect from './UiSelect.vue'

const props = defineProps<{ renderer: SubscriptionRenderer | SupportedSubscriptionRenderer }>()
const model = defineModel<SubscriptionTemplateCustomization>({ required: true })
const supportsRuntimeNetwork = computed(() => subscriptionRendererSupportsRuntimeNetwork(props.renderer))
const modeOptions = [
  { label: '规则', value: 'rule' }, { label: '全局代理', value: 'global' }, { label: '全部直连', value: 'direct' },
]
const dnsStrategyOptions = [
  { label: '优先 IPv4', value: 'prefer_ipv4' }, { label: '优先 IPv6', value: 'prefer_ipv6' },
  { label: '仅 IPv4', value: 'ipv4_only' }, { label: '仅 IPv6', value: 'ipv6_only' },
]
const dnsTypeOptions = computed(() => [
  { label: '系统 DNS', value: 'system' }, { label: 'UDP', value: 'udp' },
  ...(['zero', 'znet-sink'].includes(props.renderer) ? [] : [{ label: 'TCP', value: 'tcp' }]),
  { label: 'DoH', value: 'doh' }, { label: 'DoT', value: 'dot' }, { label: 'DoQ', value: 'doq' },
])
const dnsDefaultOptions = computed(() => model.value.dns.servers.map(server => ({ label: server.tag || '未命名', value: server.tag })))

function addDNSServer() {
  const used = new Set(model.value.dns.servers.map(server => server.tag))
  let index = model.value.dns.servers.length + 1
  while (used.has(`dns-${index}`)) index += 1
  model.value.dns.servers.push({ tag: `dns-${index}`, type: 'udp', host: '1.1.1.1', port: 53 })
}

function removeDNSServer(index: number) {
  const [removed] = model.value.dns.servers.splice(index, 1)
  if (model.value.dns.default_server === removed.tag) model.value.dns.default_server = model.value.dns.servers[0]?.tag || ''
}

function normalizeDNSServer(server: SubscriptionDNSServer) {
  if (server.type === 'system') {
    delete server.host; delete server.port; delete server.path; delete server.server_name
    return
  }
  server.host ||= '1.1.1.1'
  server.port = ['doh'].includes(server.type) ? 443 : ['dot', 'doq'].includes(server.type) ? 853 : 53
  if (server.type === 'doh') server.path ||= '/dns-query'
  else delete server.path
  if (!['doh', 'dot', 'doq'].includes(server.type)) delete server.server_name
}

function toggleTun(enabled: boolean) {
  if (enabled && model.value.tun.dns_hijack) model.value.dns.enabled = true
}

function toggleMixed(enabled: boolean) {
  if (!enabled) model.value.system_proxy = false
}

function toggleDNSHijack(enabled: boolean) {
  if (enabled) model.value.dns.enabled = true
}
</script>

<style scoped>
.runtime-settings,.nested-settings,.dns-server-list{display:grid;gap:10px}.runtime-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 12px}.setting-switch{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:11px 12px;border:1px solid var(--line);border-radius:9px;background:var(--surface-soft)}.setting-switch span{display:grid;gap:3px}.setting-switch strong,.dns-list-heading strong{font-size:10px;color:var(--text-strong)}.setting-switch small{font-size:9px;color:var(--muted);line-height:1.5}.setting-switch.compact{min-height:58px}.nested-settings{padding:12px;border:1px solid var(--line);border-radius:9px;background:var(--surface-soft)}.dns-list-heading,.dns-server-row{display:flex;align-items:center;justify-content:space-between;gap:10px}.dns-server-row{padding:10px;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.dns-server-grid{flex:1}.tun-switches{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.tun-switches label{display:flex;align-items:center;gap:6px;font-size:10px}@media(max-width:760px){.runtime-grid{grid-template-columns:1fr}.dns-server-row{align-items:flex-start;flex-direction:column}}
</style>
