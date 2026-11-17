import { computed, onMounted, onUnmounted, watch, type Ref } from 'vue'
import { fetchUserTrafficActivity, type UserTrafficActivityPage } from '../api/userTrafficActivity'
import { useRemoteResource } from './useRemoteResource'

export function useUserTrafficActivity(users: Ref<{ id: number }[]>, listLoading: Ref<boolean>, identity: () => string) {
  const resource = useRemoteResource<UserTrafficActivityPage | null>({
    initial: () => null,
    fetch: ({signal}) => fetchUserTrafficActivity(users.value.map(user => user.id), signal),
    errorMessage: '流量统计读取失败，请重试。',
  })
  let scope = ''
  watch([() => users.value.map(user => user.id).join(','), listLoading, identity], ([ids, loading, actor]) => {
    const next = `${actor}:${ids}`
    if (scope !== next) { resource.reset(); scope = next }
    if (ids && !loading) void resource.load()
  }, { immediate: true })
  function refreshVisible() {
    if (!document.hidden && users.value.length && !listLoading.value && !resource.loading.value && resource.canAutoRefresh.value) void resource.load()
  }
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => { timer = setInterval(refreshVisible, 60_000) })
  onUnmounted(() => { clearInterval(timer) })
  const byUser = computed(() => new Map(resource.data.value?.items.map(item => [item.user_id, item]) || []))
  return { ...resource, byUser }
}
