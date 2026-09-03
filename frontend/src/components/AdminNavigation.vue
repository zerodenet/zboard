<template>
  <div class="admin-navigation">
    <nav class="domain-rail" aria-label="管理端业务分区">
      <button v-for="domain in adminNavigation" :key="domain.id" type="button"
        class="domain-link" :class="{ selected: domain.id === currentDomain.id }"
        :aria-label="domain.label" :title="domain.label"
        :aria-current="domain.id === currentDomain.id ? 'true' : undefined"
        @click="domain.id !== currentDomain.id && $emit('selectDomain', domain.sections[0].pages[0].to)">
        <UiIcon :name="domain.icon" /><span>{{ domain.shortLabel }}</span>
      </button>
    </nav>
    <div class="domain-panel">
      <div class="domain-heading"><h2>{{ currentDomain.label }}</h2><p>{{ currentDomain.description }}</p></div>
      <nav class="domain-pages" :aria-label="`${currentDomain.label}页面`">
        <section v-for="section in currentDomain.sections" :key="section.label" class="page-section">
          <h3>{{ section.label }}</h3>
          <RouterLink v-for="page in section.pages" :key="page.to" :to="page.to"
            class="page-link" :class="{ selected: currentPage?.to === page.to }"
            :aria-current="currentPage?.to === page.to ? 'page' : undefined"
            @click="$emit('selectPage')">
            <span>{{ page.label }}</span><span v-if="currentPage?.to === page.to" class="current-marker" aria-hidden="true" />
          </RouterLink>
        </section>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import UiIcon from './UiIcon.vue'
import { adminNavigation, resolveAdminNavigation } from '../utils/adminNavigation'

defineEmits<{ selectDomain: [path: string]; selectPage: [] }>()
const route = useRoute()
const current = computed(() => resolveAdminNavigation(route.path))
const currentDomain = computed(() => current.value?.domain || adminNavigation[0])
const currentPage = computed(() => current.value?.page)
</script>

<style scoped>
.admin-navigation { min-height: 0; flex: 1; display: grid; grid-template-columns: 72px minmax(0, 1fr); }
.domain-rail { display: flex; flex-direction: column; gap: 8px; padding: 16px 8px; overflow-y: auto; overscroll-behavior: contain; background: var(--surface-soft); border-right: 1px solid var(--line-subtle); }
.domain-link { position: relative; flex: 0 0 auto; width: 100%; min-height: 60px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 7px; border: 1px solid transparent; border-radius: 10px; background: transparent; color: var(--text-secondary); cursor: pointer; font: inherit; font-size: 12px; font-weight: 550; transition: background .15s, color .15s; }
.domain-link .ui-icon { font-size: 19px; }
.domain-link:hover { background: var(--surface-muted); color: var(--text-strong); }
.domain-link.selected { color: var(--primary-strong); background: var(--primary-soft); border-color: var(--primary-border); font-weight: 700; }
.domain-panel { min-width: 0; min-height: 0; display: flex; flex-direction: column; background: var(--surface); }
.domain-heading { flex-shrink: 0; padding: 23px 18px 19px; }
.domain-heading h2 { margin: 0; color: var(--text-strong); font-size: 17px; letter-spacing: -.02em; }
.domain-heading p { margin: 7px 0 0; color: var(--muted); font-size: 11px; line-height: 1.6; }
.domain-pages { min-height: 0; overflow-y: auto; overscroll-behavior: contain; padding: 0 10px 20px; }
.page-section + .page-section { margin-top: 22px; }
.page-section h3 { margin: 0 8px 8px; color: var(--muted); font-size: 11px; font-weight: 500; }
.page-link { display: flex; align-items: center; justify-content: space-between; gap: 6px; min-height: 40px; margin: 3px 0; padding: 8px 10px; border-radius: 7px; color: var(--text-body); font-size: 13px; line-height: 1.5; text-decoration: none; }
.page-link:hover { color: var(--text-strong); background: var(--surface-soft); }
.page-link.selected { color: var(--primary-strong); background: var(--primary-soft); font-weight: 650; }
.current-marker { flex: 0 0 auto; width: 5px; height: 5px; border-radius: 50%; background: currentColor; }
.domain-link:focus-visible, .page-link:focus-visible { outline: 2px solid var(--focus-border); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { .domain-link { transition: none; } }
</style>
