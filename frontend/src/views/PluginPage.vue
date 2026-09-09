<template>
  <PluginFrame
    :plugin-id="String(route.params.pluginId)"
    :page-id="String(route.params.pageId)"
    :surface="surface"
  />
</template>
<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import PluginFrame from "../plugins/PluginFrame.vue";
import type { Surface } from "../api/plugins";
const route = useRoute();
const surface = computed<Surface>(() =>
  route.path.startsWith("/admin/")
    ? "admin"
    : route.path.startsWith("/account/")
      ? "account"
      : "public",
);
</script>
