<template>
  <button class="endpoint-address mono" type="button" :title="fullAddress" :aria-label="`查看完整地址 ${fullAddress}`" @click="popover?.toggle($event)">
    <span class="endpoint-address-host">{{ displayHost }}</span><span v-if="port" class="endpoint-address-port">:{{ port }}</span>
  </button>
  <PrimePopover ref="popover" @show="copyMessage = ''">
    <div class="endpoint-address-detail">
      <strong>完整地址</strong>
      <code>{{ fullAddress }}</code>
      <UiButton type="button" variant="secondary" size="sm" @click="copyAddress"><UiIcon name="copy" />复制地址</UiButton>
      <span v-if="copyMessage" role="status">{{ copyMessage }}</span>
    </div>
  </PrimePopover>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import PrimePopover from 'primevue/popover'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{ address: string; port?: number }>()
const popover = ref<InstanceType<typeof PrimePopover>>()
const copyMessage = ref('')
const displayHost = computed(() => props.port && props.address.includes(':') && !props.address.startsWith('[') ? `[${props.address}]` : props.address)
const fullAddress = computed(() => `${displayHost.value}${props.port ? `:${props.port}` : ''}`)

async function copyAddress() {
  try {
    await navigator.clipboard.writeText(fullAddress.value)
    copyMessage.value = '地址已复制'
  } catch {
    copyMessage.value = '无法访问剪贴板，请选择上方完整地址手动复制。'
  }
}
</script>

<style scoped>
.endpoint-address { display:flex; align-items:center; width:100%; max-width:240px; min-width:0; padding:4px 0; border:0; border-radius:3px; background:transparent; color:inherit; font-size:11px; text-align:left; cursor:pointer; }
.endpoint-address:hover { color:var(--primary); }
.endpoint-address:focus-visible { outline:2px solid var(--primary); outline-offset:3px; }
.endpoint-address-host { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.endpoint-address-port { flex:none; white-space:nowrap; }
.endpoint-address-detail { display:grid; gap:12px; width:min(340px, calc(100vw - 64px)); font-size:12px; }
.endpoint-address-detail code { white-space:normal; overflow-wrap:anywhere; user-select:text; line-height:1.7; }
.endpoint-address-detail .p-button { justify-self:start; }
.endpoint-address-detail [role='status'] { color:var(--text-secondary); line-height:1.5; }
</style>
