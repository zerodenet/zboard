<template>
  <ModalDialog :open="Boolean(orderId)" :title="`订单 #${orderId} 的订阅地址`" description="仅对应此订单履约的订阅，可交付给该客户使用。" @close="$emit('close')">
    <p v-if="loading" role="status">正在读取订阅地址…</p>
    <PageAlert v-else-if="error" tone="danger" role="alert">{{ error }}<template #actions><UiButton variant="secondary" @click="load">重试</UiButton></template></PageAlert>
    <template v-else-if="url">
      <p>关联订阅 #{{ access?.subscription_id }}</p>
      <label ref="deliveryField" class="delivery-link">订阅地址<UiInput :value="url" readonly autocomplete="off" spellcheck="false" @focus="selectLink" /></label>
      <p role="status">{{ feedback || '此地址包含访问凭据，请仅交付给该客户。' }}</p>
    </template>
    <p v-else>{{ access?.notice || '此订单暂时没有可用的订阅地址。' }}</p>
    <template #footer>
      <UiButton variant="secondary" @click="$emit('close')">关闭</UiButton>
      <UiButton v-if="url" :disabled="copying" @click="copy">{{ copied ? '已复制' : '复制订阅地址' }}</UiButton>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import { fetchOrderSubscriptionAccess, type SubscriptionAccess } from '../api/subscriptionAccess'

const props = defineProps<{ orderId: number }>()
defineEmits<{ close: [] }>()
const access = ref<SubscriptionAccess | null>(null)
const deliveryField = ref<HTMLElement | null>(null)
const error = ref(''), feedback = ref(''), loading = ref(false), copying = ref(false), copied = ref(false)
let generation = 0, controller: AbortController | undefined
const url = computed(() => {
  const path = access.value?.configured ? access.value.subscription_url : ''
  return path?.startsWith('/api/v1/client/subscription/') ? new URL(path, window.location.origin).toString() : ''
})
async function load() {
  const request = ++generation
  controller?.abort(); controller = new AbortController()
  access.value = null; error.value = ''; feedback.value = ''; copied.value = false; copying.value = false
  loading.value = Boolean(props.orderId)
  if (!props.orderId) return
  try {
    const result = await fetchOrderSubscriptionAccess(props.orderId, { signal: controller.signal })
    if (request === generation) access.value = result
  } catch (cause: any) {
    if (request === generation) error.value = cause?.response?.data?.message || '无法读取订阅地址，请重试。'
  } finally { if (request === generation) loading.value = false }
}
function selectLink(event: Event) { (event.target as HTMLInputElement).select() }
async function copy() {
  if (!url.value || copying.value) return
  const request = generation
  copying.value = true
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(url.value)
    } else {
      const input = deliveryField.value?.querySelector('input')
      input?.focus(); input?.select()
      if (!input || !document.execCommand('copy')) throw new Error('copy unavailable')
    }
    if (request === generation) { copied.value = true; feedback.value = '订阅地址已复制。' }
  } catch {
    if (request === generation) feedback.value = '浏览器未允许复制，请选中上方地址手动复制。'
  } finally { if (request === generation) copying.value = false }
}
watch(() => props.orderId, load, { immediate: true })
onScopeDispose(() => { generation++; controller?.abort(); access.value = null })
</script>

<style scoped>
.delivery-link { display: grid; gap: .5rem; }
.delivery-link :deep(input) { width: 100%; min-width: 0; font-family: monospace; }
</style>
