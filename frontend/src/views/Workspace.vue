<template>
  <section class="standard-page workspace-page">
    <PageHeader :title="group?.label || '系统设置'" :show-section-navigation="false" />
    <div v-if="group" class="workspace-sections">
      <UiSection v-for="section in sections" :key="section.id" :title="section.label" variant="outlined">
        <nav class="workspace-links" :aria-label="section.label || group.label">
          <RouterLink v-for="page in section.pages" :key="page.id" :to="page.path"><span>{{ page.label }}</span><UiIcon name="chevron" /></RouterLink>
        </nav>
      </UiSection>
    </div>
    <EmptyState v-else icon="settings" title="此工作区不可用" description="刷新导航或返回有权访问的页面。" />
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { navigationState } from '../stores/navigation'
import PageHeader from '../components/PageHeader.vue'
import UiSection from '../components/UiSection.vue'
import UiIcon from '../components/UiIcon.vue'
import EmptyState from '../components/EmptyState.vue'
import { descendantPages } from '../utils/navigationTree'
const route = useRoute()
const nodes = computed(() => navigationState.admin.snapshot.value.nodes)
const group = computed(() => nodes.value.find(node => node.id === route.params.groupId && !node.path))
const sections = computed(() => {
  if (!group.value) return []
  const children = nodes.value.filter(node => node.parent_id === group.value!.id)
  return [
    { id: group.value.id, label: '', pages: children.filter(node => node.path) },
    ...children.filter(node => !node.path).map(node => ({ id: node.id, label: node.label, pages: descendantPages(nodes.value, node.id) })),
  ].filter(section => section.pages.length)
})
</script>
<style scoped>
.workspace-sections { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); align-items: start; gap: 20px; max-width: 1100px; }
.workspace-links { display: grid; }
.workspace-links a { display: flex; align-items: center; gap: 12px; min-height: 52px; padding: 12px 0; color: var(--foreground); font-size: 14px; text-decoration: none; }
.workspace-links a + a { border-top: 1px solid var(--border); }
.workspace-links a > .ui-icon { flex: none; width: 16px; color: var(--muted-foreground); }
.workspace-links a > span { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.workspace-links a:hover span { text-decoration: underline; text-underline-offset: 3px; }
.workspace-links a:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; border-radius: 4px; }
@media(max-width:700px) { .workspace-sections { grid-template-columns: 1fr; } .workspace-links a { min-height: 48px; } }
</style>
