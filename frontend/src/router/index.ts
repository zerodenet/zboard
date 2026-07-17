import type { RouteRecordRaw } from 'vue-router'
import Login from '../views/Login.vue'
import Dashboard from '../views/Dashboard.vue'
import Nodes from '../views/Nodes.vue'
import Plans from '../views/Plans.vue'
import Billing from '../views/Billing.vue'
import Users from '../views/Users.vue'
import AuditLogs from '../views/AuditLogs.vue'

export const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/dashboard' },
  { path: '/login', component: Login, meta: { requiresGuest: true } },
  { path: '/dashboard', component: Dashboard, meta: { requiresAuth: true } },
  { path: '/nodes', component: Nodes, meta: { requiresAuth: true } },
  { path: '/plans', component: Plans, meta: { requiresAuth: true } },
  { path: '/billing', component: Billing, meta: { requiresAuth: true } },
  { path: '/users', component: Users, meta: { requiresAuth: true, requiresAdmin: true } },
  { path: '/audit-logs', component: AuditLogs, meta: { requiresAuth: true, requiresAdmin: true } },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]
