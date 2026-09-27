<template>
  <div class="admin-build-info" aria-label="系统版本信息">
    <span class="build-version" :title="version">版本 {{ version }}</span>
    <span>构建时间 {{ buildTime }}</span>
    <div class="version-actions">
      <button type="button" :disabled="checking || !canCompare" @click="checkForUpdates(true)">
        {{ checking ? '检查中…' : update ? '发现新版本' : status === 'current' ? '已是最新' : status === 'error' ? '重试检查' : '检查更新' }}
      </button>
      <a :href="ZBOARD_RELEASES_URL" target="_blank" rel="noopener noreferrer">GitHub 发布</a>
    </div>
    <div v-if="update" class="version-update" role="status">
      <strong>{{ update.tag_name }}</strong>
      <div class="version-update-links">
        <a :href="update.html_url" target="_blank" rel="noopener noreferrer">更新说明</a>
        <a :href="ZBOARD_DEPLOYMENT_GUIDE_URL" target="_blank" rel="noopener noreferrer">部署文档</a>
      </div>
    </div>
    <span v-else-if="status === 'error'" class="version-error" role="status">暂时无法检查，仍可查看 GitHub 发布。</span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AdminSystemInfo } from '../api/system'
import { compareZBoardVersions, fetchZBoardReleases, latestEligibleRelease, releaseChannelForVersion, ZBOARD_DEPLOYMENT_GUIDE_URL, ZBOARD_RELEASES_URL, type ZBoardRelease } from '../api/releaseUpdates'

const props = defineProps<{ info: AdminSystemInfo | null }>()
const version = computed(() => props.info?.release_version || props.info?.version || '—')
const canCompare = computed(() => compareZBoardVersions(version.value, version.value) !== null)
const buildTime = computed(() => {
  const value = props.info?.build_time
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
})
const checking = ref(false)
const status = ref<'idle' | 'current' | 'error'>('idle')
const update = ref<ZBoardRelease | null>(null)
let requestId = 0

async function checkForUpdates(force = false) {
  if (!canCompare.value || checking.value) return
  const id = ++requestId
  const current = version.value
  const channel = props.info?.release_channel || releaseChannelForVersion(current)
  checking.value = true
  status.value = 'idle'
  try {
    const releases = await fetchZBoardReleases(channel, force)
    if (id !== requestId) return
    update.value = latestEligibleRelease(releases, current, channel)
    status.value = update.value ? 'idle' : 'current'
  } catch {
    if (id !== requestId) return
    update.value = null
    status.value = 'error'
  } finally {
    if (id === requestId) checking.value = false
  }
}

watch(version, () => { update.value = null; status.value = 'idle'; void checkForUpdates() }, { immediate: true })
</script>

<style scoped>
.admin-build-info { display: grid; gap: 4px; padding: 12px 20px 14px; border-top: 1px solid var(--border); color: var(--muted-foreground); font-size: 11px; line-height: 1.35; font-variant-numeric: tabular-nums; }
.build-version { overflow: hidden; color: var(--text-secondary); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.version-actions, .version-update-links { display: flex; align-items: center; gap: 12px; }
.version-actions { margin-top: 3px; }
.version-actions button { padding: 0; border: 0; background: transparent; color: var(--primary); font: inherit; cursor: pointer; }
.version-actions button:disabled { color: var(--muted-foreground); cursor: default; }
a { color: var(--text-secondary); text-decoration: none; }
a:hover, .version-actions button:not(:disabled):hover { color: var(--foreground); text-decoration: underline; }
a:focus-visible, button:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; border-radius: 2px; }
.version-update { display: grid; gap: 3px; margin-top: 3px; padding: 7px 8px; border-radius: 6px; background: var(--muted-surface); }
.version-update strong { overflow: hidden; color: var(--foreground); font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.version-error { line-height: 1.4; }
</style>
