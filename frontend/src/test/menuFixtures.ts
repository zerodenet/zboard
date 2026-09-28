import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { toAdminNavigation, resolveAdminNavigation as resolve } from '../utils/adminNavigation'
import type { MenuNode } from '../api/menus'
import type { Surface } from '../api/plugins'

// Test inventory comes from the server seed; the browser bundle contains none.
export const menuNodes = JSON.parse(readFileSync(join(import.meta.dirname, '../../../backend/internal/adapters/persistence/menustore/defaults.json'), 'utf8')) as MenuNode[]
export const menuFixture = (surface: Surface) => ({ revision: 1, page_available: true, nodes: menuNodes.filter(node => node.surface === surface).map(node => ({ ...node })) })
export const adminNavigation = toAdminNavigation(menuFixture('admin').nodes)
export const resolveAdminNavigation = (path: string) => resolve(path, adminNavigation)
