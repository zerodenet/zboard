<template>
  <section>
    <h2>Plans</h2>
    <form @submit.prevent="create" v-if="app.isAdmin">
      <div class="grid-form">
        <input v-model="form.name" placeholder="plan name" />
        <input v-model.number="form.price_cents" type="number" placeholder="price_cents" />
        <input v-model.number="form.traffic_gb" type="number" placeholder="traffic_gb" />
        <input v-model.number="form.duration_day" type="number" placeholder="duration_day" />
        <input v-model.number="form.max_device" type="number" placeholder="max_device" />
      </div>
      <button type="submit">Create Plan</button>
    </form>
    <p class="muted" v-if="!app.isAdmin">Only admins can create or edit plans.</p>

    <p v-if="msg" class="success">{{ msg }}</p>
    <p v-if="error" class="error">{{ error }}</p>

    <ul>
      <li v-for="item in plans" :key="item.id" class="plan-item">
        <div class="row">
          <div>
            #{{ item.id }} {{ item.name }} -
            price {{ item.price_cents }} -
            traffic {{ item.traffic_gb }}GB -
            {{ item.duration_day }} days -
            max_device {{ item.max_device }} -
            active {{ item.is_active ? 'yes' : 'no' }}
            <span v-if="item.is_active"> (active)</span>
          </div>
          <button class="small ghost-btn" @click="order(item.id)">Buy</button>
        </div>

        <div v-if="app.isAdmin" class="admin-edit">
          <div class="inline">
            <label>Name</label>
            <input v-model="item.name" />
          </div>
          <div class="inline">
            <label>Price</label>
            <input v-model.number="item.price_cents" type="number" />
          </div>
          <div class="inline">
            <label>Traffic</label>
            <input v-model.number="item.traffic_gb" type="number" />
          </div>
          <div class="inline">
            <label>Duration</label>
            <input v-model.number="item.duration_day" type="number" />
          </div>
          <div class="inline">
            <label>Max Device</label>
            <input v-model.number="item.max_device" type="number" />
          </div>
          <div class="inline">
            <label>Active</label>
            <input type="checkbox" v-model="item.is_active" />
          </div>
          <div class="inline actions">
            <button class="small" @click="save(item)">Save</button>
            <button class="small" @click="toggleActive(item)">
              {{ item.is_active ? 'Disable' : 'Enable' }}
            </button>
          </div>
        </div>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { createOrder, createPlan, fetchPlansWithOptions, updatePlan } from '../api/client'
import { useAppStore } from '../stores/app'

type PlanItem = {
  id: number
  name: string
  price_cents: number
  traffic_gb: number
  duration_day: number
  max_device: number
  is_active: boolean
}

const app = useAppStore()
const plans = ref<PlanItem[]>([])
const error = ref('')
const msg = ref('')
const form = reactive({
  name: '',
  price_cents: 0,
  traffic_gb: 0,
  duration_day: 30,
  max_device: 1
})

onMounted(async () => {
  await refresh()
})

const refresh = async () => {
  plans.value = await fetchPlansWithOptions({ includeInactive: app.isAdmin })
}

const create = async () => {
  if (!app.isAdmin) {
    error.value = 'only admin can create plan'
    return
  }
  error.value = ''
  msg.value = ''
  try {
    await createPlan({
      name: form.name,
      price_cents: form.price_cents,
      traffic_gb: form.traffic_gb,
      duration_day: form.duration_day,
      max_device: form.max_device
    })
    msg.value = 'plan created'
    form.name = ''
    form.price_cents = 0
    form.traffic_gb = 0
    form.duration_day = 30
    form.max_device = 1
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'create failed'
  }
}

const order = async (planId: number) => {
  error.value = ''
  msg.value = ''
  try {
    await createOrder(planId)
    msg.value = 'order created'
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'create order failed'
  }
}

const save = async (item: PlanItem) => {
  error.value = ''
  msg.value = ''
  try {
    await updatePlan(item.id, {
      name: item.name,
      price_cents: item.price_cents,
      traffic_gb: item.traffic_gb,
      duration_day: item.duration_day,
      max_device: item.max_device,
      is_active: item.is_active
    })
    msg.value = `plan ${item.id} saved`
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'update failed'
  }
}

const toggleActive = async (item: PlanItem) => {
  error.value = ''
  msg.value = ''
  try {
    await updatePlan(item.id, { is_active: !item.is_active })
    msg.value = `plan ${item.id} updated`
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'toggle failed'
  }
}
</script>

<style scoped>
.grid-form {
  display: grid;
  gap: 8px;
  max-width: 700px;
  margin-bottom: 12px;
}

input {
  border: 1px solid var(--line);
  background: #090f1e;
  color: var(--text);
  padding: 8px;
  border-radius: 8px;
}

button {
  margin-bottom: 12px;
}

ul {
  padding-left: 20px;
}

.plan-item {
  margin-bottom: 14px;
}

.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.admin-edit {
  margin-top: 8px;
  display: grid;
  gap: 8px;
  max-width: 960px;
}

.inline {
  display: flex;
  align-items: center;
  gap: 8px;
}

.inline label {
  width: 110px;
}

.actions {
  justify-content: flex-end;
}

.small {
  margin-left: 8px;
}

.error {
  color: #ff6b6b;
}

.success {
  color: #63e6be;
}

.ghost-btn {
  margin-left: 8px;
}
</style>
