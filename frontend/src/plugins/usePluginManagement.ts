import { onScopeDispose, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchPlugins, pluginAction, type Plugin } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { confirmAction } from '../utils/feedback'

export const pluginDetailPath = (id: string) => `/admin/plugins/${encodeURIComponent(id)}`
export const capabilityGranted = (p: Plugin, c: string) => !!p.authorization?.reviewed && p.authorization.granted.includes(c) && p.manifest.capabilities.includes(c)
export const canPluginRun = (p: Plugin) => !!p.authorization?.reviewed && (!p.manifest.components.server || (p.authorization.native_trusted && capabilityGranted(p, 'zboard.config.v1'))) && !!p.data?.compatible && !p.data.migration_required
export const hasPluginConfig = (p: Plugin) => capabilityGranted(p, 'zboard.config.v1') && p.state !== 'uninstalled'
export const actionLabels: Record<string, string> = {
  authorize: '更新授权', migrate: '迁移数据', purge_data: '清除插件数据', import: '导入', install: '安装', enable: '启用', disable: '停用', uninstall: '卸载', rollback: '恢复版本', purge: '删除配置', configure: '保存配置', config: '保存配置', test: '测试配置',
}

export function usePluginDetail() {
  const route = useRoute()
  const resource = useRemoteResource<Plugin | null>({
    initial: () => null,
    fetch: async ({ signal }) => {
      const id = String(route.params.pluginId)
      return (await fetchPlugins(signal)).find(p => p.id === id) || null
    },
    errorMessage: '无法读取插件详情，请重试。',
  })
  watch(() => route.params.pluginId, () => { resource.reset(); void resource.load() }, { immediate: true })
  return resource
}

export function usePluginActions(refresh: () => Promise<unknown>) {
  const route = useRoute()
  const busy = ref(false), error = ref(''), message = ref('')
  let disposed = false
  onScopeDispose(() => { disposed = true })
  async function act(plugin: Plugin, action: string, versionID = '') {
    if (busy.value) return
    busy.value = true
    const pluginId = route.params.pluginId
    const current = () => !disposed && route.params.pluginId === pluginId
    error.value = ''; message.value = ''
    const untested = action === 'enable' && !plugin.compatibility.tested
    try {
      const confirmed = await confirmAction({
        title: `${actionLabels[action]} ${plugin.name}`,
        message: action === 'migrate' ? '按当前插件包声明迁移自身配置和私有数据。成功后保持停用；迁移后不兼容的数据版本将无法直接恢复旧程序。'
          : action === 'purge_data' ? '永久清除该插件配置和私有数据，保留核心账户、第三方身份绑定、操作与迁移记录。'
          : action === 'purge' ? '永久删除插件保留的配置。核心用户、订单和审计记录不受影响。'
          : action === 'uninstall' ? '删除插件程序和页面，保留配置、私有数据与操作记录。'
          : action === 'rollback' ? '恢复所选版本后保持停用；旧版本必须兼容当前数据。重新审查授权后，启用时校验配置。'
          : untested ? '发布者尚未声明测试当前宿主版本。确认信任该插件后启用。'
          : action === 'disable' ? '停用后，插件页面和登录入口将不可用。已提交的核心业务仍由系统继续处理。'
          : '启用后，已授权的插件页面和服务将可用。',
        tone: action === 'purge_data' || action === 'purge' || action === 'uninstall' ? 'danger' : 'primary',
        confirmText: actionLabels[action],
      })
      if (!confirmed || !current()) return
      await pluginAction(plugin, action, untested, versionID)
      if (!current()) return
      message.value = `${actionLabels[action]}已完成。`
      await refresh()
    } catch (cause: any) {
      if (current()) error.value = cause?.response?.data?.message || '插件操作失败，请刷新状态后重试。'
    } finally { busy.value = false }
  }
  return { busy, error, message, act }
}
