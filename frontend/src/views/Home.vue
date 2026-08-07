<template>
  <div class="landing-page">
    <section class="storefront-hero">
      <div class="storefront-hero__copy">
        <span class="hero-kicker"><i></i> 套餐、订单与订阅统一管理</span>
        <h1>从选择套餐到开始使用，<em>一步一步完成。</em></h1>
        <p>{{ siteDescription }}</p>
        <div class="hero-actions">
          <RouterLink class="button hero-primary" to="/pricing">浏览套餐<UiIcon name="chevron" /></RouterLink>
          <RouterLink class="button button-secondary hero-secondary" :to="secondaryPath">{{ secondaryLabel }}</RouterLink>
        </div>
        <ul class="hero-trust">
          <li><UiIcon name="check" />规格与价格清晰展示</li>
          <li><UiIcon name="check" />购买前完整确认订单</li>
          <li><UiIcon name="check" />每条订阅独立管理</li>
        </ul>
      </div>

      <div class="storefront-hero__visual" aria-label="套餐选购流程介绍">
        <div class="storefront-preview">
          <header>
            <span><i></i><i></i><i></i></span>
            <strong>{{ app.siteName }} · 购买流程</strong>
          </header>
          <div class="storefront-preview__body">
            <div class="storefront-preview__title">
              <div><small>选择套餐</small><h2>找到适合你的服务方案</h2></div>
              <span>简单清晰</span>
            </div>
            <p>浏览套餐权益与可用规格，确认订单后，即可在用户中心独立管理每一条订阅。</p>
            <div class="storefront-preview__features">
              <span>比较套餐权益</span>
              <span>选择可用规格</span>
              <span>下单前完整确认</span>
            </div>
            <RouterLink class="button" to="/pricing">浏览全部套餐<UiIcon name="chevron" /></RouterLink>
          </div>
        </div>
      </div>
    </section>

    <section class="value-strip">
      <p>一个连续的购买与服务流程</p>
      <div><span>浏览套餐</span><i></i><span>选择规格</span><i></i><span>确认订单</span><i></i><span>管理订阅</span></div>
    </section>

    <section v-if="plans.length" class="storefront-home-plans">
      <div class="section-heading">
        <span>套餐推荐</span>
        <h2>从清晰的套餐卡片开始</h2>
        <p>比较流量、速度与设备数，找到适合自己的套餐。</p>
      </div>
      <div class="commerce-catalog-grid">
        <CommercePlanCard
          v-for="plan in plans"
          :key="plan.id"
          :plan="plan"
          :offer="plan.primary_sku || null"
          :offer-count="plan.active_sku_count"
          @select="openPlan(plan.id)"
        />
      </div>
      <RouterLink class="storefront-home-plans__more" to="/pricing">查看全部套餐<UiIcon name="chevron" /></RouterLink>
    </section>

    <section class="feature-section">
      <div class="section-heading"><span>为购买与使用而设计</span><h2>重要信息保持在正确的位置</h2></div>
      <div class="feature-grid">
        <article><span class="feature-icon"><UiIcon name="plans" /></span><h3>核心权益清晰</h3><p>流量、速度、设备数和起始价格集中展示。</p></article>
        <article><span class="feature-icon"><UiIcon name="check" /></span><h3>规格选择完整</h3><p>月付、季付、年付等可用规格在详情中统一比较。</p></article>
        <article><span class="feature-icon"><UiIcon name="activity" /></span><h3>订单确认明确</h3><p>创建订单前核对套餐、规格、服务周期和应付金额。</p></article>
      </div>
    </section>

    <section class="landing-cta">
      <div><span>开始选购</span><h2>选择适合你的套餐，在确认无误后创建订单。</h2></div>
      <RouterLink class="button" to="/pricing">查看套餐<UiIcon name="chevron" /></RouterLink>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { fetchPlanCatalogPage, fetchPublicSystemConfigs, type PlanCatalogItem, type SystemConfig } from '../api/client'
import CommercePlanCard from '../components/CommercePlanCard.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const router = useRouter()
const configs = ref<SystemConfig[]>([])
const plans = ref<PlanCatalogItem[]>([])

const siteDescription = computed(() => String(configs.value.find(item => item.config_key === 'site_desc')?.value || '浏览套餐、选择规格、确认订单，再在用户中心管理每一条独立订阅。'))
const secondaryPath = computed(() => app.isAuthenticated ? (app.isAdmin ? '/admin/dashboard' : '/account') : (app.installation?.allow_registration ? '/register' : '/login'))
const secondaryLabel = computed(() => app.isAuthenticated ? (app.isAdmin ? '进入管理后台' : '进入用户中心') : (app.installation?.allow_registration ? '注册账户' : '立即登录'))

function openPlan(id: number) {
  router.push({ path: '/pricing', query: { plan: String(id) } })
}

onMounted(async () => {
  const [configResult, planResult] = await Promise.allSettled([
    fetchPublicSystemConfigs(),
    fetchPlanCatalogPage({ offset: 0, limit: 3 }),
  ])
  configs.value = configResult.status === 'fulfilled' ? configResult.value : []
  plans.value = planResult.status === 'fulfilled' ? planResult.value.items : []
})
</script>
