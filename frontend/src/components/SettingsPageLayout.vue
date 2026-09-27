<template>
  <div class="settings-page-layout">
    <AdminPageNavigation orientation="vertical" />
    <div class="settings-page-content"><slot /></div>
  </div>
</template>

<script setup lang="ts">
import AdminPageNavigation from './AdminPageNavigation.vue'
</script>
