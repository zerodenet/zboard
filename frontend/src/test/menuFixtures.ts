import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { toAdminNavigation, resolveAdminNavigation as resolve } from '../utils/adminNavigation'
import type { MenuNode, MenuWorkspace } from '../api/menus'
import type { Surface } from '../api/plugins'

// Test inventory comes from the server seed; the browser bundle contains none.
export const menuNodes = JSON.parse(readFileSync(join(import.meta.dirname, '../../../backend/internal/adapters/persistence/menustore/defaults.json'), 'utf8')) as MenuNode[]
const presentation = JSON.parse(readFileSync(join(import.meta.dirname, '../../../backend/internal/navigation/presentation.json'), 'utf8')) as Record<string, { workspaces: MenuWorkspace[]; footer?: { node_id: string; parent_id: string; position: number }[] }>
export const menuFixture = (surface: Surface) => ({ revision: 1, page_available: true, nodes: menuNodes.filter(node => node.surface === surface).map(node => ({ ...node })).sort((a,b) => a.position-b.position || a.id.localeCompare(b.id)), presentation: { workspaces: presentation[surface]?.workspaces || [], footer_ids: (presentation[surface]?.footer || []).filter(item => menuNodes.some(node => node.id === item.node_id && node.parent_id === item.parent_id && node.position === item.position && node.surface === surface)).map(item => item.node_id) } })
export const adminNavigation = toAdminNavigation(menuFixture('admin').nodes, menuFixture('admin').presentation)
export const resolveAdminNavigation = (path: string) => resolve(path, adminNavigation)
