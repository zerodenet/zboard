<template>
  <div class="admin-navigation">
    <nav class="domain-list" aria-label="管理端业务分区">
      <section v-for="domain in adminNavigation" :key="domain.id" class="domain-group" :class="{ selected: domain.id === currentDomain.id, expanded: domain.id === expandedDomainId }">
        <button type="button" class="domain-link"
          :aria-label="domain.label" :aria-expanded="domain.id === expandedDomainId"
          :aria-controls="domain.id === expandedDomainId ? `domain-pages-${domain.id}` : undefined"
          @click="toggleDomain(domain.id)">
          <UiIcon :name="domain.icon" /><span>{{ domain.label }}</span><UiIcon name="chevron" class="domain-chevron" />
        </button>
        <Transition name="domain-reveal">
        <div v-if="domain.id === expandedDomainId" :id="`domain-pages-${domain.id}`" class="domain-panel">
          <nav class="domain-pages" :aria-label="`${domain.label}页面`">
            <section v-for="section in domain.sections" :key="section.label" class="page-section">
              <RouterLink :to="section.pages[0].to"
                class="page-link" :class="{ selected: currentSection?.label === section.label }"
                :aria-current="currentSection?.label === section.label ? 'page' : undefined"
                @click="$emit('selectPage')"><UiIcon :name="section.icon" />{{ section.label }}</RouterLink>
            </section>
            <section class="page-section"><PluginNavigation surface="admin" /></section>
          </nav>
        </div>
        </Transition>
      </section>
    </nav>
  </div>
</template>

<script setup lang="ts">
import PluginNavigation from '../plugins/PluginNavigation.vue'
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import UiIcon from './UiIcon.vue'
import { adminNavigation, resolveAdminNavigation } from '../utils/adminNavigation'

defineEmits<{ selectPage: [] }>()
const route = useRoute()
const current = computed(() => resolveAdminNavigation(route.path))
const currentDomain = computed(() => current.value?.domain || adminNavigation[0])
const currentSection = computed(() => current.value?.section)
const expandedDomainId = ref<string | null>(currentDomain.value.id)
function toggleDomain(id: string) { expandedDomainId.value = expandedDomainId.value === id ? null : id }
watch(() => route.path, () => { expandedDomainId.value = currentDomain.value.id })
</script>

<style scoped>
.admin-navigation { min-height: 0; flex: 1; overflow-y: auto; overscroll-behavior: contain; padding: 14px 9px 18px; }
.domain-list { display: grid; gap: 3px; }
.domain-group { min-width: 0; }
.domain-link { width: 100%; min-height: 38px; display: flex; align-items: center; gap: 11px; padding: 0 11px; border: 0; border-radius: 6px; background: transparent; color: var(--text-secondary); cursor: pointer; font: inherit; font-size: 13px; font-weight: 500; text-align: left; transition: background-color .15s, color .15s; }
.domain-link > .ui-icon:first-child { width: 18px; flex: none; color: var(--sidebar-icon); font-size: 17px; text-align: center; }
.domain-link .domain-chevron { width: 13px; margin-left: auto; color: var(--muted-foreground); transform: rotate(90deg); transition: transform .18s ease-out; }
.domain-group.expanded .domain-chevron { transform: rotate(-90deg); }
.domain-link:hover, .page-link:hover { background: var(--muted-surface); color: var(--foreground); }
.domain-group.selected > .domain-link { color: var(--foreground); font-weight: 600; }
.domain-group.expanded > .domain-link { background: var(--sidebar-active); }
.domain-group.selected > .domain-link > .ui-icon:first-child { color: var(--foreground); }
.domain-panel { min-width: 0; margin: 4px 0 12px 20px; padding-left: 10px; border-left: 1px solid var(--border); }
.domain-reveal-enter-active, .domain-reveal-leave-active { overflow: hidden; transition: opacity .16s ease-out, transform .16s ease-out, max-height .18s ease-out, margin .18s ease-out; }
.domain-reveal-enter-from, .domain-reveal-leave-to { max-height: 0; margin-top: 0; margin-bottom: 0; opacity: 0; transform: translateY(-4px); }
.domain-reveal-enter-to, .domain-reveal-leave-from { max-height: 640px; opacity: 1; transform: translateY(0); }
.domain-pages { min-width: 0; }
.page-section + .page-section { margin-top: 3px; }
.page-link { min-height: 34px; display: flex; align-items: center; gap: 9px; margin: 1px 0; padding: 5px 10px; border-radius: 5px; color: var(--text-secondary); font-size: 12px; line-height: 1.4; text-decoration: none; }
.page-link .ui-icon { width: 15px; height: 15px; color: var(--muted-foreground); }
.page-link.selected { color: var(--foreground); background: var(--sidebar-active); font-weight: 600; }
.page-link.selected .ui-icon { color: var(--foreground); }
.domain-link:focus-visible, .page-link:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { .domain-link, .domain-link .domain-chevron, .domain-reveal-enter-active, .domain-reveal-leave-active { transition: none; } }
</style>
