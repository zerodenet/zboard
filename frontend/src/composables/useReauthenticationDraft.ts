import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '../stores/app'
import { fetchNavigation } from '../api/menus'
import { approveReauthenticationDraftPage, clearReauthenticationDrafts, hasReauthenticationDraft, registerReauthenticationDraft, takeReauthenticationDraft } from '../utils/reauthenticationDrafts'

export function useReauthenticationDraft<T>(options: {
  scope: string; permission: 'account' | 'admin'; ready: () => boolean
  read: () => unknown | null; sanitize: (value: unknown) => T | undefined
  restore: (value: T) => void | boolean
}) {
  const app = useAppStore(), route = useRoute()
  const message = ref('')
  const owner = app.user?.id || 0
  const unregister = registerReauthenticationDraft({ actorID: owner, scope: options.scope, permission: options.permission, route: () => route.fullPath, read: options.read, sanitize: options.sanitize })
  let disposed = false, busy = false
  async function restore() {
    if (disposed || busy || !options.ready() || !app.token || app.reauthRequired || !hasReauthenticationDraft(app.user.id, options.scope, route.fullPath, options.permission)) return
    const actor = app.user.id, token = app.token, epoch = app.sessionEpoch, path = route.fullPath
    busy = true
    try {
      const navigation = await fetchNavigation(options.permission, undefined, route.path)
      if (disposed || token !== app.token || actor !== app.user.id || epoch !== app.sessionEpoch || path !== route.fullPath || app.reauthRequired) return
      approveReauthenticationDraftPage(actor, path, navigation.page_available === true)
      if (!navigation.page_available) { message.value = '当前页面权限已变化，原草稿已清除。'; return }
      const value = takeReauthenticationDraft(actor, options.scope, path, options.permission, options.sanitize)
      if (value !== undefined) {
        const applied = options.restore(value)
        message.value = applied === false ? '草稿与当前资源不匹配，未恢复任何内容。' : '已恢复同一账户的未提交内容。请核对后自行保存或发送；附件需重新选择。'
      }
    } catch (cause: any) {
      if (disposed || token !== app.token || actor !== app.user.id || epoch !== app.sessionEpoch) return
      if (cause?.response?.status === 403) { clearReauthenticationDrafts(); message.value = '当前页面权限不足，原草稿已清除。' }
      else message.value = '暂时无法核对草稿访问权限，尚未恢复。可重试恢复；未提交任何操作。'
    } finally { busy = false }
  }
  watch(() => [app.token, app.user?.id, app.reauthRequired, options.ready(), route.fullPath], () => void restore(), { immediate: true, flush: 'post' })
  onBeforeUnmount(() => { disposed = true; unregister() })
  return { message, restore }
}
