<template>
  <div class="endpoint-address-field">
    <PopoverRoot v-model:open="open">
      <span v-if="!overflowing" class="endpoint-address mono"><span ref="hostElement" class="endpoint-address-host">{{ displayHost }}</span><span v-if="port" class="endpoint-address-port">:{{ port }}</span></span>
      <PopoverTrigger v-else as-child><button class="endpoint-address endpoint-address-trigger mono" type="button" :aria-label="`查看完整地址 ${fullAddress}`"><span ref="hostElement" class="endpoint-address-host">{{ displayHost }}</span><span v-if="port" class="endpoint-address-port">:{{ port }}</span></button></PopoverTrigger>
      <PopoverPortal v-if="overflowing">
        <PopoverContent as-child align="start" :side-offset="6" :collision-padding="16">
          <div class="ui-popover-content endpoint-address-detail"><code>{{ fullAddress }}</code></div>
        </PopoverContent>
      </PopoverPortal>
    </PopoverRoot>
    <Tooltip text="复制地址"><UiButton type="button" variant="ghost" size="sm" icon :aria-label="`复制地址 ${fullAddress}`" @click="copyAddress"><UiIcon :name="copyMessage === '地址已复制' ? 'check' : 'copy'" /></UiButton></Tooltip>
    <span v-if="copyMessage" role="status" :class="copyFailed ? 'endpoint-copy-error' : 'sr-only'">{{ copyMessage }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useTextOverflow } from '../composables/useTextOverflow'
import Tooltip from './ui/Tooltip.vue'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{ address: string; port?: number }>()
const copyMessage = ref(''), copyFailed = ref(false), open = ref(false)
const hostElement = ref<HTMLElement | null>(null)
const displayHost = computed(() => props.port && props.address.includes(':') && !props.address.startsWith('[') ? `[${props.address}]` : props.address)
const fullAddress = computed(() => `${displayHost.value}${props.port ? `:${props.port}` : ''}`)
const overflowing = useTextOverflow(hostElement, () => fullAddress.value)
watch(overflowing, clipped => { if (!clipped) open.value = false })
watch(fullAddress, () => { copyMessage.value = ''; copyFailed.value = false; open.value = false })
async function copyAddress() {
  const value = fullAddress.value
  copyMessage.value = ''; copyFailed.value = false
  try {
    await navigator.clipboard.writeText(value)
    if (value !== fullAddress.value) return
    copyMessage.value = '地址已复制'
  } catch {
    if (value !== fullAddress.value) return
    copyFailed.value = true; copyMessage.value = '复制失败，请手动复制地址。'
    if (overflowing.value) open.value = true
  }
}
</script>

<style scoped>
.endpoint-address { display:flex; align-items:center; width:100%; max-width:240px; min-width:0; padding:4px 0; border:0; border-radius:3px; background:transparent; color:inherit; font-size:11px; text-align:left; user-select:text; }
.endpoint-address-trigger { cursor:pointer; }
.endpoint-address-trigger:hover { color:var(--foreground); text-decoration:underline; }
.endpoint-address-trigger:focus-visible { outline:2px solid var(--ring); outline-offset:3px; }
.endpoint-address-host { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.endpoint-address-port { flex:none; white-space:nowrap; }
.endpoint-address-detail { box-sizing:border-box; display:grid; gap:12px; width:340px; max-width:min(calc(100vw - 32px), var(--reka-popover-content-available-width, 340px)); max-height:var(--reka-popover-content-available-height, 80vh); min-width:0; overflow:auto; font-size:13px; overscroll-behavior:contain; }
.endpoint-address-detail code { white-space:normal; overflow-wrap:anywhere; user-select:text; line-height:1.7; }
.endpoint-address-field { display:grid; grid-template-columns:minmax(0, 1fr) auto; align-items:center; gap:4px; width:100%; max-width:280px; min-width:0; }
.endpoint-copy-error { grid-column:1 / -1; color:var(--destructive); font-size:12px; line-height:1.5; }
</style>
