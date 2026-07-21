<template>
  <section>
    <PageHeader title="订阅管理" description="查看全站服务实例、配额和到期状态；个人订阅链接仍由用户在个人中心管理。" eyebrow="Service Delivery">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />{{ loading ? '刷新中…' : '刷新订阅' }}</button></template>
    </PageHeader>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="metric-grid subscription-metrics">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon :name="metric.icon" /></span><StatusBadge :tone="metric.tone">{{ metric.status }}</StatusBadge></div><div><p class="metric-label">{{ metric.label }}</p><p class="metric-value">{{ metric.value }}</p><span class="metric-meta">{{ metric.meta }}</span></div></article>
    </div>

    <article class="panel">
      <header class="panel-header"><div><h2>服务实例</h2><p>从用户、套餐、流量和有效期四个维度定位订阅。</p></div><span class="count-label">{{ subscriptions.length }} 条</span></header>
      <div class="panel-body toolbar"><div class="toolbar-group"><select v-model="statusFilter" aria-label="订阅状态" @change="load"><option value="">全部状态</option><option value="active">有效</option><option value="expired">已到期或耗尽</option><option value="canceled">已取消</option></select><select v-model="userFilter" aria-label="订阅用户" @change="load"><option value="">全部用户</option><option v-for="user in users" :key="user.id" :value="String(user.id)">{{ user.email }}</option></select></div><RouterLink class="button button-ghost button-sm" :to="trafficLink">查看流量<UiIcon name="chevron" /></RouterLink></div>
      <div v-if="subscriptions.length" class="table-shell"><table class="data-table"><thead><tr><th>订阅</th><th>用户</th><th>套餐</th><th>流量配额</th><th>剩余时间</th><th>状态</th><th></th></tr></thead><tbody><tr v-for="sub in subscriptions" :key="sub.id"><td class="mono">#{{ sub.id }}</td><td><div class="cell-title"><strong>{{ userName(sub.user_id) }}</strong><span>用户 #{{ sub.user_id }}</span></div></td><td><div class="cell-title"><strong>{{ planName(sub.plan_id) }}</strong><span>SKU #{{ sub.plan_sku_id }}</span></div></td><td><div class="quota-cell"><span>{{ formatBytes(sub.flow_used) }} / {{ formatBytes(sub.flow_total) }}</span><div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div><small>剩余 {{ formatBytes(Math.max(0, sub.flow_total - sub.flow_used)) }}</small></div></td><td><div class="cell-title"><strong>{{ daysRemaining(sub.end_at) }}</strong><span>{{ formatDateTime(sub.end_at) }}</span></div></td><td><StatusBadge :tone="statusTone(sub.status)">{{ statusName(sub.status) }}</StatusBadge></td><td><div class="cell-actions"><RouterLink class="button button-secondary button-sm" :to="`/admin/traffic?subscription_id=${sub.id}`">流量记录</RouterLink><RouterLink class="button button-ghost button-sm" :to="`/admin/orders?user_id=${sub.user_id}`">用户订单</RouterLink></div></td></tr></tbody></table></div>
      <EmptyState v-else icon="plans" title="没有匹配订阅" description="调整状态或用户筛选条件。" />
    </article>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { fetchPlans, fetchSubscriptions, fetchUsers } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { daysRemaining as remainingDays, formatBytes, formatDateTime } from '../utils/format'

type SubscriptionItem = { id: number; user_id: number; plan_id: number; plan_sku_id: number; end_at: string; status: string; flow_total: number; flow_used: number }
type UserItem = { id: number; email: string }
const route = useRoute()
const subscriptions = ref<SubscriptionItem[]>([])
const users = ref<UserItem[]>([])
const plans = ref<any[]>([])
const statusFilter = ref(String(route.query.status || ''))
const userFilter = ref(String(route.query.user_id || ''))
const loading = ref(false)
const error = ref('')
const trafficLink = computed(() => userFilter.value ? `/admin/traffic?user_id=${userFilter.value}` : '/admin/traffic')
const metrics = computed(() => [
  { label: '有效订阅', value: subscriptions.value.filter(item => item.status === 'active').length, icon: 'activity', status: '服务中', tone: 'success' as const, meta: '当前筛选范围内有效' },
  { label: '已失效', value: subscriptions.value.filter(item => item.status !== 'active').length, icon: 'clock', status: '需关注', tone: 'warning' as const, meta: '到期、耗尽或已取消' },
  { label: '剩余流量池', value: formatBytes(subscriptions.value.reduce((sum, item) => sum + Math.max(0, item.flow_total - item.flow_used), 0)), icon: 'database', status: '配额', tone: 'info' as const, meta: '当前列表剩余配额合计' },
  { label: '涉及用户', value: new Set(subscriptions.value.map(item => item.user_id)).size, icon: 'users', status: '范围', tone: 'neutral' as const, meta: '拥有匹配订阅的用户' }
])
function userName(id: number) { return users.value.find(user => user.id === id)?.email || `用户 #${id}` }
function planName(id: number) { return plans.value.find(plan => plan.id === id)?.name || `套餐 #${id}` }
function usagePercent(sub: SubscriptionItem) { return sub.flow_total ? Math.min(100, Math.round(sub.flow_used / sub.flow_total * 100)) : 0 }
function daysRemaining(value: string) { const days = remainingDays(value); return days === '已到期' ? days : `剩余 ${days}` }
function statusName(status: string) { return ({ active: '有效', expired: '已失效', canceled: '已取消' } as Record<string, string>)[status] || status }
function statusTone(status: string): 'success' | 'warning' | 'neutral' { return status === 'active' ? 'success' : status === 'expired' ? 'warning' : 'neutral' }
async function load() { loading.value = true; error.value = ''; try { const [subscriptionData, userData, planData] = await Promise.all([fetchSubscriptions({ userId: Number(userFilter.value) || undefined, status: statusFilter.value || undefined }, true), fetchUsers(), fetchPlans()]); subscriptions.value = subscriptionData; users.value = userData; plans.value = planData } catch (e: any) { error.value = e?.response?.data?.message || '订阅数据加载失败。' } finally { loading.value = false } }
onMounted(load)
</script>

<style scoped>
.page-alert,.subscription-metrics { margin-bottom: 16px; }.count-label { color: var(--muted); font-size: 12px; }.toolbar select { min-width: 180px; }.quota-cell { min-width: 190px; }.quota-cell > span,.quota-cell small { display: block; font-size: 10px; }.quota-cell small { margin-top: 6px; color: var(--muted); }.quota-cell .usage-track { height: 5px; margin-top: 7px; }
</style>
