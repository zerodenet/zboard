<template>
  <div class="landing-page">
    <section class="storefront-hero">
      <div class="storefront-hero__copy">
        <span class="hero-kicker"><i></i> 灵活套餐 · 独立订阅 · 清晰计费</span>
        <h1>选择适合你的套餐，<em>按需订阅，轻松管理。</em></h1>
        <p>{{ siteDescription }}</p>
        <div class="hero-actions">
          <RouterLink class="button hero-primary" to="/pricing">浏览套餐<UiIcon name="chevron" /></RouterLink>
          <RouterLink class="button button-secondary hero-secondary" :to="secondaryPath">{{ secondaryLabel }}</RouterLink>
        </div>
        <ul class="hero-trust">
          <li><UiIcon name="check" />套餐权益与价格一目了然</li>
          <li><UiIcon name="check" />下单前确认全部费用</li>
          <li><UiIcon name="check" />每条订阅独立管理</li>
        </ul>
      </div>

      <div class="storefront-hero__visual" aria-label="套餐服务介绍">
        <div class="storefront-preview">
          <header>
            <span><i></i><i></i><i></i></span>
            <strong>{{ app.siteName }} · 套餐服务</strong>
          </header>
          <div class="storefront-preview__body">
            <div class="storefront-preview__title">
              <div><small>套餐服务</small><h2>按需求选择适合你的方案</h2></div>
              <span>灵活选择</span>
            </div>
            <p>根据流量、速度、设备数和服务周期选择方案，购买后可随时在用户中心查看和管理。</p>
            <div class="storefront-preview__features">
              <span>流量与速度</span>
              <span>设备数量</span>
              <span>灵活周期</span>
            </div>
            <RouterLink class="button" to="/pricing">浏览全部套餐<UiIcon name="chevron" /></RouterLink>
          </div>
        </div>
      </div>
    </section>

    <section class="value-strip">
      <p>从选择到使用，简单明了</p>
      <div><span>浏览套餐</span><i></i><span>选择规格</span><i></i><span>确认订单</span><i></i><span>管理订阅</span></div>
    </section>

    <section v-if="plans.length" class="storefront-home-plans">
      <div class="section-heading">
        <span>可用套餐</span>
        <h2>选择适合你的服务方案</h2>
        <p>按流量、速度、设备数与价格进行比较。</p>
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
      <div class="section-heading"><span>套餐权益</span><h2>你关心的信息，一目了然</h2></div>
      <div class="feature-grid">
        <article><span class="feature-icon"><UiIcon name="plans" /></span><h3>按需选择</h3><p>根据流量、速度和设备数选择适合当前需求的套餐。</p></article>
        <article><span class="feature-icon"><UiIcon name="check" /></span><h3>灵活周期</h3><p>按可用的服务周期和价格选择适合的规格。</p></article>
        <article><span class="feature-icon"><UiIcon name="activity" /></span><h3>独立管理</h3><p>每条订阅独立生效，续费、切换和流量管理互不干扰。</p></article>
      </div>
    </section>

    <section class="landing-cta">
      <div><span>立即开始</span><h2>找到适合你的套餐，开始使用。</h2></div>
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

const siteDescription = computed(() => String(configs.value.find(item => item.config_key === 'site_desc')?.value || '按流量、速度和设备数选择服务方案，购买后可在用户中心独立管理每一条订阅。'))
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
