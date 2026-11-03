<template>
  <TooltipProvider :delay-duration="delayDuration">
    <TooltipRoot>
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
withDefaults(defineProps<{ text: string; delayDuration?: number }>(), { delayDuration: 350 })
const { forwardRef } = useForwardExpose()
</script>
