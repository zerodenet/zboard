<template>
  <span class="business-reference">
    <span class="reference-label">{{ label }}</span>
    <code>{{ value }}</code>
    <UiButton type="button" variant="ghost" size="sm" icon :aria-label="`复制${label}`" :disabled="copying || !value || value === '-'" @click="copy"><UiIcon :name="copied ? 'check' : 'copy'" /></UiButton>
    <span v-if="copied" class="sr-only" role="status">{{ label }}已复制</span>
    <span v-if="failed" class="reference-copy-error" role="alert">复制失败，请选中编号复制。</span>
  </span>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
const props = defineProps<{ value: string; label: string }>()
const copied = ref(false), failed = ref(false), copying = ref(false)
watch(() => props.value, () => { copied.value = false; failed.value = false })
async function copy() {
  const value = props.value
  copied.value = false; failed.value = false; copying.value = true
  try { await navigator.clipboard.writeText(value); if (value === props.value) copied.value = true }
  catch { if (value === props.value) failed.value = true }
  finally { copying.value = false }
}
</script>
<style scoped>
.business-reference { display:inline-flex; max-width:100%; align-items:center; flex-wrap:wrap; gap:4px; vertical-align:middle; }
.reference-label { color:var(--muted-foreground); font-size:12px; }
.business-reference code { font:inherit; font-variant-numeric:tabular-nums; user-select:all; overflow-wrap:anywhere; }
.reference-copy-error { color:var(--destructive); font-size:12px; }
</style>
