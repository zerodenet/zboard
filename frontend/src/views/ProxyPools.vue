<template>
  <section class="standard-page">
    <PageHeader title="共享代理池" />
    <UiSection variant="outlined"><NodeProxyPools /></UiSection>
  </section>
</template>
<script setup lang="ts">
import UiSection from '../components/UiSection.vue'
import PageHeader from '../components/PageHeader.vue'
import NodeProxyPools from '../components/NodeProxyPools.vue'
</script>
