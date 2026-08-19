import { readFileSync, readdirSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')
const viewsRoot = join(sourceRoot, 'views')
const componentsRoot = join(sourceRoot, 'components')
const routerSource = readFileSync(join(sourceRoot, 'router', 'index.ts'), 'utf8')

function read(...parts: string[]) {
  return readFileSync(join(sourceRoot, ...parts), 'utf8')
}

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return vueFiles(path)
    return entry.name.endsWith('.vue') ? [path] : []
  })
}

const adminRoutes = {
  dashboard: ['views/Dashboard.vue'],
  users: ['views/Users.vue'],
  subscriptions: ['views/Subscriptions.vue'],
  'fair-use': ['views/FairUse.vue'],
  tickets: ['views/Tickets.vue', 'components/TicketCenter.vue'],
  plans: ['views/Plans.vue'],
  'subscription-templates': ['views/SubscriptionTemplates.vue'],
  'subscription-templates/rule-sets': ['views/SubscriptionRuleSets.vue'],
  orders: ['views/Orders.vue'],
  nodes: ['views/Nodes.vue'],
  protocols: ['views/Protocols.vue'],
  'node-groups': ['views/NodeGroups.vue'],
  traffic: ['views/Traffic.vue'],
  tasks: ['views/Tasks.vue'],
  'operation-logs': ['views/OperationLogs.vue'],
  'audit-logs': ['views/AuditLogs.vue'],
  settings: ['views/Settings.vue', 'components/SettingsConfigRow.vue'],
} as const

function surface(parts: readonly string[]) {
  return parts.map(part => read(...part.split('/'))).join('\n')
}

const adminListRoutes = Object.keys(adminRoutes).filter(route => !['dashboard', 'settings', 'fair-use'].includes(route))
const tabularAdminRoutes = adminListRoutes.filter(route => route !== 'tickets')

describe('frontend architecture policy', () => {
  it('keeps every effective admin route in the audited surface matrix', () => {
    for (const [route, files] of Object.entries(adminRoutes)) {
      expect(routerSource, `missing /admin/${route}`).toContain(
        `{ path: '${route}', component: () => import('../${files[0].replace('views/', 'views/')}')`,
      )
    }
    expect(Object.keys(adminRoutes)).toHaveLength(17)
    expect(routerSource).toContain("{ path: 'billing', redirect: '/admin/orders' }")
    expect(routerSource).toContain("{ path: 'subscription-rule-sets', redirect: '/admin/subscription-templates/rule-sets' }")
  })

  it('keeps rule sets inside the subscription-template information architecture', () => {
    const layout = read('layouts', 'AdminLayout.vue')
    const templates = read('views', 'SubscriptionTemplates.vue')
    const ruleSets = read('views', 'SubscriptionRuleSets.vue')
    const sectionNavigation = read('components', 'SubscriptionTemplateSectionNav.vue')
    const customizer = read('components', 'SubscriptionTemplateCustomizer.vue')

    expect(layout).not.toContain("to: '/admin/subscription-rule-sets'")
    expect(templates).toContain('<SubscriptionTemplateSectionNav section="templates"')
    expect(ruleSets).toContain('<SubscriptionTemplateSectionNav section="rule-sets"')
    expect(ruleSets).toContain('PageHeader title="规则集"')
    expect(ruleSets).not.toContain('订阅规则集')
    expect(sectionNavigation).toContain('<nav class="subscription-template-section-nav"')
    expect(sectionNavigation).toContain('<RouterLink')
    expect(sectionNavigation).toContain('aria-current')
    expect(sectionNavigation).not.toContain('<UiTabs')
    expect(customizer).toContain('model.policy_groups')
    expect(customizer).toContain('include_pattern')
    expect(customizer).toContain('$zboard:all-nodes')
    expect(customizer).not.toContain('标准分流')
    expect(customizer).not.toContain('subscriptionProfileOptions')
  })

  it('gives every admin surface shared page, feedback, status and time semantics', () => {
    for (const [route, files] of Object.entries(adminRoutes)) {
      const source = surface(files)
      expect(source, `${route} needs PageHeader`).toContain('<PageHeader')
      expect(source, `${route} needs application feedback`).toContain('<TransientFeedback')
      expect(source, `${route} needs semantic status labels`).toContain('<StatusBadge')
      expect(source, `${route} needs formatted time labels`).toContain('<TimeBadge')
    }

    const dashboard = surface(adminRoutes.dashboard)
    expect(dashboard).toContain('caption="最近协议配置发布结果"')
    expect(dashboard).not.toContain('deployment-dot')
  })

  it('keeps operational lists remote, URL-restorable and table-first', () => {
    for (const route of adminListRoutes) {
      const source = surface(adminRoutes[route as keyof typeof adminRoutes])
      expect(source, `${route} needs the shared workbench`).toContain('<DataWorkbench')
      expect(source, `${route} must use a remote table state owner`).toMatch(/use(?:Remote|Cursor)Table/)
      expect(source, `${route} must restore list state from the URL`).toContain('useRoute(')
      expect(source, `${route} must write list state to the URL`).toContain('useRouter(')
      expect(source, `${route} must use the shared filter bar`).toContain('<WorkbenchFilterBar')
      expect(source, `${route} must provide an explicit filter reset`).toContain('@clear=')
    }
    for (const route of tabularAdminRoutes) {
      expect(
        surface(adminRoutes[route as keyof typeof adminRoutes]),
        `${route} must use the shared table carrier`,
      ).toContain('<DataTable')
    }
    const tickets = surface(adminRoutes.tickets)
    expect(tickets).toContain('ticket-workspace')
    expect(tickets).toContain('<TablePager')
  })

  it('keeps account and public high-volume collections bounded on the server', () => {
    const accountLists = [
      read('views', 'account', 'AccountOrders.vue'),
      read('views', 'account', 'AccountPlans.vue'),
      read('views', 'account', 'AccountSubscription.vue'),
      read('views', 'account', 'AccountTraffic.vue'),
    ]
    for (const source of accountLists) {
      expect(source).toContain('<PageHeader')
      expect(source).toContain('<DataWorkbench')
      expect(source).toMatch(/use(?:Remote|Cursor)Table/)
      expect(source).toContain('useRoute(')
      expect(source).toContain('useRouter(')
      expect(source).toContain('<WorkbenchFilterBar')
    }

    const publicPlans = read('views', 'PublicPlans.vue')
    expect(publicPlans).toContain('useRemoteTable')
    expect(publicPlans).toContain('<TablePager')
    expect(publicPlans).not.toContain('plan.skus')

    const accountDashboard = read('views', 'account', 'AccountDashboard.vue')
    expect(accountDashboard).toContain('limit: 3')
    expect(accountDashboard).toContain('limit: 1')
  })

  it('keeps the user workbench on the bounded business-summary contract', () => {
    const users = read('views', 'Users.vue')
    const client = read('api', 'client.ts')

    expect(users).toContain('fetchUsersPage')
    expect(users).toContain('active_subscription_count')
    expect(users).toContain('total_subscription_count')
    expect(users).toContain('pending_order_count')
    expect(users).toContain('total_order_count')
    expect(users).toContain('<SortableHeader field="created_at"')
    expect(users).toContain('<TimeBadge :value="user.created_at"')
    expect(client).toContain("sort?: 'id' | 'email' | 'created_at'")
    expect(client).toContain("direction?: 'asc' | 'desc'")
    expect(users).not.toMatch(/\bfetchUsers\(/)
  })

  it('prevents private native controls and tables from bypassing shared contracts', () => {
    const violations = [...vueFiles(viewsRoot), ...vueFiles(componentsRoot)].flatMap(path => {
      const source = readFileSync(path, 'utf8')
      const file = relative(sourceRoot, path)
      const issues: string[] = []
      if (/<(?:input|textarea|select)\b/.test(source)) issues.push(`${file}: native form control`)
      if (/<table\b/.test(source) && path !== join(componentsRoot, 'DataTable.vue')) issues.push(`${file}: private table`)
      return issues
    })

    expect(violations).toEqual([])
  })
})
