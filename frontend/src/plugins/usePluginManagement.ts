import { onScopeDispose, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchPlugins, pluginAction, type Plugin } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { confirmAction } from '../utils/feedback'

export const pluginDetailPath = (id: string) => `/admin/plugins/${encodeURIComponent(id)}`
export const hasPluginConfig = (p: Plugin) => p.manifest.capabilities.includes('zboard.config.v1') && p.state !== 'uninstalled'
export const actionLabels: Record<string, string> = {
  import: '导入', install: '安装', enable: '启用', disable: '停用', uninstall: '卸载', rollback: '恢复版本', purge: '删除配置', configure: '保存配置', config: '保存配置', test: '测试配置',
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
        message: action === 'purge' ? '永久删除插件保留的配置。核心用户、订单和审计记录不受影响。'
          : action === 'uninstall' ? '删除插件程序和页面，保留配置与操作记录。'
          : action === 'rollback' ? '恢复所选版本后保持停用；旧版本必须能够验证当前配置。'
          : untested ? '发布者尚未声明测试当前宿主版本。确认信任该插件后启用。'
          : action === 'disable' ? '停用后，插件页面和登录入口将不可用。已提交的核心业务仍由系统继续处理。'
          : '启用后，插件声明的页面和服务将立即可用。',
        tone: action === 'purge' || action === 'uninstall' ? 'danger' : 'primary',
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
