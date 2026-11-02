<template>
  <TooltipProvider :delay-duration="350">
    <TooltipRoot>
      <TooltipTrigger :ref="forwardRef" as-child v-bind="$attrs"><slot /></TooltipTrigger>
      <TooltipPortal>
        <TooltipContent class="ui-tooltip-content" :side-offset="6">{{ text }}</TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { TooltipContent, TooltipPortal, TooltipProvider, TooltipRoot, TooltipTrigger, useForwardExpose } from 'reka-ui'
defineOptions({ inheritAttrs: false })
defineProps<{ text: string }>()
const { forwardRef } = useForwardExpose()
</script>
