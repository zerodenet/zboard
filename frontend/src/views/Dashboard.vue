<template>
  <section>
    <h2>Dashboard</h2>
    <p class="muted">Platform overview and quick status.</p>
    <h3>Active subscription</h3>
    <div class="subscription-summary" v-if="activeSubscriptions.length">
      <article class="summary-card" v-for="sub in activeSubscriptions" :key="`active-${sub.id}`">
        <p><strong>Plan {{ sub.plan_id }}</strong> (user {{ sub.user_id }})</p>
        <p class="muted">
          {{ formatBytes(remainBytes(sub)) }} remain / {{ formatBytes(sub.flow_total) }} total
        </p>
        <p class="muted">Expires in {{ daysUntilEnd(sub.end_at) }}</p>
      </article>
    </div>
    <p class="muted" v-else>No active subscription</p>

    <div class="grid">
      <article class="card">
        <h3>System Version</h3>
        <p>{{ version }}</p>
      </article>
      <article class="card" v-for="(value, key) in stats" :key="key">
        <h3>{{ key }}</h3>
        <p>{{ value }}</p>
      </article>
    </div>
    <article class="card">
      <h3>Traffic summary</h3>
      <pre>{{ summaryText }}</pre>
    </article>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchDashboard, fetchSubscriptions, fetchTrafficSummary, getVersion } from '../api/client'
import { useAppStore } from '../stores/app'

const version = ref('loading')
const stats = ref<Record<string, number>>({})
const summaryText = ref('{}')
const app = useAppStore()
const subscriptions = ref<SubscriptionItem[]>([])

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

const activeSubscriptions = computed(() => subscriptions.value.filter((item) => item.status === 'active'))
const remainBytes = (sub: SubscriptionItem) => {
  const remain = (sub.flow_total || 0) - (sub.flow_used || 0)
  return remain > 0 ? remain : 0
}
const formatBytes = (bytes: number) => {
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

onMounted(async () => {
  const versionData = await getVersion()
  version.value = versionData.version || version.value

  if (app.isAdmin) {
    try {
      const d = await fetchDashboard()
      stats.value = d
    } catch (e) {
      stats.value = {}
    }
  }
  try {
    subscriptions.value = await fetchSubscriptions()
    const t = await fetchTrafficSummary()
    summaryText.value = JSON.stringify(t, null, 2)
  } catch (e) {
    summaryText.value = JSON.stringify({ error: 'not ready' }, null, 2)
  }
})
</script>

<style scoped>
.grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
}

.card {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 12px;
}

pre {
  white-space: pre-wrap;
}
.muted { color: var(--muted); }

.subscription-summary {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  margin-bottom: 12px;
}

.summary-card {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 10px;
}
</style>
