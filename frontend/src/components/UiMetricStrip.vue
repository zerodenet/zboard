<template>
  <div class="metric-grid ui-metric-strip" :style="{ '--metric-columns': columns }">
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ columns?: number }>(), { columns: 4 })
</script>
