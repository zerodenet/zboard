<template><span class="status-badge" :data-tone="tone"><span class="status-dot"></span><slot /></span></template>

<script setup lang="ts">
withDefaults(defineProps<{ tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info' }>(), { tone: 'neutral' })
</script>
