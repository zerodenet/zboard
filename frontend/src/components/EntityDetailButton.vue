<template>
  <UiButton variant="link" size="sm" class="entity-detail-button"><slot /></UiButton>
</template>
<script setup lang="ts">
import UiButton from './UiButton.vue'
</script>
<style scoped>
.entity-detail-button.ui-button { display: inline; width: fit-content; max-width: 100%; height: auto; min-height: 0; padding: 0; color: var(--foreground); font-size: 13px; font-weight: 600; line-height: 1.5; white-space: normal; text-align: left; overflow-wrap: anywhere; }
</style>
