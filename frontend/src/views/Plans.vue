<template>
  <section class="standard-page">
    <PageHeader title="商品与规格" description="商品定义交付权益，销售规格只定义价格、周期和可用购买场景。" eyebrow="Commerce">
      <template #actions>
        <PageRefreshButton label="刷新商品" :loading="loading" @click="refresh" />
        <UiButton type="button" @click="openCreate">新增商品</UiButton>
      </template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="商品已更新" error-title="商品加载失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" @clear="clearFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" maxlength="128" placeholder="商品名称、标识或简介" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="activeFilter" label="发布状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/orders')">查看订单<UiIcon name="chevron" /></RouterLink></template>
      <DataTable v-if="plans.length" caption="商品管理列表" :row-count="total" :min-width="980">
        <thead><tr><th class="table-primary-column">商品</th><th data-column-priority="2">交付节点组</th><th data-column-priority="1">交付权益</th><th>发布状态</th><th data-column-priority="2">销售规格</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody>
          <tr v-for="item in plans" :key="item.id">
            <td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><span>{{ item.slug }}</span></div></td>
            <td data-column-priority="2"><EntityReference kind="node_group" :id="item.node_group_id" :label="item.node_group_name" :to="adminContextLink('/admin/node-groups', { group: String(item.node_group_id) })" /></td>
            <td data-column-priority="1"><div class="cell-title"><strong>{{ formatBytes(item.traffic_bytes) }}</strong><span>{{ item.device_limit > 0 ? `${item.device_limit} 台设备` : '不限设备' }} · {{ item.speed_limit_mbps > 0 ? `${item.speed_limit_mbps} Mbps` : '不限速' }}</span></div></td>
            <td><StatusBadge :tone="item.is_active ? 'success' : 'neutral'">{{ item.is_active ? '已发布' : '草稿' }}</StatusBadge></td>
            <td data-column-priority="2"><div class="cell-title"><strong>{{ item.active_sku_count }} 个可售</strong><span>共 {{ item.sku_count }} 个 SKU</span></div></td>
            <td class="table-action-column"><RowActions :label="`商品 ${item.name} 的操作`" :trigger-key="`plan-${item.id}`"><UiButton variant="secondary" size="sm" type="button" :data-plan-detail-trigger="item.id" @click="openDetail(item.id)">查看详情</UiButton><UiButton variant="ghost" size="sm" type="button" :data-plan-editor-trigger="item.id" @click="openPlanEditor(item)">编辑商品</UiButton><UiButton variant="ghost" size="sm" type="button" @click="togglePlanStatus(item)">{{ item.is_active ? '下架' : '发布' }}</UiButton></RowActions></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else icon="plans" title="没有匹配商品" description="调整搜索或发布状态筛选。" />
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(expandedPlanID)" :title="detailPlan?.name || '商品详情'" eyebrow="Product" :description="detailPlan?.slug || '查看交付权益和销售规格'" :return-focus-selector="detailReturnFocusSelector" @close="closeDetail">
      <PageAlert v-if="detailError" tone="danger" title="商品详情加载失败">{{ detailError }}</PageAlert>
      <div v-if="detailLoading" class="detail-loading" role="status">正在加载商品详情…</div>
      <main v-else-if="detailPlan" class="business-detail stack">
        <section class="detail-status-strip" aria-label="商品状态"><StatusBadge :tone="detailPlan.is_active ? 'success' : 'neutral'">{{ detailPlan.is_active ? '已发布' : '草稿' }}</StatusBadge><StatusBadge tone="info">修订 #{{ detailPlan.revision }}</StatusBadge></section>
        <section class="detail-facts" aria-label="商品交付权益">
          <div><span>商品标识</span><strong class="mono">{{ detailPlan.slug }}</strong></div>
          <div><span>节点组</span><EntityReference kind="node_group" :id="detailPlan.node_group_id" :label="detailPlan.node_group_name" :to="adminContextLink('/admin/node-groups', { group: String(detailPlan.node_group_id) })" /></div>
          <div><span>流量</span><strong>{{ formatBytes(detailPlan.traffic_bytes) }}</strong></div>
          <div><span>设备数</span><strong>{{ detailPlan.device_limit > 0 ? detailPlan.device_limit : '不限' }}</strong></div>
          <div><span>速率</span><strong>{{ detailPlan.speed_limit_mbps > 0 ? `${detailPlan.speed_limit_mbps} Mbps` : '不限速' }}</strong></div>
          <div><span>有效订阅上限</span><strong>{{ detailPlan.max_active_subscriptions > 0 ? detailPlan.max_active_subscriptions : '不限' }}</strong></div>
          <div><span>续费</span><strong>{{ detailPlan.is_renewable ? '允许' : '不允许' }}</strong></div>
          <div><span>重置策略</span><strong>{{ resetPolicyName(detailPlan.reset_policy) }}</strong></div>
          <div><span>流量计费</span><strong>{{ trafficCalcName(detailPlan.traffic_calc_mode) }}</strong></div>
          <div><span>更新时间</span><TimeBadge :value="detailPlan.updated_at" /></div>
        </section>
        <section class="detail-copy"><h3>简介</h3><p>{{ detailPlan.summary || '未填写简介' }}</p></section>
        <section class="detail-copy"><h3>完整说明</h3><p>{{ detailPlan.description || '未填写说明' }}</p></section>
        <div class="detail-action-row"><UiButton variant="secondary" size="sm" type="button" @click="openPlanEditor(detailPlan)">编辑商品</UiButton><UiButton size="sm" type="button" data-plan-sku-create-trigger="detailPlan.id" @click="openSKUCreate">新增销售规格</UiButton></div>

        <section class="stack">
          <header class="detail-section-header"><div><h3>销售规格</h3><p>价格、周期和购买场景独立维护，不覆盖商品交付权益。</p></div><span>{{ skuTotal }}</span></header>
          <PageAlert v-if="skuListError" tone="danger" title="销售规格加载失败">{{ skuListError }}</PageAlert>
          <WorkbenchFilterBar :active="Boolean(skuSearch || skuActiveFilter)" :loading="skuListLoading" label="销售规格筛选" @clear="clearSKUFilters">
            <WorkbenchFilterInput v-model="skuSearch" label="搜索" placeholder="SKU 名称、编码或币种" @apply="applySKUFilters" />
            <WorkbenchFilterSelect v-model="skuActiveFilter" label="可售状态" :options="skuActiveOptions" @apply="applySKUFilters" />
          </WorkbenchFilterBar>
          <DataTable v-if="planSKUs.length" caption="商品销售规格" :row-count="skuTotal" :min-width="820"><thead><tr><th class="table-primary-column">SKU</th><th>购买场景</th><th>计费方式</th><th data-column-priority="1">价格</th><th>状态</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="sku in planSKUs" :key="sku.id"><td class="table-primary-column"><div class="cell-title"><strong>{{ sku.name }}</strong><span class="mono">{{ sku.code }}</span></div></td><td><div class="tag-list"><StatusBadge v-for="operation in sku.allowed_operations" :key="operation" tone="info">{{ skuOperationName(operation) }}</StatusBadge></div></td><td>{{ billingModeName(sku.billing_mode) }} · {{ billingLabel(sku) }}</td><td data-column-priority="1">{{ formatCurrency(sku.price_cents, sku.currency) }}</td><td><StatusBadge :tone="sku.is_active ? 'success' : 'neutral'">{{ sku.is_active ? '可售' : '停用' }}</StatusBadge></td><td class="table-action-column"><RowActions :label="`SKU ${sku.name} 的操作`" :trigger-key="`plan-sku-${sku.id}`"><UiButton variant="secondary" size="sm" type="button" :data-plan-sku-trigger="sku.id" @click="openSKUEditor(sku)">编辑 SKU</UiButton></RowActions></td></tr></tbody></DataTable>
          <EmptyState v-else-if="!skuListLoading" icon="billing" title="暂无销售规格" description="创建月付、年付或附加权益等规格。" />
          <TablePager v-if="skuTotal > skuLimit" :total="skuTotal" :offset="skuOffset" :limit="skuLimit" :loading="skuListLoading" @change="changeSKUPage" />
        </section>
      </main>
    </DetailDrawer>

    <ModalDialog :open="createOpen" title="新增商品" description="先定义商品权益和首个销售规格。" size="lg" :busy="saving" :return-focus-selector="'[data-create-plan-trigger]'" @close="closeCreate">
      <form ref="createFormElement" class="modal-form form-layout" @submit.prevent="submitCreate">
        <PageAlert v-if="createErrors.summary" tone="danger" title="无法创建商品">{{ createErrors.summary }}</PageAlert>
        <div class="form-grid two-column">
          <FormField label="商品名称" required :error="createErrors.errorFor('name')"><UiInput v-model="form.name" placeholder="例如：个人标准版" /></FormField>
          <FormField label="商品标识（Slug）" required hint="用于系统识别和链接，创建后应保持稳定。" :error="createErrors.errorFor('slug')"><UiInput v-model="form.slug" placeholder="例如：personal-standard" /></FormField>
        </div>
        <FormField label="简介" :error="createErrors.errorFor('summary')"><UiInput v-model="form.summary" placeholder="列表中展示的简短说明" /></FormField>
        <FormField label="完整说明" :error="createErrors.errorFor('description')"><UiTextarea v-model="form.description" rows="4" placeholder="商品详情和适用场景" /></FormField>
        <FormField label="交付节点组" required :error="createErrors.errorFor('node_group_id')"><NodeGroupLookup v-model="form.node_group_id" /></FormField>
        <div class="form-grid three-column">
          <FormField label="流量额度" required :error="createErrors.errorFor('traffic_bytes')"><ByteSizeInput v-model="form.traffic_bytes" /></FormField>
          <FormField label="设备数" required :error="createErrors.errorFor('device_limit')"><UiNumberInput v-model="form.device_limit" :min="0" /></FormField>
          <FormField label="限速 Mbps" required :error="createErrors.errorFor('speed_limit_mbps')"><UiNumberInput v-model="form.speed_limit_mbps" :min="0" /></FormField>
        </div>
        <div class="form-grid three-column">
          <FormField label="有效订阅上限" :error="createErrors.errorFor('max_active_subscriptions')"><UiNumberInput v-model="form.max_active_subscriptions" :min="0" /></FormField>
          <FormField label="家庭组人数" :error="createErrors.errorFor('family_limit')"><UiNumberInput v-model="form.family_limit" :min="0" /></FormField>
          <FormField label="流量重置" :error="createErrors.errorFor('reset_policy')"><UiSelect v-model="form.reset_policy" :options="resetPolicyOptions" /></FormField>
        </div>
        <FormField label="流量计费口径" :error="createErrors.errorFor('traffic_calc_mode')"><UiSelect v-model="form.traffic_calc_mode" :options="trafficCalcOptions" /></FormField>
        <UiCheckbox v-model="form.is_renewable">允许续费</UiCheckbox>
        <UiCheckbox v-model="form.is_active">创建后立即发布</UiCheckbox>
        <section class="form-section stack">
          <header><div><h3>首个销售规格</h3><p>销售规格只定义价格、周期和可用购买场景。</p></div></header>
          <div class="form-grid two-column">
            <FormField label="SKU 名称" required :error="createErrors.errorFor('sku.name')"><UiInput v-model="form.sku.name" placeholder="例如：月付" /></FormField>
            <FormField label="SKU 编码" required hint="全站唯一，用于订单和接口识别。" :error="createErrors.errorFor('sku.code')"><UiInput v-model="form.sku.code" placeholder="例如：personal-standard-monthly" /></FormField>
          </div>
          <div class="form-grid three-column">
            <FormField label="计费方式" required :error="createErrors.errorFor('sku.billing_mode')"><UiSelect v-model="form.sku.billing_mode" :options="billingModeOptions" /></FormField>
            <FormField label="计费单位" required :error="createErrors.errorFor('sku.billing_unit')"><UiSelect v-model="form.sku.billing_unit" :options="billingUnitOptions" /></FormField>
            <FormField label="周期数量" required :error="createErrors.errorFor('sku.billing_value')"><UiNumberInput v-model="form.sku.billing_value" :min="1" /></FormField>
          </div>
          <FormField label="可用购买场景" required :error="createErrors.errorFor('sku.allowed_operations')"><div class="operation-grid"><label v-for="option in skuOperationOptions" :key="option.value"><UiCheckbox :model-value="form.sku.allowed_operations.includes(option.value)" @update:model-value="toggleOperation(form.sku, option.value, $event)">{{ option.label }}</UiCheckbox><small>{{ option.description }}</small></label></div></FormField>
          <div class="form-grid two-column">
            <FormField label="销售价格" required :error="createErrors.errorFor('sku.price_cents')"><MoneyInput v-model="form.sku.price_cents" :currency="form.sku.currency" /></FormField>
            <FormField label="币种" required :error="createErrors.errorFor('sku.currency')"><UiInput v-model="form.sku.currency" maxlength="3" /></FormField>
          </div>
          <FormField v-if="form.sku.billing_mode === 'one_time'" label="附加流量" required :error="createErrors.errorFor('sku.grant_traffic_bytes')"><ByteSizeInput v-model="form.sku.grant_traffic_bytes" /></FormField>
        </section>
      </form>
      <template #footer><UiButton variant="secondary" type="button" :disabled="saving" @click="closeCreate">取消</UiButton><UiButton type="button" :loading="saving" @click="submitCreate">创建商品</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="planEditorOpen" title="编辑商品" description="商品权益会成为新订单和续费订单的快照来源。" size="lg" :busy="saving" :return-focus-selector="planEditorReturnFocusSelector" @close="closePlanEditor">
      <form ref="planFormElement" class="modal-form form-layout" @submit.prevent="submitPlanUpdate">
        <PageAlert v-if="planErrors.summary" tone="danger" title="无法保存商品">{{ planErrors.summary }}</PageAlert>
        <PageAlert v-if="planRevisionConflict" tone="warning" title="商品已被其他管理员修改">请关闭编辑器并重新打开，确认最新商品权益后再保存。</PageAlert>
        <div class="form-grid two-column"><FormField label="商品名称" required :error="planErrors.errorFor('name')"><UiInput v-model="planDraft.name" /></FormField><FormField label="商品标识（Slug）" required hint="用于系统识别和链接，修改前请确认外部引用。" :error="planErrors.errorFor('slug')"><UiInput v-model="planDraft.slug" /></FormField></div>
        <FormField label="简介" :error="planErrors.errorFor('summary')"><UiInput v-model="planDraft.summary" /></FormField>
        <FormField label="完整说明" :error="planErrors.errorFor('description')"><UiTextarea v-model="planDraft.description" rows="4" /></FormField>
        <FormField label="交付节点组" required :error="planErrors.errorFor('node_group_id')"><NodeGroupLookup v-model="planDraft.node_group_id" /></FormField>
        <div class="form-grid three-column"><FormField label="流量额度" required :error="planErrors.errorFor('traffic_bytes')"><ByteSizeInput v-model="planDraft.traffic_bytes" /></FormField><FormField label="设备数" required :error="planErrors.errorFor('device_limit')"><UiNumberInput v-model="planDraft.device_limit" :min="0" /></FormField><FormField label="限速 Mbps" required :error="planErrors.errorFor('speed_limit_mbps')"><UiNumberInput v-model="planDraft.speed_limit_mbps" :min="0" /></FormField></div>
        <div class="form-grid three-column"><FormField label="有效订阅上限" :error="planErrors.errorFor('max_active_subscriptions')"><UiNumberInput v-model="planDraft.max_active_subscriptions" :min="0" /></FormField><FormField label="家庭组人数" :error="planErrors.errorFor('family_limit')"><UiNumberInput v-model="planDraft.family_limit" :min="0" /></FormField><FormField label="流量重置" :error="planErrors.errorFor('reset_policy')"><UiSelect v-model="planDraft.reset_policy" :options="resetPolicyOptions" /></FormField></div>
        <FormField label="流量计费口径" :error="planErrors.errorFor('traffic_calc_mode')"><UiSelect v-model="planDraft.traffic_calc_mode" :options="trafficCalcOptions" /></FormField>
        <UiCheckbox v-model="planDraft.is_renewable">允许续费</UiCheckbox><UiCheckbox v-model="planDraft.is_active">已发布</UiCheckbox>
      </form>
      <template #footer><UiButton variant="secondary" type="button" :disabled="saving" @click="closePlanEditor">取消</UiButton><UiButton type="button" :loading="saving" :disabled="planRevisionConflict" @click="submitPlanUpdate">保存商品</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="skuOpen" :title="skuDraft.id ? '编辑销售规格' : '新增销售规格'" description="价格、周期和可用购买场景属于 SKU；商品权益不会在这里重复配置。" size="lg" :busy="saving" :return-focus-selector="skuReturnFocusSelector" @close="closeSKUEditor">
      <form ref="skuFormElement" class="modal-form form-layout" @submit.prevent="submitSKU">
        <PageAlert v-if="skuErrors.summary" tone="danger" title="无法保存 SKU">{{ skuErrors.summary }}</PageAlert>
        <div class="form-grid two-column"><FormField label="SKU 名称" required :error="skuErrors.errorFor('name')"><UiInput v-model="skuDraft.name" /></FormField><FormField label="SKU 编码" required hint="全站唯一，用于订单和接口识别。" :error="skuErrors.errorFor('code')"><UiInput v-model="skuDraft.code" /></FormField></div>
        <div class="form-grid three-column"><FormField label="计费方式" required :error="skuErrors.errorFor('billing_mode')"><UiSelect v-model="skuDraft.billing_mode" :options="billingModeOptions" /></FormField><FormField label="计费单位" required :error="skuErrors.errorFor('billing_unit')"><UiSelect v-model="skuDraft.billing_unit" :options="billingUnitOptions" /></FormField><FormField label="周期数量" required :error="skuErrors.errorFor('billing_value')"><UiNumberInput v-model="skuDraft.billing_value" :min="1" /></FormField></div>
        <FormField label="可用购买场景" required :error="skuErrors.errorFor('allowed_operations')"><div class="operation-grid"><label v-for="option in skuOperationOptions" :key="option.value"><UiCheckbox :model-value="skuDraft.allowed_operations.includes(option.value)" @update:model-value="toggleOperation(skuDraft, option.value, $event)">{{ option.label }}</UiCheckbox><small>{{ option.description }}</small></label></div></FormField>
        <div class="form-grid two-column"><FormField label="销售价格" required :error="skuErrors.errorFor('price_cents')"><MoneyInput v-model="skuDraft.price_cents" :currency="skuDraft.currency" /></FormField><FormField label="币种" required :error="skuErrors.errorFor('currency')"><UiInput v-model="skuDraft.currency" maxlength="3" /></FormField></div>
        <FormField v-if="skuDraft.billing_mode === 'one_time'" label="附加流量" required :error="skuErrors.errorFor('grant_traffic_bytes')"><ByteSizeInput v-model="skuDraft.grant_traffic_bytes" /></FormField>
        <div class="form-grid two-column"><FormField label="排序" :error="skuErrors.errorFor('sort_order')"><UiNumberInput v-model="skuDraft.sort_order" /></FormField><FormField label="销售状态" :error="skuErrors.errorFor('is_active')"><UiCheckbox v-model="skuDraft.is_active">可售</UiCheckbox></FormField></div>
      </form>
      <template #footer><UiButton variant="secondary" type="button" :disabled="saving" @click="closeSKUEditor">取消</UiButton><UiButton type="button" :loading="saving" @click="submitSKU">保存 SKU</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createPlan,
  createPlanSKU,
  fetchPlanDetail,
  fetchPlanSKU,
  fetchPlansPage,
  fetchPlanSKUs,
  updatePlan,
  updatePlanSKU,
  type PlanDetail,
  type PlanSKU,
  type PlanSummary,
} from '../api/client'
import ByteSizeInput from '../components/ByteSizeInput.vue'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import EntityReference from '../components/EntityReference.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import MoneyInput from '../components/MoneyInput.vue'
import NodeGroupLookup from '../components/NodeGroupLookup.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiIcon from '../components/UiIcon.vue'
import UiInput from '../components/UiInput.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiSelect from '../components/UiSelect.vue'
import UiTextarea from '../components/UiTextarea.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useFormErrors, useDirtyForm, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const search = ref(String(route.query.q || ''))
const activeFilter = ref(String(route.query.active || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const expandedPlanID = ref<number | null>(null)
const skuSearch = ref(String(route.query.sku_q || ''))
const skuActiveFilter = ref(String(route.query.sku_active || ''))
const skuLimit = ref(allowedPageSizes.includes(Number(route.query.sku_limit)) ? Number(route.query.sku_limit) : 25)
const skuOffset = ref((Math.max(1, Number(route.query.sku_page) || 1) - 1) * skuLimit.value)
const message = ref('')
const saving = ref(false)
const statusUpdating = ref(false)
const createOpen = ref(false)
const planEditorOpen = ref(false)
const planRevisionConflict = ref(false)
const skuOpen = ref(false)
const detailPlan = ref<PlanDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const createFormElement = ref<HTMLElement | null>(null)
const planFormElement = ref<HTMLElement | null>(null)
const skuFormElement = ref<HTMLElement | null>(null)
const createErrors = useFormErrors()
const planErrors = useFormErrors()
const skuErrors = useFormErrors()

const emptySKU = (planID = 0) => ({
  id: 0,
  plan_id: planID,
  code: '',
  name: '',
  sku_type: 'new',
  billing_mode: 'periodic' as 'periodic' | 'one_time',
  allowed_operations: ['purchase', 'renew'] as Array<'purchase' | 'renew' | 'change' | 'addon'>,
  billing_unit: 'month',
  billing_value: 1,
  price_cents: 0,
  currency: 'CNY',
  grant_traffic_bytes: 0,
  is_active: true,
  sort_order: 0,
  created_at: '',
  updated_at: '',
})
const emptyPlanForm = () => ({
  name: '',
  slug: '',
  summary: '',
  description: '',
  is_active: false,
  traffic_bytes: 100 * 1024 ** 3,
  speed_limit_mbps: 0,
  device_limit: 3,
  node_group_id: 0,
  max_active_subscriptions: 0,
  is_renewable: true,
  family_limit: 0,
  reset_policy: 0,
  traffic_calc_mode: 0,
  sku: emptySKU(),
})
const emptyPlanDraft = () => ({
  id: 0,
  name: '',
  slug: '',
  summary: '',
  description: '',
  node_group_id: 0,
  traffic_bytes: 0,
  speed_limit_mbps: 0,
  max_active_subscriptions: 0,
  is_renewable: true,
  device_limit: 1,
  family_limit: 0,
  reset_policy: 0,
  traffic_calc_mode: 0,
  is_active: false,
  sort_order: 0,
  revision: 0,
  updated_at: '',
})
const form = reactive(emptyPlanForm())
const planDraft = reactive(emptyPlanDraft())
const skuDraft = reactive<PlanSKU>(emptySKU())
const createState = useDirtyForm(() => form)
const planEditorState = useDirtyForm(() => planDraft)
const skuState = useDirtyForm(() => skuDraft)
useUnsavedChangesGuard(
  () => (createOpen.value && createState.dirty.value)
    || (planEditorOpen.value && planEditorState.dirty.value)
    || (skuOpen.value && skuState.dirty.value),
  async () => {
    if (createOpen.value && !await createState.confirmDiscard({
      title: '放弃套餐草稿？',
      message: '离开套餐管理后，尚未创建的套餐和首个 SKU 信息将丢失。',
      confirmText: '离开页面',
    })) return false
    if (planEditorOpen.value && !await planEditorState.confirmDiscard({
      title: '放弃套餐修改？',
      message: '离开套餐管理后，尚未保存的套餐交付规则将丢失。',
      confirmText: '离开页面',
    })) return false
    if (skuOpen.value && !await skuState.confirmDiscard({
      title: '放弃 SKU 修改？',
      message: '离开套餐管理后，尚未保存的价格、周期和可用场景修改将丢失。',
      confirmText: '离开页面',
    })) return false
    return true
  },
)

const createPlanFieldMap: Record<string, string> = {
  name: 'name',
  slug: 'slug',
  summary: 'summary',
  description: 'description',
  node_group_id: 'node_group_id',
  traffic_bytes: 'traffic_bytes',
  device_limit: 'device_limit',
  max_active_subscriptions: 'max_active_subscriptions',
  speed_limit_mbps: 'speed_limit_mbps',
  family_limit: 'family_limit',
  reset_policy: 'reset_policy',
  traffic_calc_mode: 'traffic_calc_mode',
  'skus.0.name': 'sku.name',
  'skus.0.code': 'sku.code',
  'skus.0.currency': 'sku.currency',
  'skus.0.billing_mode': 'sku.billing_mode',
  'skus.0.allowed_operations': 'sku.allowed_operations',
  'skus.0.billing_unit': 'sku.billing_unit',
  'skus.0.billing_value': 'sku.billing_value',
  'skus.0.price_cents': 'sku.price_cents',
  'skus.0.grant_traffic_bytes': 'sku.grant_traffic_bytes',
}
const planFieldMap: Record<string, string> = {
  name: 'name',
  slug: 'slug',
  summary: 'summary',
  description: 'description',
  node_group_id: 'node_group_id',
  traffic_bytes: 'traffic_bytes',
  speed_limit_mbps: 'speed_limit_mbps',
  max_active_subscriptions: 'max_active_subscriptions',
  device_limit: 'device_limit',
  family_limit: 'family_limit',
  reset_policy: 'reset_policy',
  traffic_calc_mode: 'traffic_calc_mode',
  sort_order: 'sort_order',
  is_active: 'is_active',
}
const skuFieldMap: Record<string, string> = {
  name: 'name',
  code: 'code',
  currency: 'currency',
  billing_mode: 'billing_mode',
  allowed_operations: 'allowed_operations',
  billing_unit: 'billing_unit',
  billing_value: 'billing_value',
  price_cents: 'price_cents',
  grant_traffic_bytes: 'grant_traffic_bytes',
  sort_order: 'sort_order',
  is_active: 'is_active',
}

const activeOptions = [
  { label: '全部状态', value: '' },
  { label: '已发布', value: 'true' },
  { label: '草稿', value: 'false' },
]
const skuActiveOptions = [
  { label: '全部 SKU', value: '' },
  { label: '可售', value: 'true' },
  { label: '停用', value: 'false' },
]
const billingModeOptions = [
  { label: '周期计费', value: 'periodic' },
  { label: '一次性计费', value: 'one_time' },
]
const skuOperationOptions = [
  { label: '新购', value: 'purchase' as const, description: '允许用户创建新的独立订阅。' },
  { label: '续费', value: 'renew' as const, description: '允许为同一商品的现有订阅延长周期。' },
  { label: '套餐切换', value: 'change' as const, description: '允许其他商品的订阅切换到当前商品。' },
  { label: '附加购买', value: 'addon' as const, description: '一次性增加目标订阅的附加权益。' },
]
const billingUnitOptions = [
  { label: '天', value: 'day' },
  { label: '月', value: 'month' },
  { label: '年', value: 'year' },
  { label: '一次性', value: 'once' },
]
const resetPolicyOptions = [
  { label: '跟随系统', value: 0 },
  { label: '每月 1 日', value: 1 },
  { label: '按购买日每月', value: 2 },
  { label: '每年 1 月 1 日', value: 3 },
  { label: '按购买日每年', value: 4 },
  { label: '不重置', value: 5 },
]
const trafficCalcOptions = [
  { label: '上行 + 下行', value: 0 },
  { label: '仅上行', value: 1 },
  { label: '仅下行', value: 2 },
]
const billingModes = ['periodic', 'one_time'] as const
const skuOperations = ['purchase', 'renew', 'change', 'addon'] as const
const billingUnits = ['day', 'month', 'year', 'once'] as const
const resetPolicies = [0, 1, 2, 3, 4, 5] as const
const trafficCalcModes = [0, 1, 2] as const
const managedQueryKeys = new Set(['q', 'active', 'page', 'limit', 'plan', 'editor', 'sku', 'sku_q', 'sku_active', 'sku_page', 'sku_limit'])

for (const [source, field] of [
  [() => form.name, 'name'],
  [() => form.slug, 'slug'],
  [() => form.summary, 'summary'],
  [() => form.description, 'description'],
  [() => form.node_group_id, 'node_group_id'],
  [() => form.traffic_bytes, 'traffic_bytes'],
  [() => form.sku.name, 'sku.name'],
  [() => form.sku.code, 'sku.code'],
  [() => form.sku.currency, 'sku.currency'],
  [() => form.sku.billing_mode, 'sku.billing_mode'],
  [() => form.sku.allowed_operations, 'sku.allowed_operations'],
  [() => form.sku.billing_unit, 'sku.billing_unit'],
  [() => form.sku.billing_value, 'sku.billing_value'],
  [() => form.sku.price_cents, 'sku.price_cents'],
  [() => form.sku.grant_traffic_bytes, 'sku.grant_traffic_bytes'],
  [() => form.device_limit, 'device_limit'],
  [() => form.speed_limit_mbps, 'speed_limit_mbps'],
  [() => form.max_active_subscriptions, 'max_active_subscriptions'],
  [() => form.family_limit, 'family_limit'],
  [() => form.reset_policy, 'reset_policy'],
  [() => form.traffic_calc_mode, 'traffic_calc_mode'],
] as Array<[() => unknown, string]>) {
  watch(source, () => createErrors.clear(field))
}
for (const field of Object.keys(planFieldMap)) {
  watch(() => planDraft[field as keyof typeof planDraft], () => planErrors.clear(field))
}
for (const field of Object.keys(skuFieldMap)) {
  watch(() => skuDraft[field as keyof PlanSKU], () => skuErrors.clear(field))
}
watch(() => form.sku.billing_mode, () => {
  syncSKUCommerceFields(form.sku)
  createErrors.clear('sku.billing_mode')
  createErrors.clear('sku.allowed_operations')
})
watch(() => skuDraft.billing_mode, () => {
  syncSKUCommerceFields(skuDraft)
  skuErrors.clear('billing_mode')
  skuErrors.clear('allowed_operations')
})

const {
  items: plans,
  total,
  loading,
  refreshing,
  error,
  load: refresh,
} = useRemoteTable<PlanSummary>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchPlansPage({
    includeInactive: app.isAdmin,
    q: search.value || undefined,
    active: activeFilter.value === '' ? undefined : activeFilter.value === 'true',
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '商品数据加载失败。',
  onOffsetCorrected: () => syncListURL(true),
})

const {
  items: planSKUs,
  total: skuTotal,
  loading: skuListLoading,
  refreshing: skuRefreshing,
  hasLoaded: skuHasLoaded,
  error: skuListError,
  load: loadSKUs,
  invalidate: invalidateSKUs,
} = useRemoteTable<PlanSKU>({
  offset: skuOffset,
  limit: skuLimit,
  fetchPage: ({ signal }) => expandedPlanID.value
    ? fetchPlanSKUs(expandedPlanID.value, {
        q: skuSearch.value || undefined,
        active: skuActiveFilter.value === '' ? undefined : skuActiveFilter.value === 'true',
        offset: skuOffset.value,
        limit: skuLimit.value,
      }, { signal })
    : Promise.resolve({ items: [], total: 0 }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '销售规格加载失败。',
  onOffsetCorrected: () => setRoute({}, true),
})

const detailReturnFocusSelector = computed(() => expandedPlanID.value ? `[data-plan-detail-trigger="${expandedPlanID.value}"]` : '')
const planEditorReturnFocusSelector = computed(() => planDraft.id ? `[data-plan-editor-trigger="${planDraft.id}"]` : '')
const skuReturnFocusSelector = computed(() => {
  if (skuDraft.id) return `[data-plan-sku-trigger="${skuDraft.id}"]`
  return expandedPlanID.value ? `[data-plan-sku-create-trigger="${expandedPlanID.value}"]` : ''
})

let detailRequestSequence = 0
let detailController: AbortController | null = null
let skuDetailController: AbortController | null = null

function parsePositiveID(value: unknown) {
  const id = Number(Array.isArray(value) ? value[0] : value)
  return Number.isInteger(id) && id > 0 ? id : null
}

function ownedQuery(overrides: { plan?: number | null; editor?: string | null; sku?: string | null } = {}) {
  const query: Record<string, string | string[]> = {}
  for (const [key, value] of Object.entries(route.query)) {
    if (!managedQueryKeys.has(key) && value !== undefined && value !== null) {
      query[key] = value as string | string[]
    }
  }
  const page = Math.floor(offset.value / limit.value) + 1
  if (search.value) query.q = search.value
  if (activeFilter.value) query.active = activeFilter.value
  if (page > 1) query.page = String(page)
  if (limit.value !== 50) query.limit = String(limit.value)
  const plan = Object.prototype.hasOwnProperty.call(overrides, 'plan') ? overrides.plan : expandedPlanID.value
  const editor = Object.prototype.hasOwnProperty.call(overrides, 'editor') ? overrides.editor : String(route.query.editor || '')
  const sku = Object.prototype.hasOwnProperty.call(overrides, 'sku') ? overrides.sku : String(route.query.sku || '')
  if (plan) query.plan = String(plan)
  if (editor) query.editor = editor
  if (sku) query.sku = sku
  if (plan) {
    const skuPage = Math.floor(skuOffset.value / skuLimit.value) + 1
    if (skuSearch.value) query.sku_q = skuSearch.value
    if (skuActiveFilter.value) query.sku_active = skuActiveFilter.value
    if (skuPage > 1) query.sku_page = String(skuPage)
    if (skuLimit.value !== 25) query.sku_limit = String(skuLimit.value)
  }
  return query
}

async function setRoute(
  overrides: { plan?: number | null; editor?: string | null; sku?: string | null } = {},
  replace = false,
) {
  const location = { query: ownedQuery(overrides) }
  await (replace ? router.replace(location) : router.push(location))
}

async function syncListURL(replace = false) {
  await setRoute({}, replace)
}

async function loadDetail(id: number) {
  const sequence = ++detailRequestSequence
  detailController?.abort()
  const controller = new AbortController()
  detailController = controller
  detailLoading.value = true
  detailError.value = ''
  try {
    const detail = await fetchPlanDetail(id, { signal: controller.signal })
    if (sequence !== detailRequestSequence) return
    detailPlan.value = detail
  } catch (cause: any) {
    if (sequence !== detailRequestSequence || cause?.name === 'CanceledError' || cause?.name === 'AbortError') return
    detailPlan.value = null
    detailError.value = cause?.response?.data?.message || '商品详情加载失败。'
  } finally {
    if (sequence === detailRequestSequence) detailLoading.value = false
  }
}

async function syncDetailAndEditorsFromRoute() {
  const nextPlanID = parsePositiveID(route.query.plan)
  const planChanged = expandedPlanID.value !== nextPlanID
  expandedPlanID.value = nextPlanID
  if (!nextPlanID) {
    detailRequestSequence++
    detailController?.abort()
    skuDetailController?.abort()
    invalidateSKUs()
    planSKUs.value = []
    skuTotal.value = 0
    detailPlan.value = null
    detailLoading.value = false
    detailError.value = ''
    planEditorOpen.value = false
    planRevisionConflict.value = false
    skuOpen.value = false
    return
  }
  if (planChanged) {
    invalidateSKUs()
    planSKUs.value = []
    skuTotal.value = 0
  }
  if (detailPlan.value?.id !== nextPlanID) await loadDetail(nextPlanID)
  if (planChanged || !skuHasLoaded.value) await loadSKUs()
  const editor = String(route.query.editor || '')
  if (editor === 'plan' && detailPlan.value?.id === nextPlanID) {
    if (!planEditorOpen.value || planDraft.id !== nextPlanID) {
      assignPlanDraft(detailPlan.value)
      planRevisionConflict.value = false
      planErrors.clear()
      planEditorState.markClean()
    }
    planEditorOpen.value = true
  } else {
    planEditorOpen.value = false
  }
  const skuKey = String(route.query.sku || '')
  if (skuKey && detailPlan.value?.id === nextPlanID) {
    let source: PlanSKU | ReturnType<typeof emptySKU> | null = skuKey === 'new' ? emptySKU(nextPlanID) : null
    if (!source) {
      const skuID = parsePositiveID(skuKey)
      if (!skuID) {
        await setRoute({ sku: null }, true)
        return
      }
      skuDetailController?.abort()
      skuDetailController = new AbortController()
      try {
        source = await fetchPlanSKU(skuID, { signal: skuDetailController.signal })
      } catch (cause: any) {
        if (cause?.name === 'CanceledError' || cause?.name === 'AbortError') return
        error.value = cause?.response?.data?.message || '销售规格详情加载失败。'
      }
    }
    if (source) {
      Object.assign(skuDraft, source)
      syncSKUCommerceFields(skuDraft)
      skuErrors.clear()
      skuState.markClean()
      skuOpen.value = true
    }
  } else {
    skuOpen.value = false
  }
}

function resetPlanForm() { Object.assign(form, emptyPlanForm()); createErrors.clear(); createState.markClean() }
function openCreate() { resetPlanForm(); createOpen.value = true }
async function closeCreate() { if (!await createState.confirmDiscard()) return; createOpen.value = false; resetPlanForm() }
function assignPlanDraft(source: PlanDetail | PlanSummary) { Object.assign(planDraft, source) }
async function openDetail(id: number) { await setRoute({ plan: id, editor: null, sku: null }) }
async function closeDetail() { if ((planEditorOpen.value && !await planEditorState.confirmDiscard()) || (skuOpen.value && !await skuState.confirmDiscard())) return; await setRoute({ plan: null, editor: null, sku: null }) }
async function openPlanEditor(source: PlanDetail | PlanSummary) { if (expandedPlanID.value !== source.id) await setRoute({ plan: source.id, editor: 'plan', sku: null }); else await setRoute({ editor: 'plan', sku: null }) }
async function closePlanEditor() { if (!await planEditorState.confirmDiscard()) return; await setRoute({ editor: null }) }
async function openSKUCreate() { if (!expandedPlanID.value) return; await setRoute({ sku: 'new', editor: null }) }
async function openSKUEditor(sku: PlanSKU) { await setRoute({ sku: String(sku.id), editor: null }) }
async function closeSKUEditor() { if (!await skuState.confirmDiscard()) return; await setRoute({ sku: null }) }

function syncSKUCommerceFields(sku: ReturnType<typeof emptySKU> | PlanSKU) {
  if (sku.billing_mode === 'one_time') {
    sku.billing_unit = 'once'
    sku.billing_value = Math.max(1, Number(sku.billing_value) || 1)
    sku.allowed_operations = ['addon']
  } else {
    if (sku.billing_unit === 'once') sku.billing_unit = 'month'
    sku.grant_traffic_bytes = 0
    sku.allowed_operations = sku.allowed_operations.filter(operation => operation !== 'addon')
    if (!sku.allowed_operations.length) sku.allowed_operations = ['purchase', 'renew']
  }
  sku.sku_type = compatibilitySKUType(sku.billing_mode, sku.allowed_operations)
}
function compatibilitySKUType(billingMode: 'periodic' | 'one_time', operations: Array<'purchase' | 'renew' | 'change' | 'addon'>): PlanSKU['sku_type'] {
  if (billingMode === 'one_time' || operations.includes('addon')) return 'traffic_pack'
  if (operations.includes('purchase')) return 'new'
  if (operations.includes('renew')) return 'renewal'
  return 'upgrade'
}
function toggleOperation(sku: ReturnType<typeof emptySKU> | PlanSKU, operation: 'purchase' | 'renew' | 'change' | 'addon', enabled: boolean) {
  const next = new Set(sku.allowed_operations)
  if (enabled) next.add(operation); else next.delete(operation)
  sku.allowed_operations = skuOperations.filter(item => next.has(item))
  syncSKUCommerceFields(sku)
}
function resetPolicyName(value: number) { return resetPolicyOptions.find(item => item.value === value)?.label || `未知策略 ${value}` }
function trafficCalcName(value: number) { return trafficCalcOptions.find(item => item.value === value)?.label || `未知口径 ${value}` }
function skuOperationName(value: string) { return skuOperationOptions.find(item => item.value === value)?.label || value }
function billingModeName(value: string) { return billingModeOptions.find(item => item.value === value)?.label || value }
function billingLabel(sku: PlanSKU) { const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit; return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}` }

function normalizePlanPayload(source: typeof planDraft) {
  return {
    name: source.name.trim(), slug: source.slug.trim().toLowerCase(), summary: source.summary.trim(), description: source.description.trim(),
    node_group_id: Number(source.node_group_id), traffic_bytes: Number(source.traffic_bytes), speed_limit_mbps: Number(source.speed_limit_mbps),
    max_active_subscriptions: Number(source.max_active_subscriptions), is_renewable: source.is_renewable, device_limit: Number(source.device_limit),
    family_limit: Number(source.family_limit), reset_policy: Number(source.reset_policy), traffic_calc_mode: Number(source.traffic_calc_mode),
    is_active: source.is_active, sort_order: Number(source.sort_order), expected_revision: Number(source.revision),
  }
}
function normalizeSKUPayload(source: ReturnType<typeof emptySKU> | PlanSKU) {
  syncSKUCommerceFields(source)
  return {
    code: source.code.trim().toLowerCase(), name: source.name.trim(), sku_type: compatibilitySKUType(source.billing_mode, source.allowed_operations),
    billing_mode: source.billing_mode, allowed_operations: source.allowed_operations, billing_unit: source.billing_unit,
    billing_value: Number(source.billing_value), price_cents: Number(source.price_cents), currency: source.currency.trim().toUpperCase(),
    grant_traffic_bytes: source.billing_mode === 'one_time' ? Number(source.grant_traffic_bytes) : 0,
    is_active: source.is_active, sort_order: Number(source.sort_order),
  }
}

async function submitCreate() {
  createErrors.clear(); saving.value = true
  try {
    syncSKUCommerceFields(form.sku)
    const created = await createPlan({
      name: form.name.trim(), slug: form.slug.trim().toLowerCase(), summary: form.summary.trim(), description: form.description.trim(),
      node_group_id: Number(form.node_group_id), traffic_bytes: Number(form.traffic_bytes), speed_limit_mbps: Number(form.speed_limit_mbps),
      max_active_subscriptions: Number(form.max_active_subscriptions), is_renewable: form.is_renewable, device_limit: Number(form.device_limit),
      family_limit: Number(form.family_limit), reset_policy: Number(form.reset_policy), traffic_calc_mode: Number(form.traffic_calc_mode),
      is_active: form.is_active, skus: [normalizeSKUPayload(form.sku)],
    })
    createState.markClean(); createOpen.value = false; message.value = `商品 ${created.name} 已创建。`; resetPlanForm(); await refresh(); await openDetail(created.id)
  } catch (cause: any) {
    const result = createErrors.applyApiError(cause, '商品创建失败，请检查表单内容。', Object.keys(createPlanFieldMap), createPlanFieldMap)
    await nextTick(); createErrors.focusFirstInvalid(createFormElement.value, result.firstField)
  } finally { saving.value = false }
}

async function submitPlanUpdate() {
  if (!planDraft.id) return
  planErrors.clear(); planRevisionConflict.value = false; saving.value = true
  try {
    const updated = await updatePlan(planDraft.id, normalizePlanPayload(planDraft))
    planEditorState.markClean(); planEditorOpen.value = false; message.value = `商品 ${updated.name} 已保存。`; await refresh(); await loadDetail(updated.id); await loadSKUs(); await setRoute({ editor: null }, true)
  } catch (cause: any) {
    if (cause?.response?.status === 409 || cause?.response?.status === 428) planRevisionConflict.value = true
    const result = planErrors.applyApiError(cause, '商品保存失败，请检查表单内容。', Object.keys(planFieldMap), planFieldMap)
    await nextTick(); planErrors.focusFirstInvalid(planFormElement.value, result.firstField)
  } finally { saving.value = false }
}

async function submitSKU() {
  if (!expandedPlanID.value) return
  skuErrors.clear(); saving.value = true
  try {
    const payload = normalizeSKUPayload(skuDraft)
    if (skuDraft.id) await updatePlanSKU(skuDraft.id, payload)
    else await createPlanSKU(expandedPlanID.value, payload)
    skuState.markClean(); skuOpen.value = false; message.value = `SKU ${payload.name} 已保存。`; invalidateSKUs(); await loadSKUs(); await loadDetail(expandedPlanID.value); await refresh(); await setRoute({ sku: null }, true)
  } catch (cause: any) {
    const result = skuErrors.applyApiError(cause, 'SKU 保存失败，请检查表单内容。', Object.keys(skuFieldMap), skuFieldMap)
    await nextTick(); skuErrors.focusFirstInvalid(skuFormElement.value, result.firstField)
  } finally { saving.value = false }
}

async function togglePlanStatus(item: PlanSummary) {
  if (statusUpdating.value) return
  statusUpdating.value = true
  try {
    await updatePlan(item.id, { is_active: !item.is_active, expected_revision: item.revision })
    message.value = `商品 ${item.name} 已${item.is_active ? '下架' : '发布'}。`
    await refresh()
    if (expandedPlanID.value === item.id) await loadDetail(item.id)
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '商品状态更新失败。'
  } finally { statusUpdating.value = false }
}

function hasFiltersValue() { return Boolean(search.value || activeFilter.value) }
const hasFilters = computed(hasFiltersValue)
async function applyFilters() { offset.value = 0; await syncListURL(); await refresh() }
async function clearFilters() { search.value = ''; activeFilter.value = ''; offset.value = 0; await syncListURL(); await refresh() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncListURL(); await refresh() }
async function applySKUFilters() { skuOffset.value = 0; await setRoute({}, true); await loadSKUs() }
async function clearSKUFilters() { skuSearch.value = ''; skuActiveFilter.value = ''; skuOffset.value = 0; await setRoute({}, true); await loadSKUs() }
async function changeSKUPage(value: { offset: number; limit: number }) { skuOffset.value = value.offset; skuLimit.value = value.limit; await setRoute({}, true); await loadSKUs() }
function adminContextLink(path: string, query: Record<string, string> = {}) { return withAdminReturnTo(path, route.fullPath, query) }

watch(() => route.fullPath, async () => {
  const nextSearch = String(route.query.q || ''), nextActive = String(route.query.active || '')
  const nextLimit = allowedPageSizes.includes(Number(route.query.limit)) ? Number(route.query.limit) : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  search.value = nextSearch; activeFilter.value = nextActive; limit.value = nextLimit; offset.value = nextOffset
  skuSearch.value = String(route.query.sku_q || ''); skuActiveFilter.value = String(route.query.sku_active || '')
  skuLimit.value = allowedPageSizes.includes(Number(route.query.sku_limit)) ? Number(route.query.sku_limit) : 25
  skuOffset.value = (Math.max(1, Number(route.query.sku_page) || 1) - 1) * skuLimit.value
  await syncDetailAndEditorsFromRoute()
})

onMounted(async () => { await refresh(); await syncDetailAndEditorsFromRoute() })
</script>
