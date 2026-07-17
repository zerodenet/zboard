<template>
  <section>
  <h2>Billing</h2>

  <h3>Traffic summary</h3>
  <pre>{{ summaryText }}</pre>

  <h3>Subscription URL</h3>
  <div class="subscription-access">
    <p v-if="subscriptionAccess.configured" class="muted">
      Active token: {{ subscriptionAccess.token_prefix }}... Last used: {{ subscriptionAccess.last_used_at || 'never' }}
    </p>
    <p v-else class="muted">No active subscription URL. Generate one after purchasing a plan.</p>
    <input v-if="subscriptionUrl" :value="subscriptionUrl" readonly />
    <button class="small" @click="rotateAccess">{{ subscriptionAccess.configured ? 'Rotate URL' : 'Generate URL' }}</button>
    <button class="small" v-if="subscriptionAccess.configured" @click="revokeAccess">Revoke</button>
    <p class="muted" v-if="subscriptionUrl">This URL is shown once. Rotating it immediately invalidates the old URL.</p>
  </div>

  <h3>Active subscription summary</h3>
    <div class="subscription-summary" v-if="activeSubscriptions.length">
      <article class="summary-card" v-for="sub in activeSubscriptions" :key="`active-${sub.id}`">
        <p>
          <strong>Plan {{ sub.plan_id }}</strong>
          ({{ getPlanName(sub.plan_id) }})
        </p>
        <p class="muted">
          {{ displayBytes(remainBytes(sub)) }} remain / {{ displayBytes(sub.flow_total) }} total
        </p>
        <p class="muted">Expires in {{ daysUntilEnd(sub.end_at) }}.</p>
        <button class="small" @click="renewByPlan(sub.plan_id)">Renew</button>
      </article>
    </div>
    <p class="muted" v-else>No active subscription</p>

    <h3>Plans you can subscribe</h3>
    <ul>
      <li v-for="plan in plans" :key="plan.id">
        #{{ plan.id }} {{ plan.name }} - {{ plan.traffic_gb }}GB / {{ plan.duration_day }}d -
        {{ plan.price_cents }} cents
        <button class="small" @click="order(plan.id)">Buy</button>
      </li>
    </ul>

    <h3>Orders</h3>
    <div class="filters">
      <label>status</label>
      <select v-model="orderStatusFilter">
        <option value="">all</option>
        <option value="pending">pending</option>
        <option value="paid">paid</option>
        <option value="failed">failed</option>
        <option value="canceled">canceled</option>
      </select>
      <button @click="refresh">Refresh</button>
    </div>
    <ul>
      <li v-for="item in orders" :key="item.id">
        #{{ item.id }} user={{ item.user_id }} plan={{ item.plan_id }} trade_no={{ item.trade_no }}
        status={{ item.status }} amount={{ item.amount_cents }}
        <button class="small" v-if="canPay(item)" @click="pay(item.id)">Pay</button>
        <button class="small" v-if="canCancel(item)" @click="cancel(item.id)">Cancel</button>
        <button class="small" v-if="app.isAdmin && item.status !== 'paid'" @click="markPaid(item.id)">Mark Paid</button>
      </li>
    </ul>

    <h3>Subscriptions</h3>
    <ul>
      <li v-for="sub in subscriptions" :key="sub.id">
        #{{ sub.id }} user={{ sub.user_id }} plan={{ sub.plan_id }} status={{ sub.status }}
        flow={{ sub.flow_used }}/{{ sub.flow_total }} bytes
        end={{ sub.end_at || '-' }}
        <button class="small" @click="renewByPlan(sub.plan_id)">Renew Plan</button>
      </li>
    </ul>

    <h3>Traffic records</h3>
    <div class="filters">
      <label>user id</label>
      <input v-if="app.isAdmin" v-model.number="recordFilterUserId" type="number" placeholder="user id" />
      <label>node id</label>
      <input v-model.number="recordFilterNodeId" type="number" placeholder="node id" />
      <label>subscription id</label>
      <input v-model.number="recordFilterSubscriptionId" type="number" placeholder="subscription id" />
      <button class="small" @click="refreshRecords">query records</button>
    </div>
    <ul>
      <li v-for="record in trafficRecords" :key="record.id">
        #{{ record.id }} report={{ record.report_id || 'legacy' }} subscription={{ record.subscription_id || 'legacy' }} node={{ record.node_id }} used={{ record.used_bytes }} bytes at {{ record.record_at }} {{ record.meta || '' }}
      </li>
    </ul>

    <h3>Traffic reconciliation</h3>
    <button class="small" @click="refreshReconciliation">refresh reconciliation</button>
    <ul>
      <li v-for="item in reconciliation" :key="item.subscription_id">
        subscription={{ item.subscription_id }} user={{ item.user_id }} plan={{ item.plan_id }}
        flow_used={{ item.flow_used }} recorded={{ item.recorded_bytes }} difference={{ item.difference }}
        result={{ item.result }}
      </li>
    </ul>

    <p class="error" v-if="errorMsg">{{ errorMsg }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  cancelOrder,
  createOrder,
  fetchOrders,
  fetchPlans,
  fetchSubscriptions,
  fetchSubscriptionAccess,
  fetchTrafficRecords,
  fetchTrafficReconciliation,
  fetchTrafficSummary,
  markOrderPaid,
  payOrder,
  revokeSubscriptionAccess,
  rotateSubscriptionAccess
} from '../api/client'
import { useAppStore } from '../stores/app'

type OrderItem = {
  id: number
  user_id: number
  plan_id: number
  trade_no: string
  status: string
  amount_cents: number
}

type PlanItem = {
  id: number
  name: string
  traffic_gb: number
  duration_day: number
  price_cents: number
  is_active: boolean
}

type SubscriptionItem = {
  id: number
  user_id: number
  plan_id: number
  start_at: string
  end_at: string
  status: string
  flow_total: number
  flow_used: number
}

const orders = ref<OrderItem[]>([])
const subscriptions = ref<SubscriptionItem[]>([])
const subscriptionAccess = ref<any>({ configured: false })
const subscriptionUrl = ref('')
const trafficRecords = ref<any[]>([])
const reconciliation = ref<any[]>([])
const summaryText = ref('{}')
const errorMsg = ref('')
const orderStatusFilter = ref('')
const plans = ref<PlanItem[]>([])
const app = useAppStore()
const recordFilterUserId = ref<number | undefined>()
const recordFilterNodeId = ref<number | undefined>()
const recordFilterSubscriptionId = ref<number | undefined>()
const activeSubscriptions = computed(() => subscriptions.value.filter((item) => item.status === 'active'))

const loadData = async () => {
  errorMsg.value = ''
  try {
    orders.value = await fetchOrders({
      status: orderStatusFilter.value || undefined
    })
    subscriptions.value = await fetchSubscriptions()
    subscriptionAccess.value = await fetchSubscriptionAccess()
    plans.value = await fetchPlans()
    summaryText.value = JSON.stringify(await fetchTrafficSummary(), null, 2)
    await refreshRecords()
    await refreshReconciliation()
  } catch (e: any) {
    summaryText.value = JSON.stringify({ error: 'not available' }, null, 2)
    errorMsg.value = e?.response?.data?.message || 'load failed'
  }
}

const refreshRecords = async () => {
  trafficRecords.value = await fetchTrafficRecords({
    userId: app.isAdmin ? recordFilterUserId.value : undefined,
    nodeId: recordFilterNodeId.value,
    subscriptionId: recordFilterSubscriptionId.value
  })
}

const refreshReconciliation = async () => {
  reconciliation.value = await fetchTrafficReconciliation({
    userId: app.isAdmin ? recordFilterUserId.value : undefined,
    subscriptionId: recordFilterSubscriptionId.value
  })
}

const canPay = (item: OrderItem) => {
  if (item.status !== 'pending') {
    return false
  }
  return app.isAdmin || item.user_id === app.user.id
}

const canCancel = (item: OrderItem) => {
  if (!['pending'].includes(item.status)) {
    return false
  }
  return app.isAdmin || item.user_id === app.user.id
}

const refresh = loadData

const getPlanName = (planId: number) => {
  const plan = plans.value.find((item) => item.id === planId)
  return plan ? plan.name : `#${planId}`
}

const remainBytes = (sub: SubscriptionItem) => {
  const remain = (sub.flow_total || 0) - (sub.flow_used || 0)
  return remain > 0 ? remain : 0
}

const displayBytes = (bytes: number) => {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const divisor = 1024
  let value = Number(bytes) || 0
  let index = 0

  while (value >= divisor && index < units.length - 1) {
    value /= divisor
    index += 1
  }
  const precision = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${units[index]}`
}

const daysUntilEnd = (endAt: string) => {
  if (!endAt) {
    return 'N/A'
  }
  const endTs = new Date(endAt).getTime()
  if (!Number.isFinite(endTs)) {
    return 'N/A'
  }
  const remainingMs = endTs - Date.now()
  if (remainingMs <= 0) {
    return 'expired'
  }
  const days = Math.ceil(remainingMs / (24 * 60 * 60 * 1000))
  return `${days} day${days > 1 ? 's' : ''}`
}

const order = async (planId: number) => {
  try {
    await createOrder(planId)
    await loadData()
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'order create failed'
  }
}

const renewByPlan = async (planId: number) => {
  await order(planId)
}

const rotateAccess = async () => {
  try {
    const result = await rotateSubscriptionAccess()
    subscriptionAccess.value = result
    subscriptionUrl.value = `${window.location.origin}${result.subscription_url}`
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'subscription URL generation failed'
  }
}

const revokeAccess = async () => {
  try {
    await revokeSubscriptionAccess()
    subscriptionAccess.value = { configured: false }
    subscriptionUrl.value = ''
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'subscription URL revoke failed'
  }
}

const pay = async (id: number) => {
  try {
    await payOrder(id)
    await loadData()
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'pay failed'
  }
}

const cancel = async (id: number) => {
  try {
    await cancelOrder(id)
    await loadData()
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'cancel failed'
  }
}

const markPaid = async (id: number) => {
  try {
    await markOrderPaid(id)
    await loadData()
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || 'mark paid failed'
  }
}

onMounted(loadData)
</script>

<style scoped>
.small {
  margin-left: 8px;
}

.grid-form {
  display: grid;
  gap: 8px;
  max-width: 720px;
}

.subscription-summary {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
}

.subscription-access {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 20px;
}

.subscription-access input {
  flex: 1 1 420px;
}

.summary-card {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 10px;
}

.filters {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}

.error {
  color: #ff6b6b;
}

.muted {
  color: var(--muted);
}
</style>
