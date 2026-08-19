<template>
  <div v-if="hasPolicies" class="auth-legal-links">
    <span>{{ context === 'register' ? '创建账户前请阅读' : '站点政策' }}</span>
    <button v-if="profile.termsContent" type="button" @click="openPolicy('服务条款', profile.termsContent)">服务条款</button>
    <i v-if="profile.termsContent && profile.privacyContent">·</i>
    <button v-if="profile.privacyContent" type="button" @click="openPolicy('隐私政策', profile.privacyContent)">隐私政策</button>
  </div>

  <LegalContentDialog
    :open="Boolean(activeContent)"
    :title="activeTitle"
    :content="activeContent"
    :profile="profile"
    @close="closePolicy"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '../stores/app'
import LegalContentDialog from './LegalContentDialog.vue'

withDefaults(defineProps<{ context?: 'register' | 'login' }>(), { context: 'login' })

const app = useAppStore()
const activeTitle = ref('')
const activeContent = ref('')
const profile = computed(() => app.siteProfile)
const hasPolicies = computed(() => Boolean(profile.value.termsContent || profile.value.privacyContent))

function openPolicy(title: string, content: string) {
  activeTitle.value = title
  activeContent.value = content
}

function closePolicy() {
  activeContent.value = ''
  activeTitle.value = ''
}
</script>

<style scoped>
.auth-legal-links { display: flex; align-items: center; justify-content: center; flex-wrap: wrap; gap: 4px; color: var(--muted); font-size: 10px; line-height: 1.5; text-align: center; }
.auth-legal-links button { padding: 0; border: 0; background: none; color: var(--primary); font: inherit; cursor: pointer; text-decoration: underline; text-underline-offset: 2px; }
.auth-legal-links button:hover { text-decoration-thickness: 2px; }
.auth-legal-links i { font-style: normal; opacity: .65; }
</style>
