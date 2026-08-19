<template>
  <div class="public-shell">
    <header class="public-header">
      <RouterLink class="public-brand" to="/">
        <img v-if="profile.logo" class="site-logo" :src="profile.logo" :alt="profile.name" />
        <span v-else class="brand-mark">{{ brandInitial }}</span>
        <strong>{{ profile.name }}</strong>
      </RouterLink>
      <UiButton variant="ghost" icon class="icon-button public-menu" type="button" :aria-label="menuOpen ? '关闭站点导航' : '打开站点导航'" :aria-expanded="menuOpen" aria-controls="public-navigation" @click="menuOpen = !menuOpen"><UiIcon :name="menuOpen ? 'close' : 'menu'" /></UiButton>
      <nav id="public-navigation" :class="{ open: menuOpen }" aria-label="站点导航"><RouterLink to="/" @click="menuOpen = false">首页</RouterLink><RouterLink to="/pricing" @click="menuOpen = false">套餐</RouterLink></nav>
      <div class="public-actions">
        <RouterLink v-if="app.isAuthenticated" class="button button-secondary button-sm" :to="landingPath">进入{{ app.isAdmin ? '管理后台' : '用户中心' }}</RouterLink>
        <template v-else><RouterLink class="button button-ghost button-sm" to="/login">登录</RouterLink><RouterLink v-if="app.installation?.allow_registration" class="button button-sm" to="/register">免费注册</RouterLink></template>
      </div>
    </header>
    <main><RouterView /></main>
    <footer class="public-footer">
      <div class="public-footer__identity">
        <RouterLink class="public-brand" to="/">
          <img v-if="footerLogo" class="site-logo" :src="footerLogo" :alt="profile.name" />
          <span v-else class="brand-mark">{{ brandInitial }}</span>
          <strong>{{ profile.name }}</strong>
        </RouterLink>
        <p>{{ profile.description }}</p>
        <small>{{ profile.copyright }}</small>
      </div>
      <div class="public-footer__links">
        <RouterLink to="/pricing">套餐</RouterLink>
        <RouterLink v-if="app.isAuthenticated" :to="landingPath">进入{{ app.isAdmin ? '管理后台' : '用户中心' }}</RouterLink>
        <RouterLink v-else to="/login">登录</RouterLink>
        <a v-if="profile.supportUrl" :href="profile.supportUrl" target="_blank" rel="noreferrer">客服</a>
        <a v-if="profile.supportEmail" :href="`mailto:${profile.supportEmail}`">联系邮箱</a>
        <a v-if="profile.telegramUrl" :href="profile.telegramUrl" target="_blank" rel="noreferrer">Telegram</a>
        <button v-if="profile.termsContent" type="button" class="footer-link-button" @click="openPolicy('服务条款', profile.termsContent)">服务条款</button>
        <button v-if="profile.privacyContent" type="button" class="footer-link-button" @click="openPolicy('隐私政策', profile.privacyContent)">隐私政策</button>
        <button v-if="profile.refundContent" type="button" class="footer-link-button" @click="openPolicy('退款政策', profile.refundContent)">退款政策</button>
        <template v-for="item in profile.legalItems" :key="`${item.label}:${item.value}`">
          <a v-if="item.url" :href="item.url" target="_blank" rel="noreferrer">{{ item.label }} · {{ item.value }}</a>
          <span v-else>{{ item.label }} · {{ item.value }}</span>
        </template>
      </div>
    </footer>

    <LegalContentDialog
      :open="Boolean(activePolicy)"
      :title="activePolicy?.title || ''"
      :content="activePolicy?.content || ''"
      :profile="profile"
      @close="activePolicy = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import LegalContentDialog from '../components/LegalContentDialog.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const menuOpen = ref(false)
const activePolicy = ref<{ title: string; content: string } | null>(null)
const landingPath = computed(() => app.isAdmin ? '/admin/dashboard' : '/account')
const profile = computed(() => app.siteProfile)
const footerLogo = computed(() => profile.value.logoDark || profile.value.logo)
const brandInitial = computed(() => Array.from(profile.value.name.trim())[0]?.toUpperCase() || 'Z')

function openPolicy(title: string, content: string) {
  activePolicy.value = { title, content }
}
</script>

<style scoped>
.site-logo { display: block; width: auto; max-width: 160px; height: 34px; object-fit: contain; }
.public-footer__identity { min-width: min(360px, 100%); }
.public-footer__identity small { display: block; margin-top: 12px; color: var(--public-footer-muted); font-size: 11px; }
.public-footer__links { max-width: 650px; justify-content: flex-end; flex-wrap: wrap; row-gap: 12px; }
.public-footer__links a, .public-footer__links span, .footer-link-button { color: var(--public-footer-muted); }
.public-footer__links a:hover, .footer-link-button:hover { color: var(--text-inverse); }
.footer-link-button { padding: 0; border: 0; background: transparent; font: inherit; cursor: pointer; }
@media (max-width: 700px) { .public-footer__links { justify-content: flex-start; } }
</style>
