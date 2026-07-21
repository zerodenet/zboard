<template>
  <section>
    <PageHeader title="商品与套餐" description="商品承载展示与权限边界，SKU 定义价格、周期、流量和设备规格。" eyebrow="Catalog">
      <template #actions>
        <button class="button button-secondary" type="button" :disabled="loading" @click="refresh"><UiIcon name="refresh" />刷新</button>
        <button v-if="app.isAdmin" class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />创建商品</button>
      </template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="catalog-summary">
      <div><span>商品总数</span><strong>{{ plans.length }}</strong></div>
      <div><span>已发布</span><strong>{{ activePlanCount }}</strong></div>
      <div><span>可售 SKU</span><strong>{{ activeSkuCount }}</strong></div>
      <div><span>节点组</span><strong>{{ nodeGroups.length }}</strong></div>
    </div>

    <div v-if="plans.length" class="plan-list">
      <article v-for="plan in plans" :key="plan.id" class="panel plan-card">
        <header class="plan-header">
          <div class="plan-identity">
            <span class="plan-icon"><UiIcon name="plans" /></span>
            <div><div class="title-line"><h2>{{ plan.name }}</h2><StatusBadge :tone="plan.is_active ? 'success' : 'neutral'">{{ plan.is_active ? '已发布' : '草稿' }}</StatusBadge></div><p>{{ plan.summary || '尚未填写商品摘要' }}</p><span class="slug mono">{{ plan.slug || `plan-${plan.id}` }}</span></div>
          </div>
          <div v-if="app.isAdmin" class="plan-actions">
            <button class="button button-secondary button-sm" type="button" @click="openGroupBinding(plan)"><UiIcon name="nodes" />选择节点组</button>
            <button class="button button-sm" :class="plan.is_active ? 'button-danger' : ''" type="button" @click="toggleActive(plan)">{{ plan.is_active ? '转为草稿' : '发布商品' }}</button>
          </div>
        </header>

        <div v-if="plan.description" class="plan-description">{{ plan.description }}</div>
        <div class="plan-meta">
          <span><UiIcon name="nodes" />{{ plan.node_group?.name || groupName(plan.node_group_id) }}</span>
          <span><UiIcon name="activity" />{{ groupEndpointCount(plan.node_group_id) }} 个协议端点</span>
          <span><UiIcon name="users" />{{ plan.max_active_subscriptions || '不限' }} 最大订阅</span>
          <span><UiIcon name="refresh" />{{ plan.is_renewable ? '支持续费' : '不支持续费' }}</span>
          <span><UiIcon name="users" />家庭共享 {{ plan.family_limit ? `${plan.family_limit} 人` : '关闭' }}</span>
        </div>

        <div class="sku-section">
          <div class="sku-heading"><h3>销售规格</h3><span>{{ plan.skus?.length || 0 }} 个 SKU</span></div>
          <div v-if="plan.skus?.length" class="sku-grid">
            <article v-for="sku in plan.skus" :key="sku.id" class="sku-card" :class="{ inactive: !sku.is_active }">
              <div class="sku-top"><div><span class="sku-type">{{ skuTypeLabel(sku.sku_type) }}</span><h4>{{ sku.name }}</h4><code>{{ sku.code }}</code></div><StatusBadge :tone="sku.is_active ? 'success' : 'neutral'">{{ sku.is_active ? '可售' : '停用' }}</StatusBadge></div>
              <p class="sku-price">{{ formatCurrency(sku.price_cents, sku.currency) }}<span>/ {{ billingLabel(sku) }}</span></p>
              <dl>
                <div><dt>流量</dt><dd>{{ formatBytes(sku.traffic_bytes) }}</dd></div>
                <div><dt>设备</dt><dd>{{ sku.device_limit }} 台</dd></div>
                <div><dt>速率</dt><dd>{{ sku.speed_limit_mbps ? `${sku.speed_limit_mbps} Mbps` : '不限速' }}</dd></div>
              </dl>
              <div class="sku-actions">
                <button v-if="app.isAdmin" class="button button-secondary button-sm" type="button" @click="openSku(sku)"><UiIcon name="edit" />编辑规格</button>
              </div>
            </article>
          </div>
          <EmptyState v-else icon="plans" title="没有销售规格" description="该商品尚未配置可售 SKU。" />
        </div>
      </article>
    </div>
    <article v-else class="panel"><EmptyState icon="plans" title="还没有商品" description="先准备节点组，再创建商品和销售 SKU。"><template v-if="app.isAdmin" #actions><button class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />创建商品</button></template></EmptyState></article>

    <ModalDialog :open="createOpen" title="创建商品与首个 SKU" description="先建立一个可售规格，后续可以独立调整 SKU 状态和规格。" size="xl" :busy="saving" @close="createOpen = false">
      <form id="create-plan-form" class="stack" @submit.prevent="create">
        <section class="form-section"><div class="form-section-title"><span>1</span><div><h3>商品信息</h3><p>用于目录展示和内部识别。</p></div></div><div class="form-grid form-grid-3"><label class="field"><span>商品名称</span><input v-model.trim="form.name" required placeholder="基础套餐" /></label><label class="field"><span>Slug</span><input v-model.trim="form.slug" required placeholder="starter" /></label><label class="field"><span>摘要</span><input v-model.trim="form.summary" maxlength="255" placeholder="适合个人日常使用" /></label><label class="field field-full"><span>详细说明</span><textarea v-model="form.description" rows="3"></textarea></label></div></section>
        <section class="form-section"><div class="form-section-title"><span>2</span><div><h3>首个销售规格</h3><p>定义价格、计费周期和服务限制。</p></div></div><div class="form-grid form-grid-3"><label class="field"><span>SKU 名称</span><input v-model.trim="form.sku.name" required placeholder="月付" /></label><label class="field"><span>SKU 编码</span><input v-model.trim="form.sku.code" required placeholder="starter-monthly" /></label><label class="field"><span>规格类型</span><select v-model="form.sku.sku_type"><option value="new">新购</option><option value="renewal">续费</option><option value="upgrade">升级</option><option value="traffic_pack">流量包</option></select></label><label class="field"><span>计费单位</span><select v-model="form.sku.billing_unit"><option value="day">天</option><option value="month">月</option><option value="year">年</option><option value="once">一次性</option></select></label><label class="field"><span>周期数量</span><input v-model.number="form.sku.billing_value" type="number" min="1" required /></label><label class="field"><span>价格（分）</span><input v-model.number="form.sku.price_cents" type="number" min="0" required /><small class="field-hint">{{ formatCurrency(form.sku.price_cents, form.sku.currency) }}</small></label><label class="field"><span>币种</span><input v-model.trim="form.sku.currency" maxlength="8" required /></label><label class="field"><span>流量（GiB）</span><input v-model.number="form.traffic_gib" type="number" min="1" required /></label><label class="field"><span>设备数</span><input v-model.number="form.sku.device_limit" type="number" min="1" required /></label><label class="field"><span>速率限制 Mbps</span><input v-model.number="form.sku.speed_limit_mbps" type="number" min="0" /><small class="field-hint">0 表示不限速。</small></label><label class="field"><span>最大有效订阅</span><input v-model.number="form.max_active_subscriptions" type="number" min="0" /></label><label class="field"><span>家庭共享人数</span><input v-model.number="form.family_limit" type="number" min="0" /></label></div></section>
        <section class="form-section"><div class="form-section-title"><span>3</span><div><h3>流量与交付边界</h3><p>套餐只关联节点组；协议端点及倍率由节点组和协议服务统一维护。</p></div></div><div class="form-grid form-grid-3"><label class="field"><span>节点组</span><select v-model.number="form.node_group_id" required><option disabled :value="0">请选择节点组</option><option v-for="group in enabledNodeGroups" :key="group.id" :value="group.id">{{ group.name }}（{{ group.protocol_endpoint_ids?.length || 0 }} 个端点）</option></select><small v-if="!enabledNodeGroups.length" class="field-hint">请先在“节点组”页面创建并启用节点组。</small></label><label class="field"><span>流量重置</span><select v-model.number="form.reset_policy"><option :value="0">跟随系统</option><option :value="1">每月 1 日</option><option :value="2">按购买日每月</option><option :value="3">每年 1 月 1 日</option><option :value="4">按购买日每年</option><option :value="5">不重置</option></select></label><label class="field"><span>消耗计算</span><select v-model.number="form.traffic_calc_mode"><option :value="0">上行 + 下行</option><option :value="1">仅上行</option><option :value="2">仅下行</option></select></label><label class="check-field"><input v-model="form.is_active" type="checkbox" /><span>创建后立即发布商品</span></label><label class="check-field"><input v-model="form.is_renewable" type="checkbox" /><span>允许用户续费</span></label></div></section>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="createOpen = false">取消</button><button class="button" form="create-plan-form" type="submit" :disabled="saving">{{ saving ? '创建中…' : '创建商品与 SKU' }}</button></template>
    </ModalDialog>

    <ModalDialog :open="skuOpen" title="编辑销售规格" description="修改会影响后续新订单，历史订单仍保留商业快照。" size="lg" :busy="saving" @close="skuOpen = false">
      <form id="sku-form" class="form-grid form-grid-3" @submit.prevent="saveSKU">
        <label class="field"><span>规格名称</span><input v-model.trim="skuDraft.name" required /></label><label class="field"><span>SKU 编码</span><input v-model.trim="skuDraft.code" required /></label><label class="field"><span>价格（分）</span><input v-model.number="skuDraft.price_cents" type="number" min="0" /></label><label class="field"><span>流量（字节）</span><input v-model.number="skuDraft.traffic_bytes" type="number" min="1" /></label><label class="field"><span>设备数</span><input v-model.number="skuDraft.device_limit" type="number" min="1" /></label><label class="field"><span>速率 Mbps</span><input v-model.number="skuDraft.speed_limit_mbps" type="number" min="0" /></label><label class="check-field field-full"><input v-model="skuDraft.is_active" type="checkbox" /><span>该 SKU 可用于创建新订单</span></label>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="skuOpen = false">取消</button><button class="button" form="sku-form" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存规格' }}</button></template>
    </ModalDialog>

    <ModalDialog :open="groupBindingOpen" title="选择节点组" :description="selectedPlan ? `套餐 ${selectedPlan.name} 将通过节点组获得协议端点。` : ''" size="md" :busy="saving" @close="groupBindingOpen = false">
      <label class="field"><span>节点组</span><select v-model.number="selectedGroupID" required><option disabled :value="0">请选择节点组</option><option v-for="group in enabledNodeGroups" :key="group.id" :value="group.id">{{ group.name }}（{{ group.protocol_endpoint_ids?.length || 0 }} 个端点）</option></select></label>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="groupBindingOpen = false">取消</button><button class="button" type="button" :disabled="saving || !selectedGroupID" @click="saveGroupBinding">{{ saving ? '保存中…' : '保存节点组' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { createPlan, fetchNodeGroups, fetchPlansWithOptions, updatePlan, updatePlanSKU } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'

const app = useAppStore()
const plans = ref<any[]>([])
const nodeGroups = ref<any[]>([])
const error = ref('')
const message = ref('')
const loading = ref(false)
const saving = ref(false)
const createOpen = ref(false)
const skuOpen = ref(false)
const groupBindingOpen = ref(false)
const selectedPlan = ref<any>(null)
const selectedGroupID = ref(0)
const skuDraft = reactive<any>({})
const activePlanCount = computed(() => plans.value.filter(plan => plan.is_active).length)
const activeSkuCount = computed(() => plans.value.flatMap(plan => plan.skus || []).filter(sku => sku.is_active).length)
const enabledNodeGroups = computed(() => nodeGroups.value.filter(group => group.is_enabled))
const form = reactive({ name: '', slug: '', summary: '', description: '', is_active: false, traffic_gib: 100, node_group_id: 0, max_active_subscriptions: 0, is_renewable: true, family_limit: 0, reset_policy: 0, traffic_calc_mode: 0, sku: { code: '', name: '', sku_type: 'new', billing_unit: 'month', billing_value: 1, price_cents: 0, currency: 'CNY', device_limit: 3, speed_limit_mbps: 0 } })

function skuTypeLabel(type: string) { return ({ new: '新购', renewal: '续费', upgrade: '升级', traffic_pack: '流量包' } as Record<string, string>)[type] || type }
function billingLabel(sku: any) { const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit; return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}` }
function groupName(id: number) { return nodeGroups.value.find(group => group.id === id)?.name || `节点组 #${id}` }
function groupEndpointCount(id: number) { return nodeGroups.value.find(group => group.id === id)?.protocol_endpoint_ids?.length || 0 }

async function refresh() {
  loading.value = true; error.value = ''
  try {
    plans.value = await fetchPlansWithOptions({ includeInactive: app.isAdmin })
    if (app.isAdmin) nodeGroups.value = await fetchNodeGroups()
  } catch (e: any) { error.value = e?.response?.data?.message || '商品数据加载失败。' }
  finally { loading.value = false }
}

async function create() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    await createPlan({ name: form.name, slug: form.slug, summary: form.summary, description: form.description, is_active: form.is_active, node_group_id: form.node_group_id, traffic_bytes: Math.round(form.traffic_gib * 1024 ** 3), speed_limit_mbps: form.sku.speed_limit_mbps, device_limit: form.sku.device_limit, max_active_subscriptions: form.max_active_subscriptions, is_renewable: form.is_renewable, family_limit: form.family_limit, reset_policy: form.reset_policy, traffic_calc_mode: form.traffic_calc_mode, skus: [{ ...form.sku, traffic_bytes: Math.round(form.traffic_gib * 1024 ** 3), is_active: true }] })
    Object.assign(form, { name: '', slug: '', summary: '', description: '', is_active: false, node_group_id: 0 })
    createOpen.value = false; message.value = '商品和首个 SKU 已创建。'; await refresh()
  } catch (e: any) { error.value = e?.response?.data?.message || '商品创建失败。' }
  finally { saving.value = false }
}

async function toggleActive(plan: any) { try { await updatePlan(plan.id, { is_active: !plan.is_active }); message.value = plan.is_active ? '商品已转为草稿。' : '商品已发布。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '商品状态更新失败。' } }
function openSku(sku: any) { Object.keys(skuDraft).forEach(key => delete skuDraft[key]); Object.assign(skuDraft, JSON.parse(JSON.stringify(sku))); skuOpen.value = true }
async function saveSKU() { saving.value = true; try { await updatePlanSKU(skuDraft.id, skuDraft); skuOpen.value = false; message.value = `SKU ${skuDraft.code} 已保存。`; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || 'SKU 保存失败。' } finally { saving.value = false } }
function openGroupBinding(plan: any) { selectedPlan.value = plan; selectedGroupID.value = plan.node_group_id || 0; groupBindingOpen.value = true }
async function saveGroupBinding() { if (!selectedPlan.value || !selectedGroupID.value) return; saving.value = true; try { await updatePlan(selectedPlan.value.id, { node_group_id: selectedGroupID.value }); groupBindingOpen.value = false; message.value = '套餐节点组已更新。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '节点组更新失败。' } finally { saving.value = false } }

onMounted(refresh)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.catalog-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 16px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface); }.catalog-summary > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 15px 18px; }.catalog-summary > div + div { border-left: 1px solid var(--line); }.catalog-summary span { color: var(--muted); font-size: 12px; }.catalog-summary strong { font-size: 20px; }.plan-list { display: grid; gap: 16px; }.plan-card { overflow: hidden; }.plan-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; padding: 20px; }.plan-identity { display: flex; gap: 13px; min-width: 0; }.plan-icon { width: 42px; height: 42px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 11px; color: var(--primary); background: var(--primary-soft); font-size: 20px; }.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 9px; }.title-line h2 { margin: 0; font-size: 18px; }.plan-identity p { margin: 5px 0; color: var(--muted); font-size: 12px; }.slug { color: var(--subtle); font-size: 10px; }.plan-actions { display: flex; flex-wrap: wrap; gap: 8px; }.plan-description { margin: 0 20px 16px; padding: 13px 15px; border-radius: 9px; color: var(--muted); background: var(--surface-soft); font-size: 12px; line-height: 1.6; }.plan-meta { display: flex; flex-wrap: wrap; gap: 18px; padding: 13px 20px; color: var(--muted); border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); font-size: 11px; }.plan-meta span { display: inline-flex; align-items: center; gap: 6px; }.sku-section { padding: 20px; }.sku-heading { display: flex; align-items: center; justify-content: space-between; margin-bottom: 13px; }.sku-heading h3 { margin: 0; font-size: 14px; }.sku-heading span { color: var(--muted); font-size: 11px; }.sku-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }.sku-card { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--line); border-radius: 11px; }.sku-card.inactive { opacity: .65; }.sku-top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }.sku-type { color: var(--primary); font-size: 10px; font-weight: 700; }.sku-top h4 { margin: 3px 0 2px; font-size: 14px; }.sku-top code { color: var(--muted); font-size: 10px; }.sku-price { margin: 0; color: var(--text-strong); font-size: 24px; font-weight: 750; }.sku-price span { color: var(--muted); font-size: 11px; font-weight: 500; }.sku-card dl { display: grid; grid-template-columns: repeat(3, 1fr); margin: 0; padding: 11px; border-radius: 9px; background: var(--surface-soft); }.sku-card dl > div { display: grid; gap: 3px; }.sku-card dt { color: var(--muted); font-size: 10px; }.sku-card dd { margin: 0; font-size: 11px; font-weight: 650; }.sku-actions { display: flex; flex-wrap: wrap; gap: 7px; }.form-section { display: grid; gap: 16px; padding: 18px; border: 1px solid var(--line); border-radius: 12px; }.form-section-title { display: flex; gap: 10px; }.form-section-title > span { width: 27px; height: 27px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 50%; color: #fff; background: var(--primary); font-size: 11px; font-weight: 700; }.form-section-title h3 { margin: 1px 0 2px; font-size: 14px; }.form-section-title p { margin: 0; color: var(--muted); font-size: 11px; }.endpoint-picker { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; padding: 12px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); }.endpoint-picker label { padding: 8px; border-radius: 7px; background: #fff; }.endpoint-picker label span { display: grid; gap: 2px; }.endpoint-picker small { color: var(--muted); }.binding-picker { grid-template-columns: 1fr; }
@media (max-width: 900px) { .catalog-summary { grid-template-columns: repeat(2, 1fr); }.catalog-summary > div:nth-child(3) { border-left: 0; border-top: 1px solid var(--line); }.catalog-summary > div:nth-child(4) { border-top: 1px solid var(--line); }.plan-header { flex-direction: column; }.endpoint-picker { grid-template-columns: 1fr; } }
@media (max-width: 520px) { .catalog-summary { grid-template-columns: 1fr; }.catalog-summary > div + div { border-left: 0; border-top: 1px solid var(--line); } }
</style>
