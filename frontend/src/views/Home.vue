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

      <div class="storefront-hero__visual" aria-label="套餐选购预览">
        <div class="storefront-preview">
          <header>
            <span><i></i><i></i><i></i></span>
            <strong>{{ app.siteName }} · 套餐中心</strong>
          </header>
          <div v-if="featuredPlan" class="storefront-preview__body">
            <div class="storefront-preview__title">
              <div><small>{{ featuredPlan.slug }}</small><h2>{{ featuredPlan.name }}</h2></div>
              <span>可购买</span>
            </div>
            <p>{{ featuredPlan.summary || '稳定、透明的订阅服务。' }}</p>
            <div class="storefront-preview__price" v-if="featuredPlan.primary_sku">
              <strong>{{ formatCurrency(featuredPlan.primary_sku.price_cents, featuredPlan.primary_sku.currency) }}</strong>
              <span>/ {{ billingLabel(featuredPlan.primary_sku) }}</span>
            </div>
            <div class="storefront-preview__features">
              <span>{{ formatBytes(featuredPlan.traffic_bytes) }} 流量</span>
              <span>{{ featuredPlan.speed_limit_mbps > 0 ? `${featuredPlan.speed_limit_mbps} Mbps` : '不限速' }}</span>
              <span>{{ featuredPlan.device_limit > 0 ? `${featuredPlan.device_limit} 台设备` : '不限设备' }}</span>
            </div>
            <RouterLink class="button" :to="`/pricing?plan=${featuredPlan.id}`">查看套餐详情<UiIcon name="chevron" /></RouterLink>
          </div>
          <div v-else class="storefront-preview__empty">
            <UiIcon name="plans" />
            <strong>套餐中心</strong>
            <span>可购买套餐将在这里展示</span>
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
        <p>首页只展示套餐核心信息，进入详情后再比较全部规格。</p>
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
        <article><span class="feature-icon"><UiIcon name="plans" /></span><h3>目录保持简洁</h3><p>套餐卡片聚焦名称、起始价格和核心权益，规格不会挤满首页。</p></article>
        <article><span class="feature-icon"><UiIcon name="check" /></span><h3>详情完整比较</h3><p>进入商品详情后一次查看全部可用规格，再选择适合的周期。</p></article>
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
import { fetchPlanCatalogPage, fetchPublicSystemConfigs, type PlanCatalogItem, type PlanSKU, type SystemConfig } from '../api/client'
import CommercePlanCard from '../components/CommercePlanCard.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'

const app = useAppStore()
const router = useRouter()
const configs = ref<SystemConfig[]>([])
const plans = ref<PlanCatalogItem[]>([])

const siteDescription = computed(() => String(configs.value.find(item => item.config_key === 'site_desc')?.value || '浏览套餐、选择规格、确认订单，再在用户中心管理每一条独立订阅。'))
const secondaryPath = computed(() => app.isAuthenticated ? (app.isAdmin ? '/admin/dashboard' : '/account') : (app.installation?.allow_registration ? '/register' : '/login'))
const secondaryLabel = computed(() => app.isAuthenticated ? (app.isAdmin ? '进入管理后台' : '进入用户中心') : (app.installation?.allow_registration ? '注册账户' : '立即登录'))
const featuredPlan = computed(() => plans.value[0] || null)

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

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
