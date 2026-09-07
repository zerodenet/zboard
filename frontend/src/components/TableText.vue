<template>
  <span class="table-text">
    <button type="button" class="table-secondary-text" :title="text" :aria-label="`查看完整内容：${text}`" @click="popover?.toggle($event)">{{ text }}</button>
    <PrimePopover ref="popover"><div class="table-text-detail">{{ text }}</div></PrimePopover>
  </span>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import PrimePopover from 'primevue/popover'
const props = defineProps<{ value?: string | number | null }>()
const text = computed(() => props.value === undefined || props.value === null || props.value === '' ? '—' : String(props.value))
const popover = ref<InstanceType<typeof PrimePopover>>()
</script>

<style scoped>
.table-text { display:block; min-width:0; max-width:100%; }
.table-secondary-text { padding:0; border:0; background:transparent; color:inherit; font:inherit; text-align:left; cursor:pointer; }
.table-secondary-text:hover { color:var(--primary); }
.table-secondary-text:focus-visible { outline:2px solid var(--primary); outline-offset:3px; border-radius:2px; }
.table-text-detail { width:max-content; max-width:min(420px, calc(100vw - 64px)); max-height:50vh; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font-size:12px; line-height:1.7; user-select:text; }
</style>
