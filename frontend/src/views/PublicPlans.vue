<template>
  <div class="pricing-page">
    <section class="pricing-hero"><span>套餐与价格</span><h1>按你的使用方式选择</h1><p>所有价格、周期、流量和设备限制都在下单前完整展示。</p></section>
    <div v-if="error" class="alert alert-danger pricing-alert"><UiIcon name="alert" />{{ error }}</div>
    <section v-if="plans.length" class="public-plan-grid">
      <article v-for="(plan, index) in plans" :key="plan.id" :class="{ featured: index === 1 || (plans.length === 1 && index === 0) }">
        <span v-if="index === 1 || (plans.length === 1 && index === 0)" class="plan-ribbon">推荐</span>
        <div class="public-plan-head"><span>{{ plan.slug }}</span><h2>{{ plan.name }}</h2><p>{{ plan.summary || plan.description || '稳定、透明的订阅服务。' }}</p></div>
        <div v-if="activeSku(plan)" class="public-price"><strong>{{ formatCurrency(activeSku(plan).price_cents, activeSku(plan).currency) }}</strong><span>/ {{ billingLabel(activeSku(plan)) }}</span></div>
        <ul v-if="activeSku(plan)"><li><UiIcon name="check" />{{ formatBytes(activeSku(plan).traffic_bytes) }} 套餐流量</li><li><UiIcon name="check" />{{ activeSku(plan).device_limit || '不限' }} 台设备</li><li><UiIcon name="check" />{{ activeSku(plan).speed_limit_mbps ? `${activeSku(plan).speed_limit_mbps} Mbps` : '不限速' }}</li><li><UiIcon name="check" />{{ plan.is_renewable ? '支持续费' : '固定周期' }}</li></ul>
        <RouterLink class="button" :class="{ 'button-secondary': !(index === 1 || (plans.length === 1 && index === 0)) }" :to="purchasePath">{{ app.isAuthenticated ? '进入用户中心购买' : '登录后购买' }}</RouterLink>
      </article>
    </section>
    <div v-else-if="!loading" class="public-empty"><span><UiIcon name="plans" /></span><h2>套餐正在准备中</h2><p>管理员发布可售套餐后会显示在这里。</p></div>
    <section class="pricing-note"><UiIcon name="shield" /><div><strong>购买前信息完全透明</strong><p>订单会保留套餐与规格快照，后续调整不会改变已经创建的订单。</p></div></section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchPlans } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'
const app = useAppStore(); const plans = ref<any[]>([]); const loading = ref(false); const error = ref('')
const purchasePath = computed(() => app.isAuthenticated ? '/account/plans' : '/login?redirect=/account/plans')
function activeSku(plan: any) { return (plan.skus || []).find((sku: any) => sku.is_active) }
function billingLabel(sku: any) { const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string,string>)[sku.billing_unit] || sku.billing_unit; return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}` }
onMounted(async () => { loading.value = true; try { plans.value = await fetchPlans() } catch (e: any) { error.value = e?.response?.data?.message || '套餐加载失败。' } finally { loading.value = false } })
</script>
