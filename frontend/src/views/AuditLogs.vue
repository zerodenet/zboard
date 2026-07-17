<template>
  <section>
    <h2>Audit Logs</h2>
    <p class="muted">Security and business state changes. Secrets and passwords are never recorded here.</p>

    <div class="filters">
      <input v-model="actor" placeholder="actor (exact)" />
      <input v-model="action" placeholder="action (exact)" />
      <input v-model="target" placeholder="target (exact, e.g. order:12)" />
      <button type="button" @click="applyFilters">Query</button>
    </div>

    <div class="log-list">
      <article v-for="item in items" :key="item.id" class="log-card">
        <div><strong>#{{ item.id }} {{ item.action }}</strong></div>
        <div>{{ item.actor }} → {{ item.target }}</div>
        <div class="muted">{{ item.detail || '-' }}</div>
        <time class="muted">{{ item.created_at }}</time>
      </article>
    </div>
    <p class="muted" v-if="!loading && items.length === 0">No matching audit events.</p>

    <div class="pagination">
      <button type="button" :disabled="loading || offset === 0" @click="previous">Previous</button>
      <span>{{ total ? offset + 1 : 0 }}-{{ Math.min(offset + items.length, total) }} / {{ total }}</span>
      <button type="button" :disabled="loading || offset + limit >= total" @click="next">Next</button>
    </div>
    <p class="error" v-if="error">{{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchAuditLogs } from '../api/client'

const items = ref<any[]>([])
const total = ref(0)
const offset = ref(0)
const limit = 50
const actor = ref('')
const action = ref('')
const target = ref('')
const loading = ref(false)
const error = ref('')

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const result = await fetchAuditLogs({
      actor: actor.value.trim() || undefined,
      action: action.value.trim() || undefined,
      target: target.value.trim() || undefined,
      offset: offset.value,
      limit
    })
    items.value = result.items || []
    total.value = result.total || 0
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'failed to load audit logs'
  } finally {
    loading.value = false
  }
}

const applyFilters = async () => {
  offset.value = 0
  await load()
}

const previous = async () => {
  offset.value = Math.max(0, offset.value - limit)
  await load()
}

const next = async () => {
  offset.value += limit
  await load()
}

onMounted(load)
</script>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}
.filters input {
  min-width: 210px;
}
.log-list {
  display: grid;
  gap: 8px;
}
.log-card {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 10px;
  display: grid;
  gap: 4px;
  padding: 12px;
}
.pagination {
  align-items: center;
  display: flex;
  gap: 12px;
  margin-top: 16px;
}
.muted { color: var(--muted); }
.error { color: #ff6b6b; }
</style>
