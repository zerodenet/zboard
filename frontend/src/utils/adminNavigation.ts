import type { MenuNode } from '../api/menus'
export interface AdminNavigationPage { to: string; label: string }
export interface AdminNavigationSection { id: string; label: string; icon: string; pages: AdminNavigationPage[] }
export interface AdminNavigationDomain {
  id: string
  label: string
  icon: string
  to?: string
  sections: AdminNavigationSection[]
}

// The backend owns inventory and visibility. This projection only chooses the
// admin presentation (sidebar groups and sibling page tabs).
export function toAdminNavigation(nodes: MenuNode[]): AdminNavigationDomain[] {
  const children = new Map<string, MenuNode[]>()
  for (const node of nodes) children.set(node.parent_id, [...(children.get(node.parent_id) || []), node])
  function pages(node: MenuNode, depth = 0): AdminNavigationPage[] {
    if (depth > 8) return []
    if (node.path) return [{ to: node.path, label: node.label }]
    return (children.get(node.id) || []).flatMap(child => pages(child, depth + 1))
  }
  return (children.get('') || []).map(root => ({
    id: root.id, label: root.label, icon: root.icon, to: root.path || undefined,
    sections: (root.path ? [root] : children.get(root.id) || []).map(node => ({
      id: node.id, label: node.label, icon: node.icon, pages: pages(node),
    })).filter(section => section.pages.length),
  })).filter(domain => domain.sections.length)
}

export function resolveAdminNavigation(path: string, domains: AdminNavigationDomain[]) {
  const pathname = path.split(/[?#]/, 1)[0]
  const entries = domains.flatMap(domain => domain.sections.flatMap(section =>
    section.pages.map(page => ({ domain, section, page })),
  )).sort((left, right) => right.page.to.length - left.page.to.length)
  return entries.find(({ page }) => pathname === page.to || pathname.startsWith(`${page.to}/`))
}
