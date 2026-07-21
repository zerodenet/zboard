<template>
  <section class="account-page stack">
    <header class="account-page-header"><div><p class="page-eyebrow">MY ACCOUNT</p><h1>你好，{{ app.user.email }}</h1><p>这是你的订阅、流量和订单状态总览。</p></div><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />刷新</button></header>
    <div v-if="error" class="alert alert-danger"><UiIcon name="alert" />{{ error }}</div>
    <div class="account-metric-grid">
      <article><span class="metric-icon"><UiIcon name="activity" /></span><div><small>剩余流量</small><strong>{{ formatBytes(summary.remaining_bytes) }}</strong><p>累计使用 {{ formatBytes(summary.total_used_bytes) }}</p></div></article>
      <article><span class="metric-icon success"><UiIcon name="plans" /></span><div><small>有效订阅</small><strong>{{ activeSubscriptions.length }}</strong><p>全部订阅 {{ subscriptions.length }}</p></div></article>
      <article><span class="metric-icon warning"><UiIcon name="billing" /></span><div><small>待处理订单</small><strong>{{ pendingOrders }}</strong><p>全部订单 {{ orders.length }}</p></div></article>
    </div>
    <div class="section-grid">
      <article class="panel span-8">
        <header class="panel-header"><div><h2>当前订阅</h2><p>套餐用量和到期状态。</p></div><RouterLink class="button button-ghost button-sm" to="/account/subscription">管理订阅<UiIcon name="chevron" /></RouterLink></header>
        <div v-if="activeSubscriptions.length" class="panel-body customer-subscriptions">
          <article v-for="sub in activeSubscriptions" :key="sub.id"><div class="subscription-title"><div><span>订阅 #{{ sub.id }}</span><h3>{{ planName(sub.plan_id) }}</h3></div><StatusBadge tone="success">服务中</StatusBadge></div><p class="quota-value">{{ formatBytes(remaining(sub)) }} <span>/ {{ formatBytes(sub.flow_total) }}</span></p><div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div><footer><span>已使用 {{ formatBytes(sub.flow_used) }}</span><strong>{{ daysRemaining(sub.end_at) }}后到期</strong></footer></article>
        </div>
        <EmptyState v-else icon="plans" title="还没有有效订阅" description="选择一个套餐并创建订单，订单确认后即可获得订阅服务。"><template #actions><RouterLink class="button button-sm" to="/account/plans">选择套餐</RouterLink></template></EmptyState>
      </article>
      <article class="panel span-4">
        <header class="panel-header"><div><h2>快捷操作</h2><p>常用入口集中在这里。</p></div></header>
        <div class="panel-body quick-links"><RouterLink to="/account/plans"><span><UiIcon name="plans" /></span><div><strong>购买套餐</strong><small>浏览可售规格</small></div><UiIcon name="chevron" /></RouterLink><RouterLink to="/account/subscription"><span><UiIcon name="key" /></span><div><strong>订阅配置</strong><small>生成或复制链接</small></div><UiIcon name="chevron" /></RouterLink><RouterLink to="/account/traffic"><span><UiIcon name="activity" /></span><div><strong>流量明细</strong><small>查看每条使用记录</small></div><UiIcon name="chevron" /></RouterLink></div>
      </article>
    </div>
    <article class="panel"><header class="panel-header"><div><h2>最近订单</h2><p>最近三笔订单的处理状态。</p></div><RouterLink class="button button-ghost button-sm" to="/account/orders">全部订单<UiIcon name="chevron" /></RouterLink></header><div v-if="orders.length" class="table-shell"><table class="data-table"><thead><tr><th>订单</th><th>商品</th><th>金额</th><th>状态</th><th>创建时间</th></tr></thead><tbody><tr v-for="order in orders.slice(0,3)" :key="order.id"><td>#{{ order.id }}</td><td><div class="cell-title"><strong>{{ order.plan_name || `套餐 #${order.plan_id}` }}</strong><span>{{ order.sku_name }}</span></div></td><td>{{ formatCurrency(order.amount_cents, order.currency) }}</td><td><StatusBadge :tone="orderTone(order.status)">{{ orderLabel(order.status) }}</StatusBadge></td><td>{{ formatDateTime(order.created_at) }}</td></tr></tbody></table></div><EmptyState v-else icon="billing" title="还没有订单" description="从套餐页面创建第一笔订单。" /></article>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchOrders, fetchPlans, fetchSubscriptions, fetchTrafficSummary } from '../../api/client'
import EmptyState from '../../components/EmptyState.vue'; import StatusBadge from '../../components/StatusBadge.vue'; import UiIcon from '../../components/UiIcon.vue'
import { useAppStore } from '../../stores/app'; import { daysRemaining, formatBytes, formatCurrency, formatDateTime } from '../../utils/format'
const app=useAppStore(); const loading=ref(false); const error=ref(''); const summary=ref<any>({}); const subscriptions=ref<any[]>([]); const orders=ref<any[]>([]); const plans=ref<any[]>([])
const activeSubscriptions=computed(()=>subscriptions.value.filter(item=>item.status==='active')); const pendingOrders=computed(()=>orders.value.filter(item=>item.status==='pending').length)
function remaining(sub:any){return Math.max(0,(sub.flow_total||0)-(sub.flow_used||0))} function usagePercent(sub:any){return sub.flow_total?Math.min(100,Math.round((sub.flow_used||0)/sub.flow_total*100)):0} function planName(id:number){return plans.value.find(p=>p.id===id)?.name||`套餐 #${id}`}
function orderTone(status:string):'success'|'warning'|'danger'|'neutral'{return status==='paid'?'success':status==='pending'?'warning':status==='failed'?'danger':'neutral'} function orderLabel(status:string){return ({pending:'待支付',paid:'已支付',failed:'失败',canceled:'已取消'} as Record<string,string>)[status]||status}
async function load(){loading.value=true;error.value='';try{[summary.value,subscriptions.value,orders.value,plans.value]=await Promise.all([fetchTrafficSummary(),fetchSubscriptions(),fetchOrders(),fetchPlans()])}catch(e:any){error.value=e?.response?.data?.message||'账户数据加载失败。'}finally{loading.value=false}}
onMounted(load)
</script>
