<template>
  <TooltipProvider :delay-duration="delayDuration">
    <TooltipRoot :open="open" @update:open="$emit('update:open', $event)">
      <TooltipTrigger :ref="forwardRef" as-child v-bind="$attrs"><slot /></TooltipTrigger>
      <TooltipPortal>
        <TooltipContent class="ui-tooltip-content" :side-offset="6" :collision-padding="16"><slot name="content">{{ text }}</slot></TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { TooltipContent, TooltipPortal, TooltipProvider, TooltipRoot, TooltipTrigger, useForwardExpose } from 'reka-ui'
defineOptions({ inheritAttrs: false })
defineEmits<{ 'update:open': [value: boolean] }>()
withDefaults(defineProps<{ text: string; delayDuration?: number; open?: boolean }>(), { delayDuration: 350, open: undefined })
const { forwardRef } = useForwardExpose()
</script>
