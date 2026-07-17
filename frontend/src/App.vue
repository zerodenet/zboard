<template>
  <div class="layout">
    <aside class="sidebar">
      <h1>zboard</h1>
      <p class="version">v{{ version }}</p>
      <p v-if="app.isAuthenticated">Welcome {{ app.user.username || 'user' }}</p>
      <p v-if="app.isAdmin">Role: Admin</p>
      <nav>
        <RouterLink to="/login" v-if="!app.isAuthenticated">Login</RouterLink>
        <RouterLink to="/dashboard" v-if="app.isAuthenticated">Dashboard</RouterLink>
        <RouterLink to="/nodes" v-if="app.isAuthenticated">Nodes</RouterLink>
        <RouterLink to="/plans" v-if="app.isAuthenticated">Plans</RouterLink>
        <RouterLink to="/billing" v-if="app.isAuthenticated">Billing</RouterLink>
        <RouterLink to="/users" v-if="app.isAdmin">Users</RouterLink>
        <RouterLink to="/audit-logs" v-if="app.isAdmin">Audit Logs</RouterLink>
      </nav>
      <div v-if="app.isAuthenticated" class="actions">
        <button @click="logout">Logout</button>
      </div>
    </aside>
    <main class="content">
      <RouterView />
    </main>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getVersion } from './api/client'
import { useAppStore } from './stores/app'
import { useRouter } from 'vue-router'

const app = useAppStore()
const version = ref('loading')
const router = useRouter()

onMounted(async () => {
  try {
    const result = await getVersion()
    version.value = result.version || 'v0.0.0'
  } catch (_) {
    version.value = 'unreachable'
  }
  app.loadMe()
})

const logout = () => {
  app.clear()
  router.push('/login')
}
</script>

<style scoped>
.layout {
  display: grid;
  grid-template-columns: 240px 1fr;
  min-height: 100vh;
}

.sidebar {
  padding: 24px;
  background: #0a1224cc;
  border-right: 1px solid var(--line);
}

.content {
  padding: 24px;
}

nav {
  display: grid;
  gap: 10px;
}

a {
  color: var(--text);
  text-decoration: none;
}

.actions {
  margin-top: 16px;
}

button {
  border: 1px solid var(--line);
  background: transparent;
  color: var(--text);
  padding: 8px 12px;
  border-radius: 8px;
}
</style>
