export interface AdminNavigationPage { to: string; label: string }
export interface AdminNavigationSection { label: string; pages: AdminNavigationPage[] }
export interface AdminNavigationDomain {
  id: string
  label: string
  shortLabel: string
  icon: string
  description: string
  sections: AdminNavigationSection[]
}

// Navigation only: resource ownership, route guards and permissions remain unchanged.
export const adminNavigation: AdminNavigationDomain[] = [
  { id: 'overview', label: '概览', shortLabel: '概览', icon: 'dashboard', description: '业务动态，一目了然', sections: [
    { label: '工作台', pages: [{ to: '/admin/dashboard', label: '运营工作台' }] },
  ] },
  { id: 'customers', label: '用户与订阅', shortLabel: '客户', icon: 'users', description: '用户服务与权益管理', sections: [
    { label: '客户服务', pages: [
      { to: '/admin/users', label: '用户管理' },
      { to: '/admin/subscriptions', label: '订阅管理' },
      { to: '/admin/tickets', label: '工单中心' },
    ] },
    { label: '用量观测', pages: [{ to: '/admin/fair-use', label: 'Fair Use 观测' }] },
  ] },
  { id: 'commerce', label: '商品与订单', shortLabel: '商业', icon: 'plans', description: '商品销售与订阅交付', sections: [
    { label: '交易管理', pages: [
      { to: '/admin/plans', label: '商品与套餐' },
      { to: '/admin/orders', label: '订单管理' },
    ] },
    { label: '配置交付', pages: [
      { to: '/admin/subscription-templates', label: '订阅模板' },
      { to: '/admin/subscription-templates/rule-sets', label: '规则集' },
    ] },
  ] },
  { id: 'infrastructure', label: '节点与协议', shortLabel: '节点', icon: 'nodes', description: '基础资源与服务接入', sections: [
    { label: '服务资源', pages: [
      { to: '/admin/nodes', label: '节点资产' },
      { to: '/admin/protocols', label: '协议服务' },
      { to: '/admin/node-groups', label: '节点组' },
      { to: '/admin/traffic', label: '流量与对账' },
    ] },
    { label: '接入配置', pages: [
      { to: '/admin/providers', label: '外部供应商' },
      { to: '/admin/dns-records', label: 'DNS 解析' },
      { to: '/admin/certificates', label: '免费证书' },
    ] },
  ] },
  { id: 'operations', label: '运营', shortLabel: '运营', icon: 'activity', description: '消息发布与运行追踪', sections: [
    { label: '日常运营', pages: [
      { to: '/admin/announcements', label: '站点公告' },
      { to: '/admin/tasks', label: '运营任务' },
    ] },
    { label: '日志与审计', pages: [
      { to: '/admin/operation-logs', label: '运行日志' },
      { to: '/admin/audit-logs', label: '审计日志' },
    ] },
  ] },
  { id: 'settings', label: '设置', shortLabel: '设置', icon: 'settings', description: '站点配置与系统管理', sections: [
    { label: '站点设置', pages: [
      { to: '/admin/settings/site', label: '站点与品牌' },
      { to: '/admin/settings/registration', label: '注册与验证' },
      { to: '/admin/settings/email', label: '邮件与运营模板' },
      { to: '/admin/settings/legal', label: '法务与政策' },
    ] },
    { label: '系统管理', pages: [
      { to: '/admin/settings/runtime', label: '系统运行' },
      { to: '/admin/maintenance', label: '系统维护' },
      { to: '/admin/about', label: '关于 ZBoard' },
    ] },
  ] },
]

const entries = adminNavigation.flatMap(domain => domain.sections.flatMap(section =>
  section.pages.map(page => ({ domain, page })),
)).sort((left, right) => right.page.to.length - left.page.to.length)

export function resolveAdminNavigation(path: string) {
  const pathname = path.split(/[?#]/, 1)[0]
  // Longest match prevents both templates and their rule-set page being selected.
  return entries.find(({ page }) => pathname === page.to || pathname.startsWith(`${page.to}/`))
}
