<template>
  <section class="card">
    <h2>Admin/User Login</h2>
    <p>Use one account to log in. Register endpoint is available in API only.</p>

    <label>
      Account
      <input v-model="account" placeholder="username or email" />
    </label>

    <label>
      Password
      <input v-model="password" type="password" placeholder="password" />
    </label>

    <button :disabled="loading" @click="submit">Login</button>

    <p class="error" v-if="error">{{ error }}</p>
    <p v-if="msg">{{ msg }}</p>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../api/client'
import { useAppStore } from '../stores/app'
import { useRoute } from 'vue-router'

const account = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')
const msg = ref('')
const store = useAppStore()
const router = useRouter()
const route = useRoute()

const submit = async () => {
  loading.value = true
  error.value = ''
  msg.value = ''
  try {
    const result = await login(account.value, password.value)
    store.setToken(result.auth.token)
    store.setUser(result.user)
    msg.value = 'login success'
    const target = typeof route.query.redirect === 'string' && route.query.redirect ? route.query.redirect : '/dashboard'
    router.push(target)
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'login failed'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.card {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 16px;
  max-width: 420px;
}

label {
  display: grid;
  gap: 8px;
  margin: 10px 0;
}

input {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #090f1e;
  color: var(--text);
  padding: 8px;
}

button {
  margin-top: 8px;
  border: 1px solid var(--line);
  background: #2a4eff33;
  color: var(--text);
  border-radius: 8px;
  padding: 8px 12px;
}
.error { color: #ff6b6b; }
</style>
