<template>
  <details ref="disclosure" class="settings-disclosure">
    <summary :aria-disabled="busy || undefined" @click="busy && $event.preventDefault()"><span>{{ title }}</span><span v-if="dirty" class="settings-disclosure-dirty" aria-label="有未保存修改" /><UiIcon name="chevron" /></summary>
    <div class="settings-disclosure-body"><slot /></div>
  </details>
</template>
<script setup lang="ts">
import UiIcon from './UiIcon.vue'
import { nextTick, ref, watch } from 'vue'
const props = defineProps<{ title: string; dirty?: boolean; busy?: boolean; error?: boolean }>()
const disclosure = ref<HTMLDetailsElement | null>(null)
watch(() => props.error, async value => { if (value) { await nextTick(); if (disclosure.value) disclosure.value.open = true } }, { immediate: true })
</script>
<style scoped>
.settings-disclosure { min-width: 0; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); }
.settings-disclosure > summary { display: flex; align-items: center; gap: 10px; min-height: 52px; padding: 14px 18px; cursor: pointer; list-style: none; font-size: 14px; font-weight: 500; }
.settings-disclosure > summary::-webkit-details-marker { display: none; }
.settings-disclosure > summary > .ui-icon { margin-left: auto; width: 14px; height: 14px; color: var(--muted-foreground); }
.settings-disclosure[open] > summary > .ui-icon { transform: rotate(90deg); }
.settings-disclosure > summary:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: var(--radius-card); }
.settings-disclosure-dirty { width: 6px; height: 6px; border-radius: 50%; background: var(--warning); }
.settings-disclosure-body { padding: 0 18px 18px; min-width: 0; }
</style>
