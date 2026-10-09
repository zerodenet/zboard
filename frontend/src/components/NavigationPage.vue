<template>
  <section v-if="loading" class="navigation-page-state" role="status">正在加载页面…</section>
  <section v-else-if="error || !available" class="navigation-page-state" role="alert">
    <div class="navigation-state-icon"><UiIcon :name="failure === 'network' ? 'alert' : 'shield'" /></div>
    <h1>{{ title }}</h1>
    <p>{{ description }}</p>
    <div class="navigation-state-actions">
      <RouterLink v-if="failure === 'session-expired'" class="button" :to="{ path: '/login', query: { redirect: route.fullPath, reason: 'session-expired' } }">重新登录</RouterLink>
      <UiButton v-else variant="secondary" :loading="retrying" @click="$emit('retry')">{{ error && failure !== 'forbidden' ? '重试' : '重新检查授权' }}</UiButton>
      <RouterLink class="button button-ghost" :to="route.path.startsWith('/admin') ? '/account' : '/'">{{ route.path.startsWith('/admin') ? '返回个人中心' : '返回首页' }}</RouterLink>
    </div>
  </section>
  <slot v-else />
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import type { NavigationFailure } from '../stores/navigation'
const props = defineProps<{ available: boolean; loading: boolean; error: string; failure?: NavigationFailure; retrying?: boolean }>()
defineEmits<{ retry: [] }>()
const route = useRoute()
const title = computed(() => props.failure === 'session-expired' ? '登录已过期'
  : props.failure === 'forbidden' ? '访问权限不足'
  : props.error ? '暂时无法加载页面' : '页面不可用')
const description = computed(() => props.failure === 'session-expired' ? '请重新登录以继续操作，登录后会返回当前页面。'
  : props.failure === 'forbidden' ? '当前账户暂时无法访问此页面。授权可能已到期或被撤回，请联系管理员确认，恢复后可重新检查。'
  : props.error ? props.error : '此页面已隐藏、扩展已停用或访问授权已变化。请联系管理员确认，或返回其他页面继续操作。')
</script>
<style scoped>
.navigation-page-state { width: 100%; max-width: 560px; margin: 48px auto; padding: 40px 24px; text-align: center; color: var(--muted-foreground); }
.navigation-state-icon { display: inline-grid; place-items: center; width: 48px; height: 48px; margin-bottom: 20px; border: 1px solid var(--border); border-radius: 12px; background: var(--surface-subtle); }
.navigation-state-icon .ui-icon { width: 22px; height: 22px; }
.navigation-page-state h1 { margin: 0 0 12px; color: var(--foreground); font-size: 20px; }
.navigation-page-state p { margin: 0 auto 24px; max-width: 420px; line-height: 1.7; font-size: 14px; }
.navigation-state-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 12px; }
@media (max-width: 560px) { .navigation-page-state { margin-block: 24px; padding-inline: 12px; } }
</style>
