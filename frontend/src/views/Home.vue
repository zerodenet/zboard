<template>
  <div class="landing-page">
    <section class="hero-section">
      <div class="hero-copy">
        <span class="hero-kicker"><i></i> 稳定、清晰、随时可用</span>
        <h1>你的网络订阅，<br /><em>不该复杂。</em></h1>
        <p>{{ siteDescription }}</p>
        <div class="hero-actions">
          <RouterLink class="button hero-primary" :to="primaryPath">{{ primaryLabel }}<UiIcon name="chevron" /></RouterLink>
          <RouterLink class="button button-secondary hero-secondary" to="/pricing">查看套餐</RouterLink>
        </div>
        <ul class="hero-trust"><li><UiIcon name="check" />清晰的套餐规格</li><li><UiIcon name="check" />独立订阅链接</li><li><UiIcon name="check" />实时流量明细</li></ul>
      </div>
      <div class="hero-visual" aria-label="用户中心预览">
        <div class="preview-window">
          <div class="preview-top"><span><i></i><i></i><i></i></span><strong>{{ app.siteName }} · 用户中心</strong></div>
          <div class="preview-body">
            <div class="preview-welcome"><div><small>下午好</small><h2>欢迎回来</h2></div><span class="preview-avatar">U</span></div>
            <div class="preview-plan"><div class="preview-plan-head"><div><small>当前套餐</small><strong>个人标准版</strong></div><span>服务中</span></div><p><strong>72.6</strong> GB <small>/ 100 GB</small></p><div class="usage-track"><i style="width: 27.4%"></i></div><footer><span>本周期已使用 27.4 GB</span><span>剩余 18 天</span></footer></div>
            <div class="preview-stats"><article><UiIcon name="activity" /><span>今日使用<strong>1.24 GB</strong></span></article><article><UiIcon name="clock" /><span>到期时间<strong>2026/08/06</strong></span></article></div>
          </div>
        </div>
        <div class="floating-card"><span><UiIcon name="shield" /></span><div><strong>订阅配置已就绪</strong><small>安全凭证 · 随时轮换</small></div></div>
      </div>
    </section>

    <section class="value-strip"><p>一个清晰的账户空间，覆盖从购买到使用的完整过程</p><div><span>套餐选购</span><i></i><span>订单追踪</span><i></i><span>订阅交付</span><i></i><span>流量查看</span></div></section>

    <section class="feature-section">
      <div class="section-heading"><span>为日常使用而设计</span><h2>重要信息，一眼就懂</h2><p>不用理解节点、协议和后台术语，只关注与你有关的套餐、余额和连接。</p></div>
      <div class="feature-grid">
        <article><span class="feature-icon"><UiIcon name="plans" /></span><h3>套餐规格透明</h3><p>价格、流量、速度和设备数放在同一张卡片里，购买前没有隐藏信息。</p></article>
        <article><span class="feature-icon"><UiIcon name="key" /></span><h3>订阅链接可控</h3><p>生成、复制、轮换和吊销都由你自己完成，旧链接失效状态清晰可见。</p></article>
        <article><span class="feature-icon"><UiIcon name="activity" /></span><h3>流量去向清楚</h3><p>查看剩余流量、今日消耗和每条使用记录，不再只看到一个模糊数字。</p></article>
      </div>
    </section>

    <section class="landing-cta"><div><span>准备开始了吗？</span><h2>选择适合你的套餐，几分钟内完成配置。</h2></div><RouterLink class="button" :to="primaryPath">{{ primaryLabel }}<UiIcon name="chevron" /></RouterLink></section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchPublicSystemConfigs, type SystemConfig } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
const app = useAppStore()
const configs = ref<SystemConfig[]>([])
const siteDescription = computed(() => String(configs.value.find(item => item.config_key === 'site_desc')?.value || '从选择套餐、管理订单到获取订阅配置，在一个简单的账户空间里完成。'))
const primaryPath = computed(() => app.isAuthenticated ? (app.isAdmin ? '/admin/dashboard' : '/account') : (app.installation?.allow_registration ? '/register' : '/login'))
const primaryLabel = computed(() => app.isAuthenticated ? (app.isAdmin ? '进入管理后台' : '进入用户中心') : (app.installation?.allow_registration ? '免费注册' : '立即登录'))
onMounted(async () => { try { configs.value = await fetchPublicSystemConfigs() } catch { configs.value = [] } })
</script>
