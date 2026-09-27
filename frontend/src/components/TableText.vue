<template>
  <span class="table-text"><PopoverRoot>
    <PopoverTrigger class="table-secondary-text" type="button" :title="text" :aria-label="`查看完整内容：${text}`">{{ text }}</PopoverTrigger>
    <PopoverPortal>
      <PopoverContent class="ui-popover-content table-text-detail" :side-offset="6">
        {{ text }}
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot></span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
const props = defineProps<{ value?: string | number | null }>()
const text = computed(() => props.value === undefined || props.value === null || props.value === '' ? '—' : String(props.value))
</script>

<style scoped>
.table-text { display:block; min-width:0; max-width:100%; }
.table-secondary-text { display:block; min-width:0; max-width:100%; overflow:hidden; text-overflow:ellipsis; padding:0; border:0; background:transparent; color:inherit; font:inherit; text-align:left; cursor:pointer; }
.table-secondary-text:hover { color:var(--foreground); text-decoration:underline; }
.table-secondary-text:focus-visible { outline:2px solid var(--ring); outline-offset:3px; border-radius:2px; }
.table-text-detail { width:max-content; max-width:min(420px, calc(100vw - 64px)); max-height:50vh; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font-size:12px; line-height:1.7; user-select:text; }
</style>
