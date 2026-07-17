<template>
  <section>
    <h2>User Management</h2>

    <form @submit.prevent="create" class="card">
      <h3>Create User</h3>
      <div class="grid-form">
        <input v-model="form.username" placeholder="username" />
        <input v-model="form.email" placeholder="email" />
        <input v-model="form.password" type="password" placeholder="password" />
        <label class="inline">
          <input type="checkbox" v-model="form.isAdmin" />
          Admin
        </label>
        <select v-model="form.status">
          <option value="active">active</option>
          <option value="suspended">suspended</option>
          <option value="deactivated">deactivated</option>
        </select>
      </div>
      <button :disabled="loading" type="submit">Create User</button>
    </form>

    <h3>Filter</h3>
    <div class="filters">
      <input v-model="search" placeholder="search username/email" />
      <select v-model="statusFilter">
        <option value="">all status</option>
        <option value="active">active</option>
        <option value="suspended">suspended</option>
        <option value="deactivated">deactivated</option>
      </select>
      <button @click="refresh">Refresh</button>
    </div>

    <ul>
      <li v-for="item in users" :key="item.id">
        #{{ item.id }} {{ item.username }} ({{ item.email }}) - status={{ item.status }} - admin={{ item.is_admin ? 'yes' : 'no' }}
        <button class="small" @click="setStatus(item, 'active')">active</button>
        <button class="small" @click="setStatus(item, 'suspended')">suspend</button>
        <button class="small" @click="setAdmin(item, !item.is_admin)">
          {{ item.is_admin ? 'remove admin' : 'make admin' }}
        </button>
        <button class="small" @click="resetPassword(item)">reset password</button>
      </li>
    </ul>

    <p class="error" v-if="error">{{ error }}</p>
    <p v-if="msg">{{ msg }}</p>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { createAdminUser, fetchUsers, updateAdminUser } from '../api/client'

type UserItem = {
  id: number
  username: string
  email: string
  is_admin: boolean
  status: string
}

const users = ref<UserItem[]>([])
const search = ref('')
const statusFilter = ref('')
const error = ref('')
const msg = ref('')
const loading = ref(false)

const form = reactive({
  username: '',
  email: '',
  password: '',
  isAdmin: false,
  status: 'active'
})

const refresh = async () => {
  const query: { q?: string; status?: string } = {}
  if (search.value) query.q = search.value
  if (statusFilter.value) query.status = statusFilter.value
  users.value = await fetchUsers(query)
}

const create = async () => {
  error.value = ''
  msg.value = ''
  loading.value = true
  try {
    await createAdminUser({
      username: form.username,
      email: form.email,
      password: form.password,
      is_admin: form.isAdmin,
      status: form.status
    })
    msg.value = 'user created'
    await refresh()
    form.username = ''
    form.email = ''
    form.password = ''
    form.isAdmin = false
    form.status = 'active'
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'create user failed'
  } finally {
    loading.value = false
  }
}

const setStatus = async (user: UserItem, status: string) => {
  error.value = ''
  msg.value = ''
  try {
    await updateAdminUser(user.id, { status })
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'update status failed'
  }
}

const setAdmin = async (user: UserItem, isAdmin: boolean) => {
  error.value = ''
  msg.value = ''
  try {
    await updateAdminUser(user.id, { is_admin: isAdmin })
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'update role failed'
  }
}

const resetPassword = async (user: UserItem) => {
  const password = prompt(`Set new password for ${user.username}`)
  if (!password) {
    return
  }
  error.value = ''
  msg.value = ''
  try {
    await updateAdminUser(user.id, { password })
    msg.value = `password reset for ${user.username}`
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'password reset failed'
  }
}

onMounted(async () => {
  await refresh()
})
</script>

<style scoped>
.grid-form {
  display: grid;
  gap: 8px;
  max-width: 700px;
  margin-bottom: 12px;
}

.filters {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}

input,
select {
  border: 1px solid var(--line);
  background: #090f1e;
  color: var(--text);
  padding: 8px;
  border-radius: 8px;
}

.inline {
  display: flex;
  gap: 8px;
  align-items: center;
}

button {
  margin-bottom: 10px;
}

.small {
  margin-left: 8px;
}

ul {
  padding-left: 20px;
}

.error {
  color: #ff6b6b;
}
</style>
