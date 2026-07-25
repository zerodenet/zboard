<template>
  <section class="standard-page">
    <PageHeader
      title="商品与套餐"
      description="列表只保留商品摘要；销售策略和 SKU 在按需详情中管理。"
      eyebrow="Catalog"
    >
      <template #actions>
        <PageRefreshButton label="刷新商品与套餐" :loading="loading" @click="refreshAll" />
        <UiButton v-if="app.isAdmin" type="button" @click="openCreate">
          <UiIcon name="plus" />创建商品
        </UiButton>
      </template>
    </PageHeader>

    <TransientFeedback
      :success="message"
      :error="error"
      success-title="商品配置已更新"
      error-title="商品操作失败"
    />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || activeFilter)" @clear="clearFilters">
          <WorkbenchFilterInput
            v-model="search"
            label="搜索"
            placeholder="名称、Slug 或摘要"
            @apply="applyFilters"
          />
          <WorkbenchFilterSelect v-model="activeFilter" label="发布状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable
        v-if="plans.length"
        caption="套餐商品列表"
        :row-count="total"
        :min-width="820"
        table-class="plan-table"
      >
        <thead>
          <tr>
            <th class="table-primary-column">商品</th>
            <th>状态</th>
            <th data-column-priority="2">节点组</th>
            <th class="numeric-column" data-column-priority="3">SKU</th>
            <th class="numeric-column">可售 SKU</th>
            <th data-column-priority="2">更新时间</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="plan in plans" :key="plan.id">
            <td class="table-primary-column">
              <div class="cell-title">
                <strong>{{ plan.name }}</strong>
                <span>{{ plan.slug }} · {{ plan.summary || '暂无摘要' }}</span>
              </div>
            </td>
            <td>
              <StatusBadge :tone="plan.is_active ? 'success' : 'neutral'">
                {{ plan.is_active ? '已发布' : '草稿' }}
              </StatusBadge>
            </td>
            <td data-column-priority="2">
              {{ plan.node_group?.name || `节点组 #${plan.node_group_id}` }}
            </td>
            <td class="numeric-column" data-column-priority="3">{{ plan.sku_count }}</td>
            <td class="numeric-column">{{ plan.active_sku_count }}</td>
            <td data-column-priority="2"><TimeBadge :value="plan.updated_at" /></td>
            <td class="table-action-column">
              <UiButton
                variant="secondary"
                size="sm"
                type="button"
                :data-plan-detail-trigger="plan.id"
                @click="openDetails(plan.id)"
              >
                查看
              </UiButton>
            </td>
          </tr>
        </tbody>
      </DataTable>

      <EmptyState
        v-else
        icon="plans"
        title="没有匹配的商品"
        description="调整筛选条件，或创建第一个商品。"
      >
        <template v-if="app.isAdmin" #actions>
          <UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建商品</UiButton>
        </template>
      </EmptyState>

      <template #footer>
        <TablePager
          :total="total"
          :offset="offset"
          :limit="limit"
          :loading="loading"
          @change="changePage"
        />
      </template>
    </DataWorkbench>

    <DetailDrawer
      :open="Boolean(expandedPlanID)"
      :title="detailPlan?.name || '商品详情'"
      :description="detailPlan?.summary || '商品策略和销售规格按需加载'"
      :return-focus-selector="detailReturnFocusSelector"
      @close="closeDetails"
    >
      <div v-if="detailLoading" class="detail-loading" role="status" aria-live="polite">
        <StatusBadge tone="info" icon="refresh">正在加载商品详情</StatusBadge>
      </div>
      <PageAlert v-else-if="detailError" tone="danger" title="商品详情加载失败">
        {{ detailError }}
        <template #actions>
          <UiButton variant="secondary" size="sm" type="button" @click="retryDetail">重试</UiButton>
        </template>
      </PageAlert>
      <template v-else-if="detailPlan">
        <div class="detail-toolbar">
          <UiButton
            v-if="app.isAdmin"
            variant="secondary"
            size="sm"
            type="button"
            :data-plan-editor-trigger="detailPlan.id"
            @click="openPlanEditor"
          >
            <UiIcon name="edit" />编辑商品
          </UiButton>
          <UiButton
            v-if="app.isAdmin"
            variant="secondary"
            size="sm"
            type="button"
            :data-plan-sku-create-trigger="detailPlan.id"
            @click="openCreateSKU"
          >
            <UiIcon name="plus" />新增 SKU
          </UiButton>
          <UiButton
            v-if="app.isAdmin"
            :variant="detailPlan.is_active ? 'danger' : 'primary'"
            size="sm"
            type="button"
            :loading="statusUpdating"
            @click="toggleActive(detailPlan)"
          >
            {{ detailPlan.is_active ? '转为草稿' : '发布商品' }}
          </UiButton>
        </div>

        <dl class="detail-kv">
          <div><dt>商品状态</dt><dd><StatusBadge :tone="detailPlan.is_active ? 'success' : 'neutral'">{{ detailPlan.is_active ? '已发布' : '草稿' }}</StatusBadge></dd></div>
          <div><dt>商品 Slug</dt><dd class="mono">{{ detailPlan.slug }}</dd></div>
          <div><dt>节点组</dt><dd>{{ detailPlan.node_group?.name || `#${detailPlan.node_group_id}` }}</dd></div>
          <div><dt>SKU 总数 / 可售</dt><dd>{{ detailPlan.sku_count }} / {{ detailPlan.active_sku_count }}</dd></div>
          <div><dt>流量配额</dt><dd>{{ formatBytes(detailPlan.traffic_bytes) }}</dd></div>
          <div><dt>速率限制</dt><dd>{{ detailPlan.speed_limit_mbps }} Mbps{{ detailPlan.speed_limit_mbps === 0 ? '（不限速）' : '' }}</dd></div>
          <div><dt>设备数</dt><dd>{{ detailPlan.device_limit }}</dd></div>
          <div><dt>最大有效订阅</dt><dd>{{ detailPlan.max_active_subscriptions }}</dd></div>
          <div><dt>续费</dt><dd>{{ detailPlan.is_renewable ? '支持' : '不支持' }}</dd></div>
          <div><dt>家庭共享人数</dt><dd>{{ detailPlan.family_limit }}</dd></div>
          <div><dt>流量重置</dt><dd>{{ resetPolicyLabel(detailPlan.reset_policy) }}</dd></div>
          <div><dt>消耗计算</dt><dd>{{ trafficModeLabel(detailPlan.traffic_calc_mode) }}</dd></div>
          <div><dt>排序</dt><dd>{{ detailPlan.sort_order }}</dd></div>
          <div><dt>更新时间</dt><dd><TimeBadge :value="detailPlan.updated_at" /></dd></div>
        </dl>

        <p v-if="detailPlan.description" class="plan-description">{{ detailPlan.description }}</p>

        <div class="detail-section-heading">
          <div>
            <h3>销售规格</h3>
            <p>SKU 决定新订单的价格、周期、流量和设备限制。</p>
          </div>
          <span>{{ detailPlan.sku_count }} 个</span>
        </div>
        <DataWorkbench :total="skuTotal" :loading="skuListLoading" :refreshing="skuRefreshing">
          <template #filters>
            <WorkbenchFilterBar :active="Boolean(skuSearch || skuActiveFilter)" @clear="clearSKUFilters">
              <WorkbenchFilterInput
                v-model="skuSearch"
                label="搜索"
                placeholder="SKU 名称、编码或币种"
                @apply="applySKUFilters"
              />
              <WorkbenchFilterSelect v-model="skuActiveFilter" label="销售规格状态" :options="skuActiveOptions" @apply="applySKUFilters" />
            </WorkbenchFilterBar>
          </template>

          <PageAlert v-if="skuListError" tone="danger" title="销售规格加载失败">{{ skuListError }}</PageAlert>
          <DataTable
            v-else-if="planSKUs.length"
            caption="套餐销售规格列表"
            :row-count="skuTotal"
            :min-width="720"
            table-class="sku-table"
          >
            <thead>
              <tr>
                <th class="table-primary-column">SKU</th>
                <th>状态</th>
                <th>价格</th>
                <th data-column-priority="2">周期</th>
                <th data-column-priority="2">流量</th>
                <th class="numeric-column" data-column-priority="3">设备数</th>
                <th class="table-action-column"><span class="sr-only">操作</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="sku in planSKUs" :key="sku.id">
                <td class="table-primary-column">
                  <div class="cell-title">
                    <strong>{{ sku.name }}</strong>
                    <span>{{ sku.code }} · {{ skuTypeLabel(sku.sku_type) }}</span>
                  </div>
                </td>
                <td><StatusBadge :tone="sku.is_active ? 'success' : 'neutral'">{{ sku.is_active ? '可售' : '停用' }}</StatusBadge></td>
                <td>{{ formatCurrency(sku.price_cents, sku.currency) }}</td>
                <td data-column-priority="2">{{ billingLabel(sku) }}</td>
                <td data-column-priority="2">{{ formatBytes(sku.traffic_bytes) }}</td>
                <td class="numeric-column" data-column-priority="3">{{ sku.device_limit }}</td>
                <td class="table-action-column">
                  <UiButton
                    v-if="app.isAdmin"
                    variant="secondary"
                    size="sm"
                    type="button"
                    :data-plan-sku-trigger="sku.id"
                    @click="openSKU(sku)"
                  >
                    编辑
                  </UiButton>
                </td>
              </tr>
            </tbody>
          </DataTable>
          <EmptyState
            v-else
            icon="plans"
            title="没有匹配销售规格"
            description="调整筛选条件，或为商品新增 SKU。"
          >
            <template v-if="app.isAdmin" #actions>
              <UiButton type="button" @click="openCreateSKU"><UiIcon name="plus" />新增 SKU</UiButton>
            </template>
          </EmptyState>

          <template #footer>
            <TablePager
              :total="skuTotal"
              :offset="skuOffset"
              :limit="skuLimit"
              :loading="skuListLoading"
              @change="changeSKUPage"
            />
          </template>
        </DataWorkbench>
      </template>
    </DetailDrawer>

    <ModalDialog
      :open="createOpen"
      :dirty="createState.dirty.value"
      title="创建商品与首个 SKU"
      description="先建立商品边界和一个可售规格，后续 SKU 独立维护。"
      size="xl"
      :busy="saving"
      @close="createOpen = false"
    >
      <form id="create-plan-form" ref="createFormElement" class="stack" novalidate @submit.prevent="create">
        <PageAlert v-if="createErrors.formError.value" tone="danger" title="无法创建商品">
          {{ createErrors.formError.value }}
        </PageAlert>
        <section class="form-section">
          <div class="form-section-title"><span>1</span><div><h3>商品信息</h3><p>用于目录展示和内部识别。</p></div></div>
          <div class="form-grid form-grid-3">
            <FormField v-slot="{ controlAttrs }" label="商品名称" name="create-plan-name" :error="createErrors.fields.name" required>
              <UiInput v-model.trim="form.name" v-bind="controlAttrs" placeholder="基础套餐" />
            </FormField>
            <FormField v-slot="{ controlAttrs }" label="Slug" name="create-plan-slug" :error="createErrors.fields.slug" required>
              <UiInput v-model.trim="form.slug" v-bind="controlAttrs" placeholder="starter" />
            </FormField>
            <FormField v-slot="{ controlAttrs }" label="摘要" name="create-plan-summary" :error="createErrors.fields.summary">
              <UiInput v-model.trim="form.summary" v-bind="controlAttrs" maxlength="255" placeholder="适合个人日常使用" />
            </FormField>
            <FormField v-slot="{ controlAttrs }" label="详细说明" name="create-plan-description" :error="createErrors.fields.description" full>
              <UiTextarea v-model="form.description" v-bind="controlAttrs" rows="3" />
            </FormField>
          </div>
        </section>
        <section class="form-section">
          <div class="form-section-title"><span>2</span><div><h3>首个销售规格</h3><p>定义价格、计费周期和服务限制。</p></div></div>
          <div class="form-grid form-grid-3">
            <FormField v-slot="{ controlAttrs }" label="SKU 名称" name="create-plan-sku-name" :error="createErrors.fields['sku.name']" required><UiInput v-model.trim="form.sku.name" v-bind="controlAttrs" placeholder="月付" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="SKU 编码" name="create-plan-sku-code" :error="createErrors.fields['sku.code']" required><UiInput v-model.trim="form.sku.code" v-bind="controlAttrs" placeholder="starter-monthly" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="规格类型" name="create-plan-sku-type" :error="createErrors.fields['sku.sku_type']"><UiSelect v-model="form.sku.sku_type" v-bind="controlAttrs" :options="skuTypeOptions" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="计费单位" name="create-plan-billing-unit" :error="createErrors.fields['sku.billing_unit']"><UiSelect v-model="form.sku.billing_unit" v-bind="controlAttrs" :options="billingUnitOptions" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="周期数量" name="create-plan-billing-value" :error="createErrors.fields['sku.billing_value']" required><UiNumberInput v-model="form.sku.billing_value" v-bind="controlAttrs" :min="1" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="价格" name="create-plan-price" hint="按所选币种的标准金额输入；系统以整数分保存。" :error="createErrors.fields['sku.price_cents']" required><MoneyInput v-model="form.sku.price_cents" v-bind="controlAttrs" :currency="form.sku.currency || 'CNY'" :min-cents="0" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="币种" name="create-plan-currency" :error="createErrors.fields['sku.currency']" required><UiInput v-model.trim="form.sku.currency" v-bind="controlAttrs" maxlength="8" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="流量配额" name="create-plan-traffic" hint="统一以 GiB 输入并换算为字节保存。" :error="createErrors.fields.traffic_bytes" required><ByteSizeInput v-model="form.traffic_bytes" v-bind="controlAttrs" :min-bytes="1024 ** 3" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="设备数" name="create-plan-device-limit" :error="createErrors.fields['sku.device_limit']" required><UiNumberInput v-model="form.sku.device_limit" v-bind="controlAttrs" :min="1" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="速率限制" name="create-plan-speed-limit" hint="0 表示不限速。" :error="createErrors.fields['sku.speed_limit_mbps']"><UiNumberInput v-model="form.sku.speed_limit_mbps" v-bind="controlAttrs" :min="0" suffix=" Mbps" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="最大有效订阅" name="create-plan-max-subscriptions" :error="createErrors.fields.max_active_subscriptions"><UiNumberInput v-model="form.max_active_subscriptions" v-bind="controlAttrs" :min="0" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="家庭共享人数" name="create-plan-family-limit" :error="createErrors.fields.family_limit"><UiNumberInput v-model="form.family_limit" v-bind="controlAttrs" :min="0" inputmode="numeric" /></FormField>
          </div>
        </section>
        <section class="form-section">
          <div class="form-section-title"><span>3</span><div><h3>流量与交付边界</h3><p>套餐只关联节点组；协议端点及倍率由节点组和协议服务维护。</p></div></div>
          <div class="form-grid form-grid-3">
            <FormField v-slot="{ controlAttrs }" label="节点组" name="create-plan-node-group" hint="按名称、代码或说明远程搜索，不预载全部节点组。" :error="createErrors.fields.node_group_id" required><NodeGroupLookup v-model="form.node_group_id" v-bind="controlAttrs" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="流量重置" name="create-plan-reset-policy" :error="createErrors.fields.reset_policy"><UiSelect v-model.number="form.reset_policy" v-bind="controlAttrs" :options="resetPolicyOptions" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="消耗计算" name="create-plan-traffic-mode" :error="createErrors.fields.traffic_calc_mode"><UiSelect v-model.number="form.traffic_calc_mode" v-bind="controlAttrs" :options="trafficCalcOptions" /></FormField>
            <label class="check-field"><UiCheckbox v-model="form.is_active" /><span>创建后立即发布商品</span></label>
            <label class="check-field"><UiCheckbox v-model="form.is_renewable" /><span>允许用户续费</span></label>
          </div>
        </section>
      </form>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
        <UiButton form="create-plan-form" type="submit" :loading="saving">创建商品与 SKU</UiButton>
      </template>
    </ModalDialog>

    <ModalDialog
      :open="planEditorOpen"
      :dirty="planEditorState.dirty.value"
      title="编辑商品"
      description="商品展示、交付边界和套餐策略独立于 SKU 价格维护。"
      size="xl"
      :busy="saving"
      :return-focus-selector="planEditorReturnFocusSelector"
      @close="closePlanEditor"
    >
      <form id="edit-plan-form" ref="planFormElement" class="stack" novalidate @submit.prevent="savePlan">
        <div class="editor-version-meta"><StatusBadge tone="neutral" icon="history">版本 {{ planDraft.revision }}</StatusBadge><TimeBadge :value="planDraft.updated_at" /></div>
        <PageAlert v-if="planRevisionConflict" tone="warning" title="商品已在其他会话更新">
          当前草稿基于旧版本。请重新加载最新商品信息后再继续，避免覆盖其他会话的修改。
          <template #actions><UiButton variant="secondary" size="sm" type="button" :loading="detailLoading" @click="reloadPlanEditor"><UiIcon name="refresh" />重新加载最新版本</UiButton></template>
        </PageAlert>
        <PageAlert v-if="planErrors.formError.value" tone="danger" title="无法保存商品">
          {{ planErrors.formError.value }}
        </PageAlert>
        <section class="form-section">
          <div class="form-section-title"><span>1</span><div><h3>商品信息</h3><p>调整目录展示和内部排序。</p></div></div>
          <div class="form-grid form-grid-3">
            <FormField v-slot="{ controlAttrs }" label="商品名称" name="edit-plan-name" :error="planErrors.fields.name" required><UiInput v-model.trim="planDraft.name" v-bind="controlAttrs" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="Slug" name="edit-plan-slug" :error="planErrors.fields.slug" required><UiInput v-model.trim="planDraft.slug" v-bind="controlAttrs" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="排序" name="edit-plan-sort-order" :error="planErrors.fields.sort_order"><UiNumberInput v-model="planDraft.sort_order" v-bind="controlAttrs" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="摘要" name="edit-plan-summary" :error="planErrors.fields.summary" full><UiInput v-model.trim="planDraft.summary" v-bind="controlAttrs" maxlength="255" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="详细说明" name="edit-plan-description" :error="planErrors.fields.description" full><UiTextarea v-model="planDraft.description" v-bind="controlAttrs" rows="4" /></FormField>
          </div>
        </section>
        <section class="form-section">
          <div class="form-section-title"><span>2</span><div><h3>套餐策略</h3><p>定义所有 SKU 共享的权限上限和流量口径。</p></div></div>
          <div class="form-grid form-grid-3">
            <FormField v-slot="{ controlAttrs }" label="节点组" name="edit-plan-node-group" hint="按需检索，不预载全部节点组。" :error="planErrors.fields.node_group_id" required><NodeGroupLookup v-model="planDraft.node_group_id" v-bind="controlAttrs" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="流量配额" name="edit-plan-traffic" :error="planErrors.fields.traffic_bytes" required><ByteSizeInput v-model="planDraft.traffic_bytes" v-bind="controlAttrs" :min-bytes="1" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="速率限制" name="edit-plan-speed" hint="0 表示不限速。" :error="planErrors.fields.speed_limit_mbps"><UiNumberInput v-model="planDraft.speed_limit_mbps" v-bind="controlAttrs" :min="0" suffix=" Mbps" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="设备数" name="edit-plan-device-limit" :error="planErrors.fields.device_limit" required><UiNumberInput v-model="planDraft.device_limit" v-bind="controlAttrs" :min="1" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="最大有效订阅" name="edit-plan-max-subscriptions" :error="planErrors.fields.max_active_subscriptions"><UiNumberInput v-model="planDraft.max_active_subscriptions" v-bind="controlAttrs" :min="0" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="家庭共享人数" name="edit-plan-family-limit" :error="planErrors.fields.family_limit"><UiNumberInput v-model="planDraft.family_limit" v-bind="controlAttrs" :min="0" inputmode="numeric" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="流量重置" name="edit-plan-reset-policy" :error="planErrors.fields.reset_policy"><UiSelect v-model.number="planDraft.reset_policy" v-bind="controlAttrs" :options="resetPolicyOptions" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="消耗计算" name="edit-plan-traffic-mode" :error="planErrors.fields.traffic_calc_mode"><UiSelect v-model.number="planDraft.traffic_calc_mode" v-bind="controlAttrs" :options="trafficCalcOptions" /></FormField>
            <label class="check-field"><UiCheckbox v-model="planDraft.is_renewable" /><span>允许用户续费</span></label>
            <label class="check-field"><UiCheckbox v-model="planDraft.is_active" /><span>商品已发布</span></label>
          </div>
        </section>
      </form>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
        <UiButton form="edit-plan-form" type="submit" :loading="saving" :disabled="planRevisionConflict">保存商品</UiButton>
      </template>
    </ModalDialog>

    <ModalDialog
      :open="skuOpen"
      :dirty="skuState.dirty.value"
      :title="skuDraft.id ? '编辑销售规格' : '新增销售规格'"
      description="修改只影响后续新订单；历史订单继续使用商业快照。"
      size="lg"
      :busy="saving"
      :return-focus-selector="skuReturnFocusSelector"
      @close="closeSKU"
    >
      <form id="sku-form" ref="skuFormElement" class="form-grid form-grid-3" novalidate @submit.prevent="saveSKU">
        <PageAlert v-if="skuErrors.formError.value" class="field-full" tone="danger" title="无法保存规格">
          {{ skuErrors.formError.value }}
        </PageAlert>
        <FormField v-slot="{ controlAttrs }" label="规格名称" name="edit-sku-name" :error="skuErrors.fields.name" required><UiInput v-model.trim="skuDraft.name" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="SKU 编码" name="edit-sku-code" :error="skuErrors.fields.code" required><UiInput v-model.trim="skuDraft.code" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="规格类型" name="edit-sku-type" :error="skuErrors.fields.sku_type"><UiSelect v-model="skuDraft.sku_type" v-bind="controlAttrs" :options="skuTypeOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="计费单位" name="edit-sku-billing-unit" :error="skuErrors.fields.billing_unit"><UiSelect v-model="skuDraft.billing_unit" v-bind="controlAttrs" :options="billingUnitOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="周期数量" name="edit-sku-billing-value" :error="skuErrors.fields.billing_value" required><UiNumberInput v-model="skuDraft.billing_value" v-bind="controlAttrs" :min="1" inputmode="numeric" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="币种" name="edit-sku-currency" :error="skuErrors.fields.currency" required><UiInput v-model.trim="skuDraft.currency" v-bind="controlAttrs" maxlength="8" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="价格" name="edit-sku-price" hint="按币种标准金额输入；系统以整数分保存。" :error="skuErrors.fields.price_cents"><MoneyInput v-model="skuDraft.price_cents" v-bind="controlAttrs" :currency="skuDraft.currency || 'CNY'" :min-cents="0" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="流量配额" name="edit-sku-traffic" hint="统一以 GiB 输入并换算为字节保存。" :error="skuErrors.fields.traffic_bytes"><ByteSizeInput v-model="skuDraft.traffic_bytes" v-bind="controlAttrs" :min-bytes="1" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="设备数" name="edit-sku-device-limit" :error="skuErrors.fields.device_limit"><UiNumberInput v-model="skuDraft.device_limit" v-bind="controlAttrs" :min="1" inputmode="numeric" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="速率限制" name="edit-sku-speed-limit" :error="skuErrors.fields.speed_limit_mbps"><UiNumberInput v-model="skuDraft.speed_limit_mbps" v-bind="controlAttrs" :min="0" suffix=" Mbps" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="排序" name="edit-sku-sort-order" :error="skuErrors.fields.sort_order"><UiNumberInput v-model="skuDraft.sort_order" v-bind="controlAttrs" inputmode="numeric" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="销售状态" name="edit-sku-active" :error="skuErrors.fields.is_active" full>
          <div class="check-field"><UiCheckbox v-model="skuDraft.is_active" v-bind="controlAttrs" /><span>该 SKU 可用于创建新订单</span></div>
        </FormField>
      </form>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
        <UiButton form="sku-form" type="submit" :loading="saving">{{ skuDraft.id ? '保存规格' : '创建规格' }}</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import {
  createPlan,
  createPlanSKU,
  fetchPlanDetail,
  fetchPlanSKU,
  fetchPlanSKUs,
  fetchPlansPage,
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
import FormField from '../components/FormField.vue'
import MoneyInput from '../components/MoneyInput.vue'
import ModalDialog from '../components/ModalDialog.vue'
import NodeGroupLookup from '../components/NodeGroupLookup.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useAppStore } from '../stores/app'
import { confirmAction } from '../utils/feedback'
import { formatBytes, formatCurrency, formatUnknownValue } from '../utils/format'
import {
  collectFieldErrors,
  isBlank,
  isIntegerInRange,
  isOneOf,
  isSlug,
  isUtf8LengthInRange,
} from '../utils/validation'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const search = ref(String(route.query.q || ''))
const activeFilter = ref(String(route.query.active || ''))
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const expandedPlanID = ref(parsePositiveID(route.query.plan))
const skuSearch = ref(String(route.query.sku_q || ''))
const skuActiveFilter = ref(String(route.query.sku_active || ''))
const initialSKULimit = Number(route.query.sku_limit)
const skuLimit = ref(allowedPageSizes.includes(initialSKULimit) ? initialSKULimit : 25)
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
  billing_unit: 'month',
  billing_value: 1,
  price_cents: 0,
  currency: 'CNY',
  traffic_bytes: 100 * 1024 ** 3,
  device_limit: 3,
  speed_limit_mbps: 0,
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
      message: '离开套餐管理后，尚未保存的价格、周期和配额修改将丢失。',
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
  device_limit: 'sku.device_limit',
  speed_limit_mbps: 'sku.speed_limit_mbps',
  max_active_subscriptions: 'max_active_subscriptions',
  family_limit: 'family_limit',
  reset_policy: 'reset_policy',
  traffic_calc_mode: 'traffic_calc_mode',
  'skus.0.name': 'sku.name',
  'skus.0.code': 'sku.code',
  'skus.0.currency': 'sku.currency',
  'skus.0.sku_type': 'sku.sku_type',
  'skus.0.billing_unit': 'sku.billing_unit',
  'skus.0.billing_value': 'sku.billing_value',
  'skus.0.price_cents': 'sku.price_cents',
  'skus.0.traffic_bytes': 'traffic_bytes',
  'skus.0.device_limit': 'sku.device_limit',
  'skus.0.speed_limit_mbps': 'sku.speed_limit_mbps',
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
  sku_type: 'sku_type',
  billing_unit: 'billing_unit',
  billing_value: 'billing_value',
  price_cents: 'price_cents',
  traffic_bytes: 'traffic_bytes',
  device_limit: 'device_limit',
  speed_limit_mbps: 'speed_limit_mbps',
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
const skuTypeOptions = [
  { label: '新购', value: 'new' },
  { label: '续费', value: 'renewal' },
  { label: '升级', value: 'upgrade' },
  { label: '流量包', value: 'traffic_pack' },
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
const skuTypes = ['new', 'renewal', 'upgrade', 'traffic_pack'] as const
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
  [() => form.sku.sku_type, 'sku.sku_type'],
  [() => form.sku.billing_unit, 'sku.billing_unit'],
  [() => form.sku.billing_value, 'sku.billing_value'],
  [() => form.sku.price_cents, 'sku.price_cents'],
  [() => form.sku.device_limit, 'sku.device_limit'],
  [() => form.sku.speed_limit_mbps, 'sku.speed_limit_mbps'],
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
    if (source && source.plan_id !== nextPlanID) source = null
    if (!source) {
      await setRoute({ sku: null }, true)
      return
    }
    if (!skuOpen.value || skuDraft.id !== source.id || (skuKey === 'new' && skuDraft.plan_id !== nextPlanID)) {
      Object.assign(skuDraft, JSON.parse(JSON.stringify(source)))
      skuErrors.clear()
      skuState.markClean()
    }
    skuOpen.value = true
  } else {
    skuOpen.value = false
  }
}

function skuTypeLabel(type: string) {
  return ({
    new: '新购',
    renewal: '续费',
    upgrade: '升级',
    traffic_pack: '流量包',
  } as Record<string, string>)[type] || formatUnknownValue('规格类型', type)
}

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function resetPolicyLabel(value: number) {
  return resetPolicyOptions.find(option => option.value === value)?.label || formatUnknownValue('重置策略', value)
}

function trafficModeLabel(value: number) {
  return trafficCalcOptions.find(option => option.value === value)?.label || formatUnknownValue('流量计算方式', value)
}

function normalizeSKUInput(sku: Pick<PlanSKU, 'name' | 'code' | 'currency'>) {
  sku.name = String(sku.name || '').trim()
  sku.code = String(sku.code || '').trim().toLowerCase()
  sku.currency = String(sku.currency || '').trim().toUpperCase()
}

function skuValidation(sku: PlanSKU | ReturnType<typeof emptySKU>, prefix = '', trafficBytes = sku.traffic_bytes) {
  const field = (name: string) => `${prefix}${name}`
  const billingUnitValid = isOneOf(sku.billing_unit, billingUnits)
  return collectFieldErrors({
    [field('name')]: !isUtf8LengthInRange(sku.name, 1, 80, true) && '规格名称需包含 1–80 个 UTF-8 字节。',
    [field('code')]: !isSlug(sku.code, 80) && 'SKU 编码只能包含小写字母、数字和单个连字符。',
    [field('currency')]: !isUtf8LengthInRange(sku.currency, 1, 8, true) && '币种需包含 1–8 个 UTF-8 字节。',
    [field('sku_type')]: !isOneOf(sku.sku_type, skuTypes) && '请选择有效的规格类型。',
    [field('billing_unit')]: !billingUnitValid
      ? '请选择有效的计费单位。'
      : sku.billing_unit === 'once' && sku.sku_type !== 'traffic_pack'
        ? '一次性计费仅适用于流量包。'
        : false,
    [field('billing_value')]: !isIntegerInRange(sku.billing_value, 1, Number.MAX_SAFE_INTEGER) && '周期数量必须为大于 0 的整数。',
    [field('price_cents')]: !isIntegerInRange(sku.price_cents, 0, Number.MAX_SAFE_INTEGER) && '价格必须为不小于 0 的整数分。',
    [field('traffic_bytes')]: !isIntegerInRange(trafficBytes, 1, Number.MAX_SAFE_INTEGER) && '流量配额必须大于 0。',
    [field('device_limit')]: !isIntegerInRange(sku.device_limit, 1, Number.MAX_SAFE_INTEGER) && '设备数必须为大于 0 的整数。',
    [field('speed_limit_mbps')]: !isIntegerInRange(sku.speed_limit_mbps, 0, Number.MAX_SAFE_INTEGER) && '速率限制必须为不小于 0 的整数。',
    [field('sort_order')]: !isIntegerInRange(sku.sort_order, Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER) && '排序必须为整数。',
  })
}

async function openDetails(id: number) {
  skuSearch.value = ''
  skuActiveFilter.value = ''
  skuOffset.value = 0
  skuLimit.value = 25
  await setRoute({ plan: id, editor: null, sku: null })
}

async function closeDetails() {
  await setRoute({ plan: null, editor: null, sku: null })
}

async function retryDetail() {
  if (expandedPlanID.value) await loadDetail(expandedPlanID.value)
}

async function applyFilters() {
  offset.value = 0
  await setRoute({ plan: null, editor: null, sku: null })
  await refresh()
}

async function clearFilters() {
  search.value = ''
  activeFilter.value = ''
  await applyFilters()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  await setRoute({ plan: null, editor: null, sku: null })
  await refresh()
}

async function applySKUFilters() {
  skuOffset.value = 0
  await setRoute()
  await loadSKUs()
}

async function clearSKUFilters() {
  skuSearch.value = ''
  skuActiveFilter.value = ''
  await applySKUFilters()
}

async function changeSKUPage(value: { offset: number; limit: number }) {
  skuOffset.value = value.offset
  skuLimit.value = value.limit
  await setRoute()
  await loadSKUs()
}

async function refreshAll() {
  await refresh()
  if (expandedPlanID.value) await Promise.all([loadDetail(expandedPlanID.value), loadSKUs()])
}

function openCreate() {
  Object.assign(form, emptyPlanForm())
  createErrors.clear()
  createState.markClean()
  createOpen.value = true
}

async function create() {
  form.name = form.name.trim()
  form.slug = form.slug.trim().toLowerCase()
  form.summary = form.summary.trim()
  form.description = form.description.trim()
  normalizeSKUInput(form.sku)
  const firstSKUValidation = skuValidation(form.sku, 'sku.', form.traffic_bytes)
  delete firstSKUValidation['sku.traffic_bytes']
  delete firstSKUValidation['sku.sort_order']
  const valid = await createErrors.applyValidation({
    ...collectFieldErrors({
      name: !isUtf8LengthInRange(form.name, 1, 80, true) && '商品名称需包含 1–80 个 UTF-8 字节。',
      slug: !isSlug(form.slug, 80) && '商品 Slug 只能包含小写字母、数字和单个连字符。',
      summary: !isUtf8LengthInRange(form.summary, 0, 255) && '摘要不能超过 255 个 UTF-8 字节。',
      description: !isUtf8LengthInRange(form.description, 0, 20_000) && '详细说明不能超过 20,000 个 UTF-8 字节。',
      node_group_id: !isIntegerInRange(form.node_group_id, 1, Number.MAX_SAFE_INTEGER) && '请选择节点组。',
      traffic_bytes: !isIntegerInRange(form.traffic_bytes, 1, Number.MAX_SAFE_INTEGER) && '流量配额必须大于 0。',
      max_active_subscriptions: !isIntegerInRange(form.max_active_subscriptions, 0, Number.MAX_SAFE_INTEGER) && '最大有效订阅数不能小于 0。',
      family_limit: !isIntegerInRange(form.family_limit, 0, Number.MAX_SAFE_INTEGER) && '家庭共享人数不能小于 0。',
      reset_policy: !isOneOf(form.reset_policy, resetPolicies) && '请选择有效的流量重置策略。',
      traffic_calc_mode: !isOneOf(form.traffic_calc_mode, trafficCalcModes) && '请选择有效的流量计算方式。',
    }),
    ...firstSKUValidation,
  }, createFormElement, '请更正标记字段后再创建商品。')
  if (!valid) return
  saving.value = true
  message.value = ''
  try {
    const created = await createPlan({
      name: form.name,
      slug: form.slug,
      summary: form.summary,
      description: form.description,
      is_active: form.is_active,
      node_group_id: form.node_group_id,
      traffic_bytes: form.traffic_bytes,
      speed_limit_mbps: form.sku.speed_limit_mbps,
      device_limit: form.sku.device_limit,
      max_active_subscriptions: form.max_active_subscriptions,
      is_renewable: form.is_renewable,
      family_limit: form.family_limit,
      reset_policy: form.reset_policy,
      traffic_calc_mode: form.traffic_calc_mode,
      skus: [{ ...form.sku, traffic_bytes: form.traffic_bytes, is_active: true }],
    })
    createState.markClean()
    createOpen.value = false
    message.value = '商品和首个 SKU 已创建。'
    await refresh()
    if (created?.id) await setRoute({ plan: Number(created.id), editor: null, sku: null }, true)
  } catch (cause: any) {
    await createErrors.applyApiError(cause, '商品创建失败，请检查表单内容。', createFormElement, createPlanFieldMap)
  } finally {
    saving.value = false
  }
}

async function openPlanEditor() {
  if (!detailPlan.value) return
  await setRoute({ editor: 'plan', sku: null })
}

async function closePlanEditor() {
  planEditorState.markClean()
  planRevisionConflict.value = false
  planEditorOpen.value = false
  await setRoute({ editor: null })
}

function assignPlanDraft(detail: PlanDetail) {
  Object.assign(planDraft, {
    id: detail.id,
    name: detail.name,
    slug: detail.slug,
    summary: detail.summary,
    description: detail.description,
    node_group_id: detail.node_group_id,
    traffic_bytes: detail.traffic_bytes,
    speed_limit_mbps: detail.speed_limit_mbps,
    max_active_subscriptions: detail.max_active_subscriptions,
    is_renewable: detail.is_renewable,
    device_limit: detail.device_limit,
    family_limit: detail.family_limit,
    reset_policy: detail.reset_policy,
    traffic_calc_mode: detail.traffic_calc_mode,
    is_active: detail.is_active,
    sort_order: detail.sort_order,
    revision: detail.revision,
    updated_at: detail.updated_at,
  })
}

async function reloadPlanEditor() {
  if (!planDraft.id) return
  await loadDetail(planDraft.id)
  if (!detailPlan.value || detailPlan.value.id !== planDraft.id) return
  assignPlanDraft(detailPlan.value)
  planRevisionConflict.value = false
  planErrors.clear()
  planEditorState.markClean()
}

function planValidation() {
  return collectFieldErrors({
    name: !isUtf8LengthInRange(planDraft.name, 1, 80, true) && '商品名称需包含 1–80 个 UTF-8 字节。',
    slug: !isSlug(planDraft.slug, 80) && '商品 Slug 只能包含小写字母、数字和单个连字符。',
    summary: !isUtf8LengthInRange(planDraft.summary, 0, 255) && '摘要不能超过 255 个 UTF-8 字节。',
    description: !isUtf8LengthInRange(planDraft.description, 0, 20_000) && '详细说明不能超过 20,000 个 UTF-8 字节。',
    node_group_id: !isIntegerInRange(planDraft.node_group_id, 1, Number.MAX_SAFE_INTEGER) && '请选择节点组。',
    traffic_bytes: !isIntegerInRange(planDraft.traffic_bytes, 1, Number.MAX_SAFE_INTEGER) && '流量配额必须大于 0。',
    speed_limit_mbps: !isIntegerInRange(planDraft.speed_limit_mbps, 0, Number.MAX_SAFE_INTEGER) && '速率限制必须为不小于 0 的整数。',
    max_active_subscriptions: !isIntegerInRange(planDraft.max_active_subscriptions, 0, Number.MAX_SAFE_INTEGER) && '最大有效订阅数不能小于 0。',
    device_limit: !isIntegerInRange(planDraft.device_limit, 1, Number.MAX_SAFE_INTEGER) && '设备数必须为大于 0 的整数。',
    family_limit: !isIntegerInRange(planDraft.family_limit, 0, Number.MAX_SAFE_INTEGER) && '家庭共享人数不能小于 0。',
    reset_policy: !isOneOf(planDraft.reset_policy, resetPolicies) && '请选择有效的流量重置策略。',
    traffic_calc_mode: !isOneOf(planDraft.traffic_calc_mode, trafficCalcModes) && '请选择有效的流量计算方式。',
    sort_order: !isIntegerInRange(planDraft.sort_order, Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER) && '排序必须为整数。',
  })
}

async function savePlan() {
  planDraft.name = planDraft.name.trim()
  planDraft.slug = planDraft.slug.trim().toLowerCase()
  planDraft.summary = planDraft.summary.trim()
  planDraft.description = planDraft.description.trim()
  const valid = await planErrors.applyValidation(planValidation(), planFormElement, '请更正标记字段后再保存商品。')
  if (!valid) return
  saving.value = true
  try {
    await updatePlan(planDraft.id, {
      name: planDraft.name,
      slug: planDraft.slug,
      summary: planDraft.summary,
      description: planDraft.description,
      node_group_id: planDraft.node_group_id,
      traffic_bytes: planDraft.traffic_bytes,
      speed_limit_mbps: planDraft.speed_limit_mbps,
      max_active_subscriptions: planDraft.max_active_subscriptions,
      is_renewable: planDraft.is_renewable,
      device_limit: planDraft.device_limit,
      family_limit: planDraft.family_limit,
      reset_policy: planDraft.reset_policy,
      traffic_calc_mode: planDraft.traffic_calc_mode,
      is_active: planDraft.is_active,
      sort_order: planDraft.sort_order,
      expected_revision: planDraft.revision,
    })
    planEditorState.markClean()
    planEditorOpen.value = false
    await setRoute({ editor: null }, true)
    message.value = '商品信息和套餐策略已保存。'
    await refreshAll()
  } catch (cause: any) {
    if (cause?.response?.status === 409 || cause?.response?.status === 428) {
      planRevisionConflict.value = true
      planErrors.formError.value = '服务器版本已变化。请重新加载最新商品信息后再保存。'
    } else {
      await planErrors.applyApiError(cause, '商品保存失败，请检查表单内容。', planFormElement, planFieldMap)
    }
  } finally {
    saving.value = false
  }
}

async function openCreateSKU() {
  if (!detailPlan.value) return
  await setRoute({ editor: null, sku: 'new' })
}

async function openSKU(sku: PlanSKU) {
  await setRoute({ editor: null, sku: String(sku.id) })
}

async function closeSKU() {
  skuState.markClean()
  skuOpen.value = false
  await setRoute({ sku: null })
}

function skuPayload() {
  return {
    code: skuDraft.code,
    name: skuDraft.name,
    sku_type: skuDraft.sku_type,
    billing_unit: skuDraft.billing_unit,
    billing_value: skuDraft.billing_value,
    price_cents: skuDraft.price_cents,
    currency: skuDraft.currency,
    traffic_bytes: skuDraft.traffic_bytes,
    device_limit: skuDraft.device_limit,
    speed_limit_mbps: skuDraft.speed_limit_mbps,
    is_active: skuDraft.is_active,
    sort_order: skuDraft.sort_order,
  }
}

async function saveSKU() {
  normalizeSKUInput(skuDraft)
  const valid = await skuErrors.applyValidation(skuValidation(skuDraft), skuFormElement, '请更正标记字段后再保存 SKU。')
  if (!valid || !expandedPlanID.value) return
  saving.value = true
  try {
    if (skuDraft.id) {
      await updatePlanSKU(skuDraft.id, skuPayload())
    } else {
      await createPlanSKU(expandedPlanID.value, skuPayload())
    }
    const action = skuDraft.id ? '保存' : '创建'
    const code = skuDraft.code
    skuState.markClean()
    skuOpen.value = false
    await setRoute({ sku: null }, true)
    message.value = `SKU ${code} 已${action}。`
    await refreshAll()
  } catch (cause: any) {
    await skuErrors.applyApiError(cause, 'SKU 保存失败，请检查表单内容。', skuFormElement, skuFieldMap)
  } finally {
    saving.value = false
  }
}

async function toggleActive(plan: PlanDetail) {
  const publishing = !plan.is_active
  const confirmed = await confirmAction({
    title: publishing ? '发布商品？' : '将商品转为草稿？',
    message: publishing
      ? `发布“${plan.name}”后，所有可售 SKU 将进入用户可购买目录。`
      : `转为草稿后，“${plan.name}”将立即从新购目录中移除，已有订单和订阅不受影响。`,
    confirmText: publishing ? '确认发布' : '转为草稿',
    tone: publishing ? 'primary' : 'danger',
  })
  if (!confirmed) return
  statusUpdating.value = true
  error.value = ''
  try {
    await updatePlan(plan.id, { is_active: publishing, expected_revision: plan.revision })
    message.value = publishing ? '商品已发布。' : '商品已转为草稿。'
    await refreshAll()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '商品状态更新失败。'
  } finally {
    statusUpdating.value = false
  }
}

watch(() => route.fullPath, async () => {
  const nextSearch = String(route.query.q || '')
  const nextActive = String(route.query.active || '')
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  const listChanged = nextSearch !== search.value
    || nextActive !== activeFilter.value
    || nextLimit !== limit.value
    || nextOffset !== offset.value
  if (listChanged) {
    search.value = nextSearch
    activeFilter.value = nextActive
    limit.value = nextLimit
    offset.value = nextOffset
    await refresh()
  }
  const nextSKUSearch = String(route.query.sku_q || '')
  const nextSKUActive = String(route.query.sku_active || '')
  const rawSKULimit = Number(route.query.sku_limit)
  const nextSKULimit = allowedPageSizes.includes(rawSKULimit) ? rawSKULimit : 25
  const nextSKUOffset = (Math.max(1, Number(route.query.sku_page) || 1) - 1) * nextSKULimit
  const skuListChanged = nextSKUSearch !== skuSearch.value
    || nextSKUActive !== skuActiveFilter.value
    || nextSKULimit !== skuLimit.value
    || nextSKUOffset !== skuOffset.value
  if (skuListChanged) {
    skuSearch.value = nextSKUSearch
    skuActiveFilter.value = nextSKUActive
    skuLimit.value = nextSKULimit
    skuOffset.value = nextSKUOffset
  }
  const previousPlanID = expandedPlanID.value
  await syncDetailAndEditorsFromRoute()
  if (skuListChanged && expandedPlanID.value && previousPlanID === expandedPlanID.value) await loadSKUs()
})

onBeforeRouteUpdate(async (to, from) => {
  const planEditorChanging = String(to.query.editor || '') !== String(from.query.editor || '')
  const skuEditorChanging = String(to.query.sku || '') !== String(from.query.sku || '')
  if (planEditorChanging && planEditorOpen.value && planEditorState.dirty.value) {
    const confirmed = await confirmAction({
      title: '放弃未保存的商品修改？',
      message: '离开商品编辑器后，当前商品信息和套餐策略草稿将丢失。',
      confirmText: '放弃修改',
      tone: 'danger',
    })
    if (!confirmed) return false
    planEditorState.markClean()
  }
  if (skuEditorChanging && skuOpen.value && skuState.dirty.value) {
    const confirmed = await confirmAction({
      title: '放弃未保存的 SKU 修改？',
      message: '离开销售规格编辑器后，当前 SKU 草稿将丢失。',
      confirmText: '放弃修改',
      tone: 'danger',
    })
    if (!confirmed) return false
    skuState.markClean()
  }
  return true
})

onMounted(async () => {
  await refresh()
  await syncDetailAndEditorsFromRoute()
})

onBeforeUnmount(() => {
  detailRequestSequence++
  detailController?.abort()
  skuDetailController?.abort()
  invalidateSKUs()
})
</script>

<style scoped>
.detail-loading {
  display: grid;
  min-height: 180px;
  place-items: center;
}

.detail-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}

.editor-version-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.detail-kv {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 0 0 14px;
  border-block: 1px solid var(--line);
}

.detail-kv > div {
  min-width: 0;
  padding: 11px 4px;
}

.detail-kv > div:nth-child(even) {
  padding-left: 14px;
  border-left: 1px solid var(--line);
}

.detail-kv > div:nth-child(n + 3) {
  border-top: 1px solid var(--line);
}

.detail-kv dt {
  color: var(--muted);
  font-size: 9px;
}

.detail-kv dd {
  min-width: 0;
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  font-size: 11px;
  font-weight: 650;
}

.plan-description {
  margin: 0 0 16px;
  padding: 12px 0;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  font-size: 11px;
  line-height: 1.65;
  white-space: pre-wrap;
}

.detail-section-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin: 0 0 10px;
}

.detail-section-heading h3,
.detail-section-heading p {
  margin: 0;
}

.detail-section-heading h3 {
  font-size: 13px;
}

.detail-section-heading p,
.detail-section-heading > span {
  color: var(--muted);
  font-size: 9px;
}

.detail-section-heading p {
  margin-top: 3px;
}

.form-section {
  display: grid;
  gap: 16px;
  padding: 18px 0;
  border-block: 1px solid var(--line);
}

.form-section + .form-section {
  border-top: 0;
}

.form-section-title {
  display: flex;
  gap: 10px;
}

.form-section-title > span {
  width: 27px;
  height: 27px;
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border-radius: 50%;
  color: var(--text-inverse);
  background: var(--primary);
  font-size: 11px;
  font-weight: 700;
}

.form-section-title h3 {
  margin: 1px 0 2px;
  font-size: 14px;
}

.form-section-title p {
  margin: 0;
  color: var(--muted);
  font-size: 11px;
}

@media (max-width: 520px) {
  :deep(.plan-table) {
    width: 100%;
    min-width: 100% !important;
    table-layout: fixed;
  }

  :deep(.plan-table .table-primary-column) {
    width: 150px;
    min-width: 150px;
    max-width: 150px;
  }

  :deep(.plan-table th:nth-child(2)),
  :deep(.plan-table td:nth-child(2)) {
    width: 64px;
  }

  :deep(.plan-table th:nth-child(5)),
  :deep(.plan-table td:nth-child(5)) {
    width: 44px;
  }

  :deep(.plan-table .table-action-column) {
    width: 68px;
    min-width: 68px;
  }

  :deep(.plan-table .cell-title strong),
  :deep(.plan-table .cell-title span) {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :deep(.plan-table .table-action-column .ui-button) {
    min-width: 0;
    padding-inline: 9px;
  }

  .detail-kv {
    grid-template-columns: 1fr;
  }

  .detail-kv > div:nth-child(even) {
    padding-left: 4px;
    border-left: 0;
  }

  .detail-kv > div:nth-child(n + 2) {
    border-top: 1px solid var(--line);
  }

  .detail-toolbar > :deep(.ui-button) {
    flex: 1 1 auto;
  }
}
</style>
