<template>
  <button class="admin-search-trigger" type="button" aria-label="搜索管理页面" aria-keyshortcuts="Meta+K Control+K" @click="open = true">
    <UiIcon name="search" /><span>搜索菜单和功能...</span><kbd>⌘ K</kbd>
  </button>
  <ModalDialog :open="open" title="搜索管理页面" description="输入页面或业务分区名称，快速跳转。" size="sm" @close="open = false">
    <div class="admin-quick-search">
      <div ref="searchField" class="admin-quick-search-field"><UiIcon name="search" /><UiInput v-model="query" type="search" aria-label="搜索管理页面" placeholder="搜索页面或业务分区" @keydown.down.prevent="focusResult(0)" /></div>
      <nav class="admin-quick-search-results" aria-label="搜索结果">
        <RouterLink v-for="(item, index) in matches" :key="item.to" :to="item.to" @click="open = false" @keydown.down.prevent="focusResult(index + 1)" @keydown.up.prevent="focusResult(index - 1)">
          <span>{{ item.label }}</span><small>{{ item.domain }}</small>
        </RouterLink>
        <p v-if="!matches.length">没有匹配的管理页面</p>
      </nav>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { adminNavigation } from '../utils/adminNavigation'
import ModalDialog from './ModalDialog.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'

const route = useRoute()
const open = ref(false)
const query = ref('')
const searchField = ref<HTMLElement | null>(null)
function focusSearch() { searchField.value?.querySelector('input')?.focus() }
const pages = adminNavigation.flatMap(domain => domain.sections.flatMap(section =>
  section.pages.map(page => ({ ...page, domain: domain.label, section: section.label })),
))
const matches = computed(() => {
  const term = query.value.trim().toLocaleLowerCase()
  return (term ? pages.filter(page => `${page.label} ${page.domain} ${page.section}`.toLocaleLowerCase().includes(term)) : pages).slice(0, 12)
})

function focusResult(index: number) {
  const links = document.querySelectorAll<HTMLAnchorElement>('.admin-quick-search-results a')
  if (index < 0) { focusSearch(); return }
  links[Math.min(index, links.length - 1)]?.focus()
}
function handleShortcut(event: KeyboardEvent) {
  if (event.key.toLowerCase() !== 'k' || !(event.metaKey || event.ctrlKey)) return
  event.preventDefault()
  open.value = !open.value
}
watch(open, async value => {
  if (!value) { query.value = ''; return }
  await nextTick()
  focusSearch()
})
watch(() => route.fullPath, () => { open.value = false })
onMounted(() => document.addEventListener('keydown', handleShortcut))
onBeforeUnmount(() => document.removeEventListener('keydown', handleShortcut))
</script>

<style scoped>
.admin-search-trigger { min-width: 230px; height: 34px; display: inline-flex; align-items: center; gap: 9px; padding: 0 8px 0 11px; border: 1px solid var(--border); border-radius: 6px; color: var(--muted-foreground); background: var(--card); font-size: 12px; text-align: left; }
.admin-search-trigger:hover { border-color: var(--input); background: var(--surface-subtle); }
.admin-search-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.admin-search-trigger > span { flex: 1; }
.admin-search-trigger .ui-icon { font-size: 15px; }
.admin-search-trigger kbd { padding: 2px 5px; border: 1px solid var(--border); border-radius: 4px; background: var(--surface-subtle); font: inherit; font-size: 10px; }
.admin-quick-search { display: grid; gap: 10px; }
.admin-quick-search-field { height: 40px; display: flex; align-items: center; gap: 9px; padding: 0 11px; border: 1px solid var(--input); border-radius: 6px; }
.admin-quick-search-field:focus-within { border-color: var(--ring); box-shadow: 0 0 0 3px var(--focus-ring); }
.admin-quick-search-field :deep(.ui-input) { width: 100%; min-height: 0; padding: 0; border: 0; outline: 0; box-shadow: none; background: transparent; color: var(--foreground); font-size: 13px; }
.admin-quick-search-results { max-height: min(360px, 50vh); display: grid; gap: 2px; overflow-y: auto; }
.admin-quick-search-results a { min-height: 36px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 7px 10px; border-radius: 5px; color: var(--foreground); font-size: 13px; text-decoration: none; }
.admin-quick-search-results a:hover, .admin-quick-search-results a:focus-visible { outline: 0; background: var(--muted-surface); }
.admin-quick-search-results small { color: var(--muted-foreground); font-size: 11px; }
.admin-quick-search-results p { margin: 0; padding: 12px; color: var(--muted-foreground); font-size: 12px; }
@media (max-width: 820px) { .admin-search-trigger { min-width: 34px; width: 34px; padding: 0; justify-content: center; } .admin-search-trigger > span, .admin-search-trigger kbd { display: none; } }
</style>
