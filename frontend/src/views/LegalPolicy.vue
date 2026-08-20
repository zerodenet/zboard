<template>
  <section class="legal-page">
    <header class="legal-page__header">
      <h1>{{ title }}</h1>
    </header>
    <div v-if="remote" class="legal-page__remote">
      <a :href="remoteUrl" target="_blank" rel="noreferrer">在新窗口查看外部政策内容</a>
      <iframe :src="remoteUrl" :title="title" sandbox="allow-forms allow-popups allow-scripts" referrerpolicy="no-referrer" />
    </div>
    <div v-else class="legal-page__content" v-html="html" />
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables } from '../utils/legalContent'
import type { SiteProfile } from '../utils/siteProfile'

const props = defineProps<{ title: string; content: string; profile: SiteProfile }>()
const remote = computed(() => isRemoteLegalContent(props.content))
const remoteUrl = computed(() => remote.value ? props.content.trim() : '')
const html = computed(() => renderSafeMarkdown(resolveLegalVariables(props.content, props.profile)))
</script>

<style scoped>
.legal-page { max-width: 900px; margin: 0 auto; padding: 40px 20px 72px; }
.legal-page__header { margin-bottom: 24px; }
.legal-page__content { line-height: 1.8; }
.legal-page__remote { display: grid; gap: 16px; }
.legal-page__remote iframe { width: 100%; min-height: 70vh; border: 1px solid var(--line); border-radius: 12px; }
</style>
