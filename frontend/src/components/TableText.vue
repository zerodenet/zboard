<template>
  <span class="table-text"><span v-if="!hasValue" aria-label="无数据">-</span><span v-else-if="!overflowing" ref="textElement" class="table-secondary-text">{{ text }}</span><PopoverRoot v-else>
    <PopoverTrigger as-child><button ref="textElement" class="table-secondary-text table-text-trigger" type="button" :aria-label="`查看完整内容：${text}`">{{ text }}</button></PopoverTrigger>
    <PopoverPortal>
      <PopoverContent as-child align="start" :side-offset="6" :collision-padding="16">
        <div class="ui-popover-content table-text-detail">{{ text }}</div>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot></span>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useTextOverflow } from '../composables/useTextOverflow'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
const props = defineProps<{ value?: string | number | null }>()
const hasValue = computed(() => props.value != null && String(props.value).trim() !== '')
const text = computed(() => hasValue.value ? String(props.value) : '-')
const textElement = ref<HTMLElement | null>(null)
const overflowing = useTextOverflow(textElement, () => props.value)
</script>

<style scoped>
.table-text { display:block; min-width:0; max-width:100%; }
.table-secondary-text { display:block; width:100%; min-width:0; max-width:100%; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; padding:0; border:0; background:transparent; color:inherit; font:inherit; text-align:left; }
.table-text-trigger { cursor:pointer; }
.table-text-trigger:hover { color:var(--foreground); text-decoration:underline; }
.table-text-trigger:focus-visible { outline:2px solid var(--ring); outline-offset:3px; border-radius:2px; }
.table-text-detail { box-sizing:border-box; width:max-content; max-width:min(420px, calc(100vw - 32px), var(--reka-popover-content-available-width, 420px)); max-height:min(50vh, var(--reka-popover-content-available-height, 50vh)); min-width:0; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font-size:13px; line-height:1.7; user-select:text; overscroll-behavior:contain; }
</style>
