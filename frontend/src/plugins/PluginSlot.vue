<template>
  <PluginLoginMethods v-if="name === 'auth.login.methods' && surface === 'public'" />
  <section v-else-if="contributions.length || !state.loaded" class="plugin-slot" :data-plugin-slot="name" :aria-busy="state.loading || undefined">
    <div v-if="!state.loaded && !state.error" class="plugin-slot-placeholder" :style="{height: `${fallbackHeight}px`}" role="status" aria-label="正在读取扩展区域"><span /></div>
    <div v-else-if="!contributions.length && state.error" class="plugin-slot-unavailable" role="status"><span>扩展区域暂不可用</span><UiButton variant="ghost" size="sm" @click="reload">重试</UiButton></div>
    <template v-for="contribution in contributions" :key="`${contribution.plugin_id}:${contribution.slot.id}:${contribution.generation}:${contribution.config_revision || 0}:${targetUserId || 0}`">
      <div v-if="contribution.availability === 'unavailable'" class="plugin-slot-unavailable" :style="{height:`${contribution.reserved_height || fallbackHeight}px`}" role="status"><span>{{ contribution.slot.title }}暂不可用</span><UiButton variant="ghost" size="sm" @click="reload">重试</UiButton></div>
      <PluginFrame v-else :plugin-id="contribution.plugin_id" :slot="contribution.slot" :surface="surface" :target-user-id="targetUserId" :title="contribution.slot.title" :reserved-height="contribution.reserved_height" />
    </template>
    <div v-if="contributions.length && state.error" class="plugin-slot-refresh-error" role="status"><span>扩展目录刷新失败</span><UiButton variant="ghost" size="sm" @click="reload">重试</UiButton></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useAppStore } from '../stores/app'
import type { Surface } from '../api/plugins'
import { compositionState, preparePluginComposition } from './composition'
import PluginFrame from './PluginFrame.vue'
import PluginLoginMethods from './PluginLoginMethods.vue'
import UiButton from '../components/UiButton.vue'
const props=defineProps<{ name:string;surface:Surface;targetUserId?:number;optional?:boolean }>()
const app=useAppStore()
const state=computed(()=>compositionState(props.surface,app.token).state)
const contributions=computed(()=>state.value.slots.filter(item=>item.slot.slot===props.name))
const fallbackHeight=computed(()=>['account.overview.cards','account.security.identities','admin.user.identities'].includes(props.name)?224:120)
function reload(){return preparePluginComposition(props.surface,true,app.token)}
watch(()=>[props.surface,app.token,app.user?.id],()=>{void preparePluginComposition(props.surface,false,app.token)})
onMounted(()=>{void preparePluginComposition(props.surface,false,app.token)})
</script>
<style scoped>
.plugin-slot { display: grid; gap: 12px; min-width: 0; }
.plugin-slot-placeholder { background: var(--surface-neutral); border-radius: 8px; }
.plugin-slot-unavailable { display: flex; justify-content: center; align-items: center; gap: 12px; color: var(--muted-foreground); background: var(--surface-neutral); border-radius: 8px; font-size: 13px; padding: 12px; box-sizing: border-box; }
.plugin-slot-refresh-error { display: flex; align-items: center; justify-content: space-between; font-size: 12px; color: var(--muted-foreground); }
</style>
