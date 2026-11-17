<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface SkeletonProps {
  class?: HTMLAttributes['class']
  as?: 'div' | 'span'
}

const props = withDefaults(defineProps<SkeletonProps>(), { as: 'div' })
</script>

<template>
  <component
    :is="as"
    data-slot="skeleton"
    :class="cn('bg-muted rounded-md animate-pulse', props.class)"
  />
</template>
