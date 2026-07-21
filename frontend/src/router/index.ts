import type { RouteRecordRaw } from 'vue-router'
import PublicLayout from '../layouts/PublicLayout.vue'
import AccountLayout from '../layouts/AccountLayout.vue'
import AdminLayout from '../layouts/AdminLayout.vue'
import Home from '../views/Home.vue'
import PublicPlans from '../views/PublicPlans.vue'
import Login from '../views/Login.vue'
import Register from '../views/Register.vue'
import Setup from '../views/Setup.vue'
import AccountDashboard from '../views/account/AccountDashboard.vue'
import AccountPlans from '../views/account/AccountPlans.vue'
import AccountOrders from '../views/account/AccountOrders.vue'
import AccountSubscription from '../views/account/AccountSubscription.vue'
import AccountTraffic from '../views/account/AccountTraffic.vue'
import AccountTickets from '../views/account/AccountTickets.vue'
import Dashboard from '../views/Dashboard.vue'
import Nodes from '../views/Nodes.vue'
import Protocols from '../views/Protocols.vue'
import NodeGroups from '../views/NodeGroups.vue'
import Plans from '../views/Plans.vue'
import Orders from '../views/Orders.vue'
import Subscriptions from '../views/Subscriptions.vue'
import Traffic from '../views/Traffic.vue'
import Users from '../views/Users.vue'
import Tasks from '../views/Tasks.vue'
import AuditLogs from '../views/AuditLogs.vue'
import Settings from '../views/Settings.vue'
import Tickets from '../views/Tickets.vue'

export const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: PublicLayout,
    children: [
      { path: '', component: Home, meta: { title: '首页', layout: 'public' } },
      { path: 'pricing', component: PublicPlans, meta: { title: '套餐价格', layout: 'public' } }
    ]
  },
  { path: '/setup', component: Setup, meta: { setupOnly: true, title: '初始化', layout: 'auth' } },
  { path: '/login', component: Login, meta: { requiresGuest: true, title: '登录', layout: 'auth' } },
  { path: '/register', component: Register, meta: { requiresGuest: true, requiresRegistration: true, title: '注册', layout: 'auth' } },
  {
    path: '/account',
    component: AccountLayout,
    meta: { requiresAuth: true, layout: 'account' },
    children: [
      { path: '', component: AccountDashboard, meta: { title: '我的概览' } },
      { path: 'plans', component: AccountPlans, meta: { title: '购买套餐' } },
      { path: 'orders', component: AccountOrders, meta: { title: '我的订单' } },
      { path: 'subscription', component: AccountSubscription, meta: { title: '订阅配置' } },
      { path: 'traffic', component: AccountTraffic, meta: { title: '流量明细' } },
      { path: 'tickets', component: AccountTickets, meta: { title: '我的工单' } }
    ]
  },
  {
    path: '/admin',
    component: AdminLayout,
    meta: { requiresAuth: true, requiresAdmin: true, layout: 'admin' },
    children: [
      { path: '', redirect: '/admin/dashboard' },
      { path: 'dashboard', component: Dashboard, meta: { title: '运营工作台', section: '工作台' } },
      { path: 'users', component: Users, meta: { title: '用户管理', section: '客户与支持' } },
      { path: 'subscriptions', component: Subscriptions, meta: { title: '订阅管理', section: '客户与支持' } },
      { path: 'tickets', component: Tickets, meta: { title: '工单中心', section: '客户与支持' } },
      { path: 'plans', component: Plans, meta: { title: '商品与套餐', section: '商业管理' } },
      { path: 'orders', component: Orders, meta: { title: '订单管理', section: '商业管理' } },
      { path: 'nodes', component: Nodes, meta: { title: '节点资产', section: '基础设施' } },
      { path: 'protocols', component: Protocols, meta: { title: '协议服务', section: '基础设施' } },
      { path: 'node-groups', component: NodeGroups, meta: { title: '节点组', section: '基础设施' } },
      { path: 'traffic', component: Traffic, meta: { title: '流量与对账', section: '基础设施' } },
      { path: 'tasks', component: Tasks, meta: { title: '运营任务', section: '系统运营' } },
      { path: 'audit-logs', component: AuditLogs, meta: { title: '审计日志', section: '系统运营' } },
      { path: 'settings', component: Settings, meta: { title: '系统设置', section: '系统运营' } },
      { path: 'billing', redirect: '/admin/orders' }
    ]
  },
  { path: '/dashboard', redirect: '/admin/dashboard' },
  { path: '/nodes', redirect: '/admin/nodes' },
  { path: '/protocols', redirect: '/admin/protocols' },
  { path: '/node-groups', redirect: '/admin/node-groups' },
  { path: '/plans', redirect: '/admin/plans' },
  { path: '/billing', redirect: '/admin/orders' },
  { path: '/orders', redirect: '/admin/orders' },
  { path: '/subscriptions', redirect: '/admin/subscriptions' },
  { path: '/traffic', redirect: '/admin/traffic' },
  { path: '/users', redirect: '/admin/users' },
  { path: '/tickets', redirect: '/admin/tickets' },
  { path: '/tasks', redirect: '/admin/tasks' },
  { path: '/audit-logs', redirect: '/admin/audit-logs' },
  { path: '/settings', redirect: '/admin/settings' },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]
