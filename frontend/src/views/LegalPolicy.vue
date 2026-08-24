<template>
  <div class="policy-library">
    <aside class="policy-library__nav" aria-label="政策文档导航">
      <span>文档中心</span>
      <h2>政策与规则</h2>
      <nav>
        <RouterLink v-for="item in documents" :key="item.slug" :to="`/docs/${item.slug}`">
          <strong>{{ item.title }}</strong>
          <small v-if="item.summary">{{ item.summary }}</small>
        </RouterLink>
      </nav>
    </aside>

    <article v-if="document" class="legal-page">
      <header class="legal-page__header">
        <span>Policies</span>
        <h1>{{ document.title }}</h1>
        <p v-if="document.summary">{{ document.summary }}</p>
      </header>
      <div v-if="remote" class="legal-page__remote">
        <div><span>内容来自外部页面</span><a :href="remoteUrl" target="_blank" rel="noreferrer">在新窗口打开</a></div>
        <iframe :src="remoteUrl" :title="document.title" sandbox="allow-forms allow-popups allow-scripts" referrerpolicy="no-referrer" />
      </div>
      <div v-else class="legal-page__content" v-html="html" />
    </article>

    <article v-else class="legal-page legal-page--missing">
      <span>404</span>
      <h1>没有找到这份政策文档</h1>
      <p>该文档可能已下线或链接地址已经调整。</p>
      <RouterLink to="/">返回首页</RouterLink>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '../stores/app'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables, stripLeadingLegalTitle } from '../utils/legalContent'

const app = useAppStore()
const route = useRoute()
const profile = computed(() => app.siteProfile)
const documents = computed(() => profile.value.policyDocuments.filter(item => item.published))
const slug = computed(() => String(route.params.slug || '').trim().toLowerCase())
const document = computed(() => (slug.value ? documents.value.find(item => item.slug === slug.value) : documents.value[0]) || null)
const content = computed(() => document.value?.content || '')
const remote = computed(() => isRemoteLegalContent(content.value))
const remoteUrl = computed(() => remote.value ? content.value.trim() : '')
const html = computed(() => {
  if (!document.value) return ''
  const source = stripLeadingLegalTitle(content.value, document.value.title)
  return renderSafeMarkdown(resolveLegalVariables(source, profile.value))
})
</script>

<style scoped>
.policy-library { width: min(1180px, calc(100% - 40px)); min-height: calc(100vh - 220px); display: grid; grid-template-columns: 260px minmax(0, 1fr); gap: 42px; margin: 0 auto; padding: 48px 0 88px; }
.policy-library__nav { position: sticky; top: 104px; align-self: start; display: grid; gap: 6px; }.policy-library__nav > span { color: var(--primary); font-size: 10px; font-weight: 750; letter-spacing: .1em; text-transform: uppercase; }.policy-library__nav h2 { margin: 0 0 14px; font-size: 22px; }.policy-library__nav nav { display: grid; gap: 6px; }.policy-library__nav a { display: grid; gap: 4px; padding: 11px 12px; border: 1px solid transparent; border-radius: 9px; color: var(--text); text-decoration: none; }.policy-library__nav a:hover,.policy-library__nav a.router-link-exact-active { border-color: var(--line); background: var(--surface-soft); }.policy-library__nav strong { font-size: 12px; }.policy-library__nav small { color: var(--muted); font-size: 9px; line-height: 1.45; }
.legal-page { min-width: 0; padding: 34px 40px 52px; border: 1px solid var(--line); border-radius: 16px; background: var(--surface); box-shadow: var(--shadow-sm); }.legal-page__header { margin-bottom: 30px; padding-bottom: 24px; border-bottom: 1px solid var(--line); }.legal-page__header span { color: var(--primary); font-size: 10px; font-weight: 750; letter-spacing: .1em; text-transform: uppercase; }.legal-page__header h1 { margin: 8px 0 0; font-size: 34px; line-height: 1.25; }.legal-page__header p { max-width: 700px; margin: 10px 0 0; color: var(--muted); font-size: 13px; line-height: 1.65; }.legal-page__content { max-width: 800px; color: var(--text); font-size: 14px; line-height: 1.85; }.legal-page__content :deep(h1) { margin: 0 0 24px; font-size: 28px; }.legal-page__content :deep(h2) { margin: 30px 0 10px; font-size: 19px; }.legal-page__content :deep(h3) { margin: 22px 0 8px; font-size: 16px; }.legal-page__content :deep(p),.legal-page__content :deep(ul),.legal-page__content :deep(ol),.legal-page__content :deep(blockquote) { margin: 11px 0; }.legal-page__content :deep(a) { color: var(--primary); }.legal-page__remote { display: grid; gap: 14px; }.legal-page__remote > div { display: flex; justify-content: space-between; gap: 12px; color: var(--muted); font-size: 11px; }.legal-page__remote a { color: var(--primary); }.legal-page__remote iframe { width: 100%; min-height: 70vh; border: 1px solid var(--line); border-radius: 12px; }.legal-page--missing { align-content: center; min-height: 440px; }.legal-page--missing > span { color: var(--primary); font-weight: 800; }.legal-page--missing h1 { margin: 10px 0; }.legal-page--missing p { color: var(--muted); }.legal-page--missing a { width: fit-content; color: var(--primary); }
@media (max-width: 780px) { .policy-library { width: min(100% - 28px, 720px); grid-template-columns: 1fr; gap: 18px; padding-top: 28px; }.policy-library__nav { position: static; }.policy-library__nav nav { display: flex; overflow-x: auto; }.policy-library__nav a { min-width: 170px; }.legal-page { padding: 26px 20px 40px; }.legal-page__header h1 { font-size: 28px; } }
</style>
