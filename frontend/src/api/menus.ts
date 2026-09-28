import { authenticatedAPI as api } from './client'
import type { Surface } from './plugins'

export interface MenuNode {
  id: string
  parent_id: string
  surface: Surface
  label: string
  icon: string
  path: string
  position: number
  hidden: boolean
  owner: 'core' | 'plugin' | 'custom'
  plugin_id: string
  page_id: string
  condition: string
}
export interface MenuSnapshot { revision: number; nodes: MenuNode[] }
export interface NavigationSnapshot extends MenuSnapshot { page_available?: boolean }
const data = <T extends MenuSnapshot>(r: { data: { data: T } }): T => r.data.data
export const fetchNavigation = (surface: Surface, signal?: AbortSignal, path?: string): Promise<NavigationSnapshot> => api.get('/navigation', { params: { surface, path }, signal }).then(data<NavigationSnapshot>)
export const fetchMenus = (surface: Surface, signal?: AbortSignal) => api.get('/admin/menus', { params: { surface }, signal }).then(data)
export const saveMenus = (surface: Surface, snapshot: MenuSnapshot) => api.put('/admin/menus', snapshot, { params: { surface } }).then(data)
