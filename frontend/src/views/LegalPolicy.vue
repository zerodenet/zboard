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
import { useRoute } from 'vue-router'
import { useAppStore } from '../stores/app'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables } from '../utils/legalContent'

type PolicyType = 'terms' | 'privacy' | 'refund'

const app = useAppStore()
const route = useRoute()
const profile = computed(() => app.siteProfile)
const policyType = computed(() => route.meta.policyType as PolicyType | undefined)
const title = computed(() => typeof route.meta.title === 'string' ? route.meta.title : '政策说明')
const content = computed(() => {
  if (policyType.value === 'terms') return profile.value.termsContent
  if (policyType.value === 'privacy') return profile.value.privacyContent
  if (policyType.value === 'refund') return profile.value.refundContent
  return ''
})
const remote = computed(() => isRemoteLegalContent(content.value))
const remoteUrl = computed(() => remote.value ? content.value.trim() : '')
const html = computed(() => renderSafeMarkdown(resolveLegalVariables(content.value, profile.value)))
</script>

<style scoped>
.legal-page { max-width: 900px; margin: 0 auto; padding: 40px 20px 72px; }
.legal-page__header { margin-bottom: 24px; }
.legal-page__content { line-height: 1.8; }
.legal-page__remote { display: grid; gap: 16px; }
.legal-page__remote iframe { width: 100%; min-height: 70vh; border: 1px solid var(--line); border-radius: 12px; }
</style>
