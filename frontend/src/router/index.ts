import type { RouteRecordRaw } from 'vue-router'

export const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('../layouts/PublicLayout.vue'),
    children: [
      { path: '', component: () => import('../views/Home.vue'), meta: { title: '首页', layout: 'public' } },
      { path: 'pricing', component: () => import('../views/PublicPlans.vue'), meta: { title: '套餐价格', layout: 'public' } }
    ]
  },
  { path: '/setup', component: () => import('../views/Setup.vue'), meta: { setupOnly: true, title: '初始化', layout: 'auth' } },
  { path: '/login', component: () => import('../views/Login.vue'), meta: { requiresGuest: true, title: '登录', layout: 'auth' } },
  { path: '/register', component: () => import('../views/Register.vue'), meta: { requiresGuest: true, requiresRegistration: true, title: '注册', layout: 'auth' } },
  {
    path: '/account',
    component: () => import('../layouts/AccountLayout.vue'),
    meta: { requiresAuth: true, layout: 'account' },
    children: [
      { path: '', component: () => import('../views/account/AccountDashboard.vue'), meta: { title: '我的概览' } },
      { path: 'plans', component: () => import('../views/account/AccountPlans.vue'), meta: { title: '购买套餐' } },
      { path: 'orders', component: () => import('../views/account/AccountOrders.vue'), meta: { title: '我的订单' } },
      { path: 'subscription', component: () => import('../views/account/AccountSubscription.vue'), meta: { title: '订阅配置' } },
      { path: 'traffic', component: () => import('../views/account/AccountTraffic.vue'), meta: { title: '流量明细' } },
      { path: 'tickets', component: () => import('../views/account/AccountTickets.vue'), meta: { title: '我的工单' } }
    ]
  },
  {
    path: '/admin',
    component: () => import('../layouts/AdminLayout.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, layout: 'admin' },
    children: [
      { path: '', redirect: '/admin/dashboard' },
      { path: 'dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '运营工作台', section: '工作台' } },
      { path: 'users', component: () => import('../views/Users.vue'), meta: { title: '用户管理', section: '客户与支持' } },
      { path: 'subscriptions', component: () => import('../views/Subscriptions.vue'), meta: { title: '订阅管理', section: '客户与支持' } },
      { path: 'fair-use', component: () => import('../views/FairUse.vue'), meta: { title: 'Fair Use 观测', section: '客户与支持' } },
      { path: 'tickets', component: () => import('../views/Tickets.vue'), meta: { title: '工单中心', section: '客户与支持' } },
      { path: 'plans', component: () => import('../views/Plans.vue'), meta: { title: '商品与套餐', section: '商业管理' } },
      { path: 'subscription-templates', component: () => import('../views/SubscriptionTemplates.vue'), meta: { title: '订阅模板', section: '商业管理' } },
      { path: 'subscription-templates/rule-sets', component: () => import('../views/SubscriptionRuleSets.vue'), meta: { title: '规则集', section: '订阅模板' } },
      { path: 'subscription-rule-sets', redirect: '/admin/subscription-templates/rule-sets' },
      { path: 'orders', component: () => import('../views/Orders.vue'), meta: { title: '订单管理', section: '商业管理' } },
      { path: 'nodes', component: () => import('../views/Nodes.vue'), meta: { title: '节点资产', section: '基础设施' } },
      { path: 'certificates', component: () => import('../views/Certificates.vue'), meta: { title: '免费证书', section: '基础设施' } },
      { path: 'providers', component: () => import('../views/Providers.vue'), meta: { title: '外部供应商', section: '基础设施' } },
      { path: 'dns-records', component: () => import('../views/ManagedDNS.vue'), meta: { title: 'DNS 解析', section: '基础设施' } },
      { path: 'protocols', component: () => import('../views/Protocols.vue'), meta: { title: '协议服务', section: '基础设施' } },
      { path: 'node-groups', component: () => import('../views/NodeGroups.vue'), meta: { title: '节点组', section: '基础设施' } },
      { path: 'traffic', component: () => import('../views/Traffic.vue'), meta: { title: '流量与对账', section: '基础设施' } },
      { path: 'tasks', component: () => import('../views/Tasks.vue'), meta: { title: '运营任务', section: '系统运营' } },
      { path: 'operation-logs', component: () => import('../views/OperationLogs.vue'), meta: { title: '运行日志', section: '系统运营' } },
      { path: 'audit-logs', component: () => import('../views/AuditLogs.vue'), meta: { title: '审计日志', section: '系统运营' } },
      { path: 'settings', component: () => import('../views/Settings.vue'), meta: { title: '系统设置', section: '系统运营' } },
      { path: 'about', component: () => import('../views/About.vue'), meta: { title: '关于 Zboard', section: '系统运营' } },
      { path: 'billing', redirect: '/admin/orders' }
    ]
  },
  { path: '/dashboard', redirect: '/admin/dashboard' },
  { path: '/nodes', redirect: '/admin/nodes' },
  { path: '/certificates', redirect: '/admin/certificates' },
  { path: '/providers', redirect: '/admin/providers' },
  { path: '/dns-records', redirect: '/admin/dns-records' },
  { path: '/protocols', redirect: '/admin/protocols' },
  { path: '/node-groups', redirect: '/admin/node-groups' },
  { path: '/plans', redirect: '/admin/plans' },
  { path: '/billing', redirect: '/admin/orders' },
  { path: '/orders', redirect: '/admin/orders' },
  { path: '/subscriptions', redirect: '/admin/subscriptions' },
  { path: '/fair-use', redirect: '/admin/fair-use' },
  { path: '/traffic', redirect: '/admin/traffic' },
  { path: '/users', redirect: '/admin/users' },
  { path: '/tickets', redirect: '/admin/tickets' },
  { path: '/tasks', redirect: '/admin/tasks' },
  { path: '/operation-logs', redirect: '/admin/operation-logs' },
  { path: '/subscription-templates', redirect: '/admin/subscription-templates' },
  { path: '/subscription-rule-sets', redirect: '/admin/subscription-templates/rule-sets' },
  { path: '/audit-logs', redirect: '/admin/audit-logs' },
  { path: '/settings', redirect: '/admin/settings' },
  { path: '/about', redirect: '/admin/about' },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]
