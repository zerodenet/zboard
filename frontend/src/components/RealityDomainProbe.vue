<template>
  <section class="reality-domain-probe" aria-label="VPS 伪装域名探测" :aria-busy="busy">
    <header><div><strong>从 {{ nodeName || '所选 VPS' }} 探测伪装域名</strong><p>检查候选域名的 TLS 1.3、X25519、HTTP/2 和证书匹配。选择结果后只填入 SNI。</p></div><UiButton variant="ghost" size="sm" aria-label="收起域名探测" @click="$emit('close')"><UiIcon name="close" /></UiButton></header>
    <FormField v-slot="{ controlAttrs }" label="候选域名（可选）" hint="留空检测内置候选；也可输入最多 12 个域名，用换行或逗号分隔。"><UiTextarea v-model="candidates" v-bind="controlAttrs" rows="2" placeholder="www.microsoft.com&#10;www.cloudflare.com" :disabled="busy" /></FormField>
    <UiButton class="probe-action" variant="secondary" :loading="busy" :disabled="!nodeId" @click="probe">{{ busy ? '正在从 VPS 探测…' : '重新探测' }}</UiButton>
    <PageAlert v-if="error" tone="danger" title="域名探测失败">{{ error }}</PageAlert>
    <p v-if="busy" role="status">正在从 VPS 建立 TLS 连接，请稍候。</p>
    <template v-else-if="snapshot">
      <p v-if="!snapshot.items.some(item => item.available)" role="status">本次没有找到通过检测的域名。可以更换候选域名后重试。</p>
      <ul class="probe-results">
        <li v-for="item in snapshot.items" :key="item.server_name">
          <div><strong>{{ item.server_name }}</strong><small>{{ statusLabel(item.status) }} · {{ item.latency_ms }} ms</small></div>
          <UiButton v-if="item.available" variant="secondary" size="sm" @click="$emit('select', item.server_name)">使用此域名</UiButton>
        </li>
      </ul>
      <p class="probe-note">结果来自该 VPS 的当前网络环境；通过检测后仍需验证实际客户端连接。</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { probeRealityDomains, type RealityProbeResult, type RealityProbeSnapshot } from '../api/client'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiTextarea from './UiTextarea.vue'

const props = defineProps<{ nodeId: number; nodeName?: string }>()
defineEmits<{ select: [domain: string]; close: [] }>()
const candidates = ref(''), busy = ref(false), error = ref('')
const snapshot = ref<RealityProbeSnapshot | null>(null)
let controller: AbortController | undefined

function statusLabel(status: RealityProbeResult['status']) {
  return { available: '通过检测', timeout: '连接超时', certificate: '证书不匹配或不可信', unsupported: '不支持所需 TLS / HTTP/2', unreachable: '无法连接或解析域名' }[status]
}
async function probe() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  snapshot.value = null
  error.value = ''
  if (!props.nodeId) { busy.value = false; error.value = '请先选择承载 VPS。'; return }
  const domains = candidates.value.split(/[\s,，]+/).filter(Boolean)
  if (domains.length > 12) { busy.value = false; error.value = '每次最多探测 12 个域名。'; return }
  busy.value = true
  const nodeID = props.nodeId
  try {
    const result = await probeRealityDomains(nodeID, domains, request.signal)
    if (!request.signal.aborted && nodeID === props.nodeId) snapshot.value = result
  } catch (cause: any) {
    if (!request.signal.aborted) error.value = cause?.response?.data?.message || '无法从 VPS 探测域名，请检查 SSH 连接后重试。'
  } finally {
    if (controller === request) busy.value = false
  }
}
watch(() => props.nodeId, () => { void probe() })
onMounted(() => { void probe() })
onBeforeUnmount(() => { controller?.abort() })
</script>

<style scoped>
.reality-domain-probe { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius-floating); background: var(--muted-surface); }
header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
header > div { min-width: 0; }
p { margin: 4px 0 0; color: var(--muted-foreground); font-size: 12px; line-height: 1.6; }
.probe-action { justify-self: start; }
.probe-results { display: grid; gap: 8px; padding: 0; margin: 0; list-style: none; }
.probe-results li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--card); }
.probe-results li > div { min-width: 0; }
strong { font-size: 13px; overflow-wrap: anywhere; }
small { display: block; margin-top: 4px; font-size: 12px; color: var(--muted-foreground); }
</style>
