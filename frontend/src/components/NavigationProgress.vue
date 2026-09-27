<template>
  <div class="navigation-progress" :class="{ 'navigation-progress--visible': visible }" aria-hidden="true">
    <span :style="{ transform: `scaleX(${value / 100})` }" />
  </div>
</template>

<script setup lang="ts">
import { navigationProgress, navigationProgressVisible } from '../composables/navigationProgress'

const value = navigationProgress
const visible = navigationProgressVisible
</script>

<style scoped>
.navigation-progress { position: fixed; top: 0; left: 0; z-index: 1050; width: 100%; height: 2px; opacity: 0; pointer-events: none; transition: opacity .16s ease-out; }
.navigation-progress--visible { opacity: 1; }
.navigation-progress span { display: block; width: 100%; height: 100%; transform-origin: left center; background: var(--primary); box-shadow: 0 1px 5px color-mix(in srgb, var(--primary) 24%, transparent); transition: transform .18s ease-out; }
@media (prefers-reduced-motion: reduce) { .navigation-progress, .navigation-progress span { transition: none; } }
</style>
