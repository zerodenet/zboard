export const dashboardActionRoutes = {
  tickets: '/admin/tickets?status=attention',
  orders: '/admin/orders?status=attention',
  nodes: '/admin/nodes?connector=offline',
  tasks: '/admin/tasks?status=3',
  deployments: '/admin/protocols?deployment=failed',
} as const
