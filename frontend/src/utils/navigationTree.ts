import type { MenuNode } from '../api/menus'

export function descendantPages(nodes: MenuNode[], parent: string, depth = 0): MenuNode[] {
  if (depth > 8) return []
  return nodes.filter(node => node.parent_id === parent).flatMap(node => node.path ? [node] : descendantPages(nodes, node.id, depth + 1))
}

export function currentMenuPage(nodes: MenuNode[], path: string) {
  return nodes.filter(node => node.path && (path === node.path || node.path !== '/account' && path.startsWith(`${node.path}/`)))
    .sort((a, b) => b.path.length - a.path.length)[0]
}

export function menuRoot(nodes: MenuNode[], node?: MenuNode): MenuNode | undefined {
  for (let depth = 0; node && depth <= 8; depth++) {
    if (!node.parent_id) return node
    node = nodes.find(candidate => candidate.id === node!.parent_id)
  }
}
