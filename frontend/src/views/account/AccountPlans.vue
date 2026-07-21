<template>
  <section class="account-page stack">
    <header class="account-page-header"><div><p class="page-eyebrow">PLANS</p><h1>选择套餐</h1><p>比较价格、周期、流量与设备限制，创建适合你的订单。</p></div><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />刷新</button></header>
    <div v-if="message" class="alert alert-success"><UiIcon name="check" />{{ message }}</div><div v-if="error" class="alert alert-danger"><UiIcon name="alert" />{{ error }}</div>
    <div v-if="plans.length" class="account-plan-list"><article v-for="plan in plans" :key="plan.id" class="panel"><div class="account-plan-copy"><span>{{ plan.slug }}</span><h2>{{ plan.name }}</h2><p>{{ plan.summary || plan.description || '稳定、透明的订阅服务。' }}</p></div><div class="account-sku-grid"><article v-for="sku in activeSkus(plan)" :key="sku.id"><div><strong>{{ sku.name }}</strong><span>{{ sku.sku_type === 'new' ? '新购' : sku.sku_type }}</span></div><p>{{ formatCurrency(sku.price_cents,sku.currency) }}<small>/ {{ billingLabel(sku) }}</small></p><ul><li><UiIcon name="check" />{{ formatBytes(sku.traffic_bytes) }} 流量</li><li><UiIcon name="check" />{{ sku.device_limit || '不限' }} 台设备</li><li><UiIcon name="check" />{{ sku.speed_limit_mbps ? `${sku.speed_limit_mbps} Mbps` : '不限速' }}</li></ul><button class="button button-sm" type="button" @click="askOrder(plan,sku)">选择此规格</button></article></div></article></div>
    <EmptyState v-else-if="!loading" icon="plans" title="暂无可售套餐" description="管理员发布套餐后会显示在这里。" />
    <ConfirmDialog :open="confirmOpen" title="创建新购订单？" :message="confirmMessage" confirm-text="确认创建" :busy="creating" @close="confirmOpen=false" @confirm="submitOrder" />
  </section>
</template>
<script setup lang="ts">
import { computed,onMounted,ref } from 'vue'; import { useRouter } from 'vue-router'; import { createOrder,fetchPlans } from '../../api/client'; import ConfirmDialog from '../../components/ConfirmDialog.vue'; import EmptyState from '../../components/EmptyState.vue'; import UiIcon from '../../components/UiIcon.vue'; import { formatBytes,formatCurrency } from '../../utils/format'
const router=useRouter();const plans=ref<any[]>([]);const loading=ref(false);const creating=ref(false);const error=ref('');const message=ref('');const confirmOpen=ref(false);const selected=ref<{plan:any;sku:any}|null>(null)
const confirmMessage=computed(()=>selected.value?`将按 ${formatCurrency(selected.value.sku.price_cents,selected.value.sku.currency)} 创建“${selected.value.plan.name} / ${selected.value.sku.name}”订单。订单创建后等待支付或人工确认。`:'')
function activeSkus(plan:any){return (plan.skus||[]).filter((sku:any)=>sku.is_active&&sku.sku_type==='new')} function billingLabel(sku:any){const unit=({day:'天',month:'月',year:'年',once:'次'} as Record<string,string>)[sku.billing_unit]||sku.billing_unit;return sku.billing_unit==='once'?'一次性':`${sku.billing_value} ${unit}`}
async function load(){loading.value=true;error.value='';try{plans.value=await fetchPlans()}catch(e:any){error.value=e?.response?.data?.message||'套餐加载失败。'}finally{loading.value=false}} function askOrder(plan:any,sku:any){selected.value={plan,sku};confirmOpen.value=true}
async function submitOrder(){if(!selected.value)return;creating.value=true;error.value='';try{await createOrder(selected.value.sku.id);confirmOpen.value=false;message.value='订单已创建，正在前往订单页面。';await router.push('/account/orders')}catch(e:any){error.value=e?.response?.data?.message||'订单创建失败。'}finally{creating.value=false}}
onMounted(load)
</script>
