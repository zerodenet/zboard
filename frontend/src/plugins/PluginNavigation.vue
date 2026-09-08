<template>
  <RouterLink
    v-for="item in pages.data.value"
    :key="`${item.plugin_id}:${item.page.id}`"
    :to="pluginPagePath(surface, item.plugin_id, item.page.id)"
    class="plugin-nav-link"
    >{{ item.page.title }}</RouterLink
  >
</template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, watch } from "vue";
import { useAppStore } from "../stores/app";
import { useRemoteResource } from "../composables/useRemoteResource";
import {
  fetchPluginPages,
  pluginPagePath,
  type CatalogPage,
  type Surface,
} from "../api/plugins";
const props = defineProps<{ surface: Surface }>();
const app = useAppStore();
const pages = useRemoteResource<CatalogPage[]>({
  initial: () => [],
  fetch: ({ signal }) => fetchPluginPages(props.surface, signal),
  errorMessage: "扩展入口暂不可用",
});
let timer: ReturnType<typeof setInterval> | undefined;
function refresh() {
  if (!document.hidden) void pages.load();
}
watch(
  () => app.token,
  () => {
    pages.reset();
    refresh();
  },
);
onMounted(() => {
  refresh();
  timer = setInterval(refresh, 15_000);
  window.addEventListener("focus", refresh);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
  window.removeEventListener("focus", refresh);
});
</script>
<style scoped>
.plugin-nav-link {
  display: inline-flex;
  padding: 8px 10px;
  color: var(--text-body);
  text-decoration: none;
  border-radius: 8px;
}
.plugin-nav-link:hover {
  background: var(--surface-soft);
}
</style>
