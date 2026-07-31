<template>
  <section class="standard-page">
    <PageHeader title="协议服务" description="协议配置可复用、复制并切换承载 VPS；保存为运行实例后，系统自动校验并发布完整 Zero 配置。" eyebrow="Infrastructure">
      <template #actions>
        <PageRefreshButton label="刷新协议服务" :loading="loading" @click="refresh" />
        <UiButton  type="button" @click="openCreate"><UiIcon name="plus" />创建协议服务</UiButton>
      </template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="协议操作已完成" error-title="协议操作失败" />
    <PageAlert v-if="mieruUnavailableReason" tone="warning" title="Mieru 暂不可用">
      {{ mieruUnavailableReason }} 已有 Mieru 记录会保留供查看和停用，但不会进入新订阅或节点发布。
    </PageAlert>

    <section class="protocol-status-overview" aria-label="按发布状态查看协议服务">
      <OverviewCard
        v-for="item in protocolStatusOverview"
        :key="item.value || 'all'"
        :label="item.label"
        :value="formatNumber(item.count)"
        :description="item.caption"
        :icon="item.icon"
        :tone="item.tone"
        interactive
        :selected="filters.deployment === item.value"
        :loading="overviewLoading"
        :disabled="overviewLoading"
        @select="selectDeploymentStatus(item.value)"
      />
    </section>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing" :density="density" show-density @update:density="setDensity">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(filters.q || filters.protocol || filters.active || filters.deployment)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="filters.q" label="搜索" placeholder="服务名称或对外地址" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="filters.protocol" label="协议类型" :options="protocolFilterOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="filters.active" label="服务状态" :options="activeFilterOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="filters.deployment" label="发布状态" :options="deploymentFilterOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions><UiButton :variant="groupedByNode ? 'secondary' : 'ghost'" size="sm" type="button" @click="setGroupedView(!groupedByNode)"><UiIcon name="nodes" />{{ groupedByNode ? '节点分组' : '按节点分组' }}</UiButton></template>
      <template #selection>
        <div v-if="selectedEndpointIDs.length || selectionAllMatching" class="bulk-action-bar">
          <div><strong>已选择 {{ selectedEndpointCount }} 个协议服务</strong><span v-if="selectionAllMatching">范围：当前全部筛选结果</span><span v-else>范围：已勾选行</span><UiButton v-if="canSelectAllMatching" variant="ghost" size="sm" type="button" @click="selectAllMatching">选择全部 {{ total }} 条筛选结果</UiButton></div>
          <div><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'deploy'" @click="runProtocolBatch('deploy')"><UiIcon name="play" />批量发布</UiButton><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'enable'" @click="runProtocolBatch('enable')"><UiIcon name="check" />批量启用</UiButton><UiButton variant="danger" size="sm" type="button" :loading="bulkBusy === 'disable'" @click="runProtocolBatch('disable')">批量停用</UiButton><UiButton variant="ghost" size="sm" type="button" @click="clearSelection">清除</UiButton></div>
        </div>
      </template>
      <DataTable v-if="endpoints.length" caption="协议服务列表；可按服务、节点、协议和倍率排序，数量直接显示数字，时间保留精确时间提示" :row-count="total" :density="density" :min-width="1120" selectable table-class="protocol-table">
          <thead><tr>
            <th class="selection-column"><UiCheckbox :model-value="allPageEndpointsSelected" :indeterminate="pageEndpointSelectionIndeterminate" :disabled="selectionAllMatching" aria-label="选择当前页全部协议服务" @update:model-value="toggleCurrentEndpointPage" /></th>
            <SortableHeader field="name" label="服务" :sort-field="effectiveSortField" :direction="effectiveSortDirection" pinned="start" @sort="setSort" />
            <SortableHeader field="node_id" label="承载节点" :sort-field="effectiveSortField" :direction="effectiveSortDirection" :priority="2" @sort="setSort" />
            <SortableHeader field="protocol" label="协议" :sort-field="effectiveSortField" :direction="effectiveSortDirection" :priority="2" @sort="setSort" />
            <th data-column-priority="3">对外入口</th><th>服务状态</th><th>发布状态</th><th class="numeric-column" data-column-priority="3">连接数</th><th class="numeric-column" data-column-priority="3">凭证数</th><th data-column-priority="3">今日流量</th>
            <SortableHeader field="multiplier" label="倍率" :sort-field="effectiveSortField" :direction="effectiveSortDirection" numeric :priority="3" @sort="setSort" />
            <th data-column-priority="3">最近使用</th><th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr></thead>
          <tbody>
            <template v-for="(endpoint, index) in endpoints" :key="endpoint.id">
            <tr v-if="groupedByNode && isFirstNodeGroup(index)" class="protocol-group-row"><td colspan="13"><div class="protocol-group-content"><UiIcon name="nodes" /><strong>{{ endpoint.node_name || `VPS #${endpoint.node_id}` }}</strong><span>节点 #{{ endpoint.node_id }}</span></div></td></tr>
            <tr :class="{ 'batch-selected': isEndpointSelected(endpoint.id) }">
              <td class="selection-column"><UiCheckbox :model-value="isEndpointSelected(endpoint.id)" :disabled="selectionAllMatching" :aria-label="`选择协议服务 ${endpoint.name}`" @update:model-value="toggleEndpointSelection(endpoint.id, $event)" /></td>
              <td class="table-primary-column"><div class="cell-title"><strong>{{ endpoint.name }}</strong><span>#{{ endpoint.id }}</span></div></td>
              <td data-column-priority="2"><RouterLink :to="adminContextLink('/admin/nodes', { node: String(endpoint.node_id) })">{{ endpoint.node_name || `VPS #${endpoint.node_id}` }}</RouterLink></td>
              <td data-column-priority="2"><StatusBadge :tone="endpoint.kernel_supported ? 'info' : 'warning'" :icon="endpoint.kernel_supported ? 'activity' : 'alert'">{{ protocolLabel(endpoint.protocol) }}</StatusBadge></td>
              <td class="mono" data-column-priority="3">{{ endpoint.address }}:{{ endpoint.public_port || endpoint.port }}</td>
              <td><StatusBadge v-if="!endpoint.kernel_supported" tone="warning" icon="alert">内核不支持</StatusBadge><StatusBadge v-else :tone="endpoint.is_active ? 'success' : 'neutral'" :icon="endpoint.is_active ? 'check' : 'minus'">{{ endpoint.is_active ? '运行中' : '已停用' }}</StatusBadge></td>
              <td><StatusBadge :tone="deploymentTone(endpoint.latest_deployment?.status)" :icon="deploymentIcon(endpoint.latest_deployment?.status)">{{ deploymentLabel(endpoint.latest_deployment?.status) }}</StatusBadge></td>
              <td class="numeric-column" data-column-priority="3">{{ formatNumber(endpoint.usage?.active_flows) }}</td>
              <td class="numeric-column" data-column-priority="3">{{ formatNumber(endpoint.usage?.active_credentials) }}</td>
              <td data-column-priority="3">{{ formatBytes(endpoint.usage?.used_bytes_today) }}</td>
              <td class="numeric-column" data-column-priority="3">{{ formatMultiplierNumber(endpoint.multiplier_milli) }}</td>
              <td data-column-priority="3"><TimeBadge :value="endpoint.usage?.last_used_at" /></td>
              <td class="table-action-column"><RowActions :label="`${endpoint.name} 的操作`" :trigger-key="`protocol-${endpoint.id}`"><UiButton variant="ghost" size="sm" type="button" :data-protocol-detail-trigger="endpoint.id" :loading="detailLoadingID === endpoint.id" :aria-label="`查看协议服务 ${endpoint.name}`" @click="openDetail(endpoint)"><UiIcon name="search" />查看</UiButton><UiButton variant="ghost" size="sm" type="button" :aria-label="`编辑协议服务 ${endpoint.name}`" @click="openEdit(endpoint)"><UiIcon name="edit" />编辑</UiButton><UiButton variant="ghost" size="sm" type="button" :disabled="!endpoint.kernel_supported" :title="endpoint.kernel_unsupported_reason" :aria-label="`复制协议服务 ${endpoint.name}`" @click="openCopy(endpoint)"><UiIcon name="copy" />复制</UiButton><RouterLink v-if="endpoint.latest_deployment?.has_error" class="button button-ghost button-sm" :aria-label="`查看协议服务 ${endpoint.name} 的失败日志`" :to="adminContextLink('/admin/operation-logs', { source: 'protocol_publish', status: 'failed', protocol_endpoint_id: String(endpoint.id) })"><UiIcon name="terminal" />日志</RouterLink><UiButton variant="secondary" size="sm" type="button" :disabled="!endpoint.kernel_supported" :title="endpoint.kernel_unsupported_reason" :loading="deployingID === endpoint.id" :aria-label="`发布协议服务 ${endpoint.name}`" @click="deploy(endpoint)"><UiIcon name="play" />发布</UiButton><UiButton variant="danger" size="sm" type="button" :loading="deletingID === endpoint.id" @click="removeEndpoint(endpoint)"><UiIcon name="trash" />删除</UiButton></RowActions></td>
            </tr>
            </template>
          </tbody>
      </DataTable>
      <EmptyState v-else-if="!initialLoading" icon="activity" :title="filters.q || filters.protocol || filters.active || filters.deployment ? '没有匹配服务' : '还没有协议服务'" :description="filters.q || filters.protocol || filters.active || filters.deployment ? '调整或清除筛选条件后重试。' : '选择一台 VPS，填写连接参数后即可创建。'"><template #actions><UiButton v-if="!filters.q && !filters.protocol && !filters.active && !filters.deployment"  type="button" @click="openCreate"><UiIcon name="plus" />创建协议服务</UiButton></template></EmptyState>
      <template #footer><TablePager variant="stripe" :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(selectedEndpointDetail)" :title="selectedEndpointDetail?.name || '协议服务详情'" eyebrow="Protocol endpoint" :description="selectedEndpointSummary ? `${selectedEndpointSummary.node_name || `VPS #${selectedEndpointSummary.node_id}`} · ${selectedEndpointSummary.address}:${selectedEndpointSummary.public_port || selectedEndpointSummary.port}` : ''" :return-focus-selector="selectedEndpointDetail ? `[data-row-action-trigger='protocol-${selectedEndpointDetail.id}']` : ''" @close="closeDetail">
      <main v-if="selectedEndpointDetail" class="stack protocol-detail">
        <PageAlert v-if="!selectedEndpointDetail.kernel_supported" tone="warning" title="当前内核无法使用此协议">
          {{ selectedEndpointDetail.kernel_unsupported_reason }} 该记录仅保留供查看；如仍处于启用状态，请编辑并将其停用。
        </PageAlert>
        <section class="detail-status-strip" aria-label="协议服务状态">
          <StatusBadge :tone="selectedEndpointDetail.is_active ? 'success' : 'neutral'" :icon="selectedEndpointDetail.is_active ? 'check' : 'minus'">{{ selectedEndpointDetail.is_active ? '运行中' : '已停用' }}</StatusBadge>
          <StatusBadge :tone="deploymentTone(selectedEndpointDetail.latest_deployment?.status)" :icon="deploymentIcon(selectedEndpointDetail.latest_deployment?.status)">{{ deploymentLabel(selectedEndpointDetail.latest_deployment?.status) }}</StatusBadge>
          <StatusBadge tone="info" icon="activity">{{ protocolLabel(selectedEndpointDetail.protocol) }}</StatusBadge>
        </section>
        <div v-if="selectedEndpointSummary" class="protocol-mobile-detail-actions" aria-label="协议服务操作">
          <UiButton variant="secondary" size="sm" type="button" @click="editSelectedEndpoint"><UiIcon name="edit" />编辑服务</UiButton>
          <UiButton variant="secondary" size="sm" type="button" :disabled="!selectedEndpointDetail.kernel_supported" :title="selectedEndpointDetail.kernel_unsupported_reason" @click="copySelectedEndpoint"><UiIcon name="copy" />复制配置</UiButton>
          <RouterLink v-if="selectedEndpointSummary.latest_deployment?.has_error" class="button button-ghost button-sm" :to="adminContextLink('/admin/operation-logs', { source: 'protocol_publish', status: 'failed', protocol_endpoint_id: String(selectedEndpointSummary.id) })"><UiIcon name="terminal" />失败日志</RouterLink>
          <UiButton variant="secondary" size="sm" type="button" :disabled="!selectedEndpointDetail.kernel_supported" :title="selectedEndpointDetail.kernel_unsupported_reason" :loading="deployingID === selectedEndpointSummary.id" @click="deploy(selectedEndpointSummary)"><UiIcon name="play" />发布配置</UiButton>
        </div>
        <section class="panel detail-facts">
          <div><span>承载节点</span><RouterLink :to="adminContextLink('/admin/nodes', { node: String(selectedEndpointDetail.node_id) })">{{ selectedEndpointSummary?.node_name || `VPS #${selectedEndpointDetail.node_id}` }}</RouterLink></div>
          <div><span>对外入口</span><strong class="mono">{{ selectedEndpointDetail.address }}:{{ selectedEndpointDetail.public_port || selectedEndpointDetail.port }}</strong></div>
          <div><span>监听端口</span><strong>{{ formatNumber(selectedEndpointDetail.port) }}</strong></div>
          <div><span>计费倍率</span><strong>{{ formatMultiplierNumber(selectedEndpointDetail.multiplier_milli) }}×</strong></div>
          <div><span>活跃连接</span><strong>{{ formatNumber(selectedEndpointDetail.usage?.active_flows) }}</strong></div>
          <div><span>活跃凭证</span><strong>{{ formatNumber(selectedEndpointDetail.usage?.active_credentials) }}</strong></div>
          <div><span>今日流量</span><strong>{{ formatBytes(selectedEndpointDetail.usage?.used_bytes_today) }}</strong></div>
          <div><span>累计流量</span><strong>{{ formatBytes(selectedEndpointDetail.usage?.used_bytes_total) }}</strong></div>
          <div><span>最近使用</span><TimeBadge :value="selectedEndpointDetail.usage?.last_used_at" /></div>
          <div><span>最后更新</span><TimeBadge :value="selectedEndpointDetail.updated_at" /></div>
        </section>
        <section class="panel deployment-history">
          <header class="panel-header"><div><h2>发布历史</h2><p>按需分页读取；错误和输出先清理控制字符，再限制列表展示长度。</p></div><span class="numeric-summary">{{ deploymentTotal }}</span></header>
          <PageAlert v-if="deploymentError" tone="danger" title="发布历史加载失败">{{ deploymentError }}</PageAlert>
          <DataTable v-if="deployments.length" caption="协议服务发布历史" :row-count="deploymentTotal" :min-width="760">
            <thead>
              <tr>
                <th class="table-primary-column">发布</th>
                <th>状态</th>
                <th>输出摘要</th>
                <th data-column-priority="2">开始时间</th>
                <th data-column-priority="2">完成时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in deployments" :key="item.id">
                <td class="table-primary-column">
                  <div class="cell-title"><strong>#{{ item.id }}</strong><span>修订 {{ formatNumber(item.config_revision) }}</span></div>
                </td>
                <td><StatusBadge :tone="deploymentTone(item.status)" :icon="deploymentIcon(item.status)">{{ deploymentLabel(item.status) }}</StatusBadge></td>
                <td><p v-if="deploymentSummary(item)" class="deployment-output">{{ deploymentSummary(item) }}</p><span v-else>—</span></td>
                <td data-column-priority="2"><TimeBadge :value="item.started_at || item.created_at" /></td>
                <td data-column-priority="2"><TimeBadge :value="item.finished_at" /></td>
              </tr>
            </tbody>
          </DataTable>
          <EmptyState v-else-if="!deploymentLoading && !deploymentError" icon="activity" title="还没有发布记录" description="创建或手动发布协议服务后，结果会出现在这里。" />
          <TablePager :total="deploymentTotal" :offset="deploymentOffset" :limit="deploymentLimit" :loading="deploymentLoading" @change="changeDeploymentPage" />
        </section>
      </main>
    </DetailDrawer>

    <ModalDialog :open="editorOpen" :dirty="editorState.dirty.value" :title="form.id ? '编辑协议服务' : copySourceID ? '复制协议配置' : '创建协议服务'" :description="form.id ? '协议配置可切换承载 VPS；保存前会校验凭证、端口和完整配置。' : copySourceID ? '已复制原服务配置为独立草稿，可更换节点、入口和名称后保存。' : '跟随步骤完成节点、连接参数和配置确认。'" size="xl" :busy="saving" @close="closeEditor">
      <form id="protocol-form" ref="protocolFormElement" class="protocol-editor" novalidate @submit.prevent="save">
        <UiStepNav :steps="wizardSteps" :current="editorStep" :max-step="form.id ? 3 : editorStep" label="协议服务配置步骤" @select="goToStep" />
        <PageAlert v-if="editorErrors.formError.value || editorError" tone="danger" title="无法保存协议服务">{{ editorErrors.formError.value || editorError }}</PageAlert>
        <PageAlert v-if="!selectedProtocolCapability.supported" tone="warning" title="当前内核不支持所选协议">
          {{ selectedProtocolCapability.reason }}<template v-if="form.id"> 此历史记录只能在关闭“启用服务”后保存，以便从节点配置中移除。</template>
        </PageAlert>

        <section v-if="editorStep === 1" class="wizard-panel">
          <header class="wizard-heading"><span><UiIcon name="nodes" /></span><div><h3>选择承载节点和对外入口</h3><p>选择 VPS 后会自动使用节点资产中维护的默认对外地址，仍可在这里改成域名或其他入口地址。</p></div></header>
          <div class="guided-grid">
            <FormField v-slot="{ controlAttrs }" label="承载 VPS" name="protocol-node" hint="配置本身可复用；保存为运行实例时选择承载节点，编辑后也可安全切换。" :error="editorErrors.fields.node_id" required full><NodeLookup v-model="form.node_id" v-bind="controlAttrs" @select="handleNodeSelect" /></FormField>
            <div v-if="selectedNode" class="selected-node-card field-full"><span class="protocol-icon"><UiIcon name="nodes" /></span><div><strong>{{ selectedNode.name }}</strong><p>{{ selectedNode.region || '未设置区域' }} · {{ selectedNode.address || '节点资产中还没有默认对外地址' }}</p></div><StatusBadge :tone="selectedNode.is_enabled ? 'success' : 'warning'">{{ selectedNode.is_enabled ? '可用' : '已停用' }}</StatusBadge></div>
            <FormField v-slot="{ controlAttrs }" label="协议类型" name="protocol-type" :hint="form.id ? '已创建服务不直接切换协议，避免破坏现有用户凭证；如需更换请创建新服务。' : ''" :error="editorErrors.fields.protocol" required><UiSelect v-model="form.protocol" v-bind="controlAttrs" :options="protocolOptions" :disabled="Boolean(form.id)" @change="handleProtocolChange" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="服务名称" name="protocol-name" :error="editorErrors.fields.name" required><UiInput v-model.trim="form.name" v-bind="controlAttrs" placeholder="例如：香港 VLESS 01" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="对外地址" name="protocol-address" :hint="selectedNode?.address ? `已从 ${selectedNode.name} 自动带出，可按实际入口修改。` : '当前节点没有默认地址，请填写客户端可访问的域名或公网 IP。'" :error="editorErrors.fields.address" required full><div class="input-with-action"><UiInput v-model.trim="form.address" v-bind="controlAttrs" placeholder="域名或公网 IP" /><UiButton v-if="selectedNode?.address && form.address !== selectedNode.address" type="button" @click="useNodeAddress">使用节点地址</UiButton></div></FormField>
            <FormField v-slot="{ controlAttrs }" label="服务监听端口" name="protocol-port" hint="Zero 在 VPS 上实际监听的端口；已有用户的 Shadowsocks 服务需新建后迁移端口。" :error="editorErrors.fields.port" required><PortInput v-model="form.port" v-bind="controlAttrs" :disabled="Boolean(form.id && form.protocol === 'shadowsocks')" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="客户端连接端口" name="protocol-public-port" hint="存在端口转发时可与监听端口不同。" :error="editorErrors.fields.public_port" required><PortInput v-model="form.public_port" v-bind="controlAttrs" :disabled="Boolean(form.id && form.protocol === 'shadowsocks')" /></FormField>
          </div>
        </section>

        <section v-else-if="editorStep === 2" class="wizard-panel">
          <header class="wizard-heading"><span><UiIcon name="key" /></span><div><h3>设置 {{ protocolLabel(form.protocol) }} 服务参数</h3><p>这里只设置服务能力；用户凭证由订阅开通流程自动生成并关联。</p></div></header>
          <div class="guided-grid">
            <div v-if="usesManagedCredentials" class="generated-config-note field-full"><UiIcon name="shield" /><div><strong>用户凭证按订阅生成</strong><p>每个订阅在这个服务上拥有独立凭证和稳定归属标识；Shadowsocks 同时分配独立 UDP/TCP 端口，客户端与服务端只共享该订阅自己的 PSK。</p></div></div>
            <template v-else-if="form.protocol !== 'mieru'">
              <FormField v-slot="{ controlAttrs }" label="连接密码" name="protocol-password" :error="editorErrors.fields['structured.password']" full required><div class="input-with-action"><UiInput v-model="structured.password" v-bind="controlAttrs" type="password" autocomplete="new-password" /><UiButton type="button" @click="structured.password = randomSecret()">重新生成</UiButton></div></FormField>
            </template>
            <div v-else class="generated-config-note field-full"><UiIcon name="shield" /><div><strong>Mieru 凭据由系统生成</strong><p>创建时自动生成端点凭据并加密保存；订阅输出会自动补齐连接参数，不需要手工维护用户名或密码。</p></div></div>
            <FormField v-if="form.protocol === 'vmess'" v-slot="{ controlAttrs }" label="VMess 加密方式"><UiSelect v-model="structured.cipher" v-bind="controlAttrs" :options="vmessCipherOptions" /></FormField>
            <FormField v-if="form.protocol === 'shadowsocks'" v-slot="{ controlAttrs }" label="Shadowsocks 加密方式"><UiSelect v-model="structured.cipher" v-bind="controlAttrs" :options="shadowsocksCipherOptions" /></FormField>
            <FormField v-if="form.protocol === 'vless'" v-slot="{ controlAttrs }" label="传输安全"><UiSelect v-model="structured.security" v-bind="controlAttrs" :options="securityOptions" /></FormField>
            <template v-if="usesTLS">
              <FormField v-slot="{ controlAttrs }" label="TLS 证书来源" name="protocol-managed-certificate" :error="editorErrors.fields.managed_certificate_id" hint="选择本节点已签发证书；续期后会自动重新发布完整节点配置。" full><UiSelect v-model="form.managed_certificate_id" v-bind="controlAttrs" :options="managedCertificateOptions" /></FormField>
              <FormField v-if="!form.managed_certificate_id" v-slot="{ controlAttrs }" label="证书文件路径" name="protocol-cert-path" :error="editorErrors.fields['structured.cert_path']" :required="requiresTLSFiles"><UiInput v-model.trim="structured.cert_path" v-bind="controlAttrs" placeholder="/etc/zero/tls/cert.pem" /></FormField>
              <FormField v-if="!form.managed_certificate_id" v-slot="{ controlAttrs }" label="私钥文件路径" name="protocol-key-path" :error="editorErrors.fields['structured.key_path']" :required="requiresTLSFiles"><UiInput v-model.trim="structured.key_path" v-bind="controlAttrs" placeholder="/etc/zero/tls/key.pem" /></FormField>
              <FormField v-if="form.protocol !== 'hysteria2'" v-slot="{ controlAttrs }" :label="form.protocol === 'trojan' ? 'TLS 域名（SNI）' : '证书域名'" hint="通常与对外地址一致；使用 IP 连接时请填写证书对应域名。" full><UiInput v-model.trim="structured.server_name" v-bind="controlAttrs" :placeholder="form.address || 'edge.example.com'" /></FormField>
              <div v-if="!managedCertificateOptions.length || (managedCertificateOptions.length === 1 && managedCertificateOptions[0].value === 0)" class="generated-config-note field-full"><UiIcon name="shield" /><div><strong>本节点暂无可用托管证书</strong><p>可先到 <RouterLink to="/admin/certificates">免费证书</RouterLink> 完成申请，再返回选择使用。</p></div></div>
            </template>
            <div class="generated-config-note field-full"><UiIcon name="check" /><div><strong>配置由系统生成</strong><p>系统会把服务参数转换为 Zero 配置，并在订阅开通、续费、到期时自动更新节点；原始 JSON 仅在最后一步的高级设置中提供。</p></div></div>
          </div>
        </section>

        <section v-else class="wizard-panel review-panel">
          <header class="wizard-heading"><span><UiIcon name="check" /></span><div><h3>确认服务信息</h3><p>保存后立即进入自动发布：校验完整配置、原子切换、重启并检查心跳，失败会回滚。</p></div></header>
          <div class="review-grid">
            <article><span>承载节点</span><strong>{{ selectedNode?.name || '未选择' }}</strong><small>{{ selectedNode?.region || '未设置区域' }}</small></article>
            <article><span>协议服务</span><strong>{{ form.name }}</strong><small>{{ protocolLabel(form.protocol) }}</small></article>
            <article><span>对外入口</span><strong class="mono">{{ form.address }}:{{ form.public_port }}</strong><small>VPS 监听 {{ form.port }}</small></article>
            <article><span>流量计费</span><strong>{{ formatMultiplier(form.multiplier_milli) }}</strong><small>前端自动换算，无需手动填写千分值</small></article>
          </div>
          <details class="advanced-settings" :open="hasConfigError">
            <summary><span><UiIcon name="settings" /></span><div><strong>高级设置</strong><small>仅在需要链式协议、排序或自定义 Zero 参数时展开。</small></div><UiIcon name="chevron" /></summary>
            <div class="advanced-body">
              <div class="guided-grid compact-grid">
                <FormField label="父协议" name="protocol-parent" hint="可选；只检索同一节点的协议端点。" :error="editorErrors.fields.parent_protocol_id"><template #default="{ controlAttrs }"><EndpointLookup v-model="form.parent_protocol_id" v-bind="controlAttrs" :node-id="form.node_id" :exclude-id="form.id" /></template></FormField>
                <FormField v-slot="{ controlAttrs }" label="流量倍率" name="protocol-multiplier" hint="显示为倍数，接口保存千分值。" :error="editorErrors.fields.multiplier_milli"><MultiplierInput v-model="form.multiplier_milli" v-bind="controlAttrs" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="排序" name="protocol-sort" :error="editorErrors.fields.sort_order"><UiNumberInput v-model="form.sort_order" v-bind="controlAttrs" inputmode="numeric" /></FormField>
                <FormField v-slot="{ controlAttrs }" label="运行状态" name="protocol-active" :error="editorErrors.fields.is_active"><div class="check-field"><UiCheckbox v-model="form.is_active" v-bind="controlAttrs" /><span>保存后启用并发布该运行实例</span></div></FormField>
              </div>
              <div class="config-grid">
                <FormField v-slot="{ controlAttrs }" label="服务端配置 JSON" name="protocol-server-config" :error="editorErrors.fields.config" required><UiTextarea v-model="form.config" v-bind="controlAttrs" rows="10" spellcheck="false"></UiTextarea></FormField>
                <FormField v-slot="{ controlAttrs }" label="客户端配置 JSON" name="protocol-client-config" :error="editorErrors.fields.client_config" required><UiTextarea v-model="form.client_config" v-bind="controlAttrs" rows="10" spellcheck="false"></UiTextarea></FormField>
                <FormField v-slot="{ controlAttrs }" label="可选配置 JSON" name="protocol-optional-config" :error="editorErrors.fields.optional_config"><UiTextarea v-model="form.optional_config" v-bind="controlAttrs" rows="5" spellcheck="false"></UiTextarea></FormField>
                <FormField v-slot="{ controlAttrs }" label="标签 JSON 数组" name="protocol-tags" :error="editorErrors.fields.tags"><UiTextarea v-model="form.tags" v-bind="controlAttrs" rows="5" spellcheck="false"></UiTextarea></FormField>
              </div>
            </div>
          </details>
        </section>
      </form>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="editorStep === 1 ? requestClose() : editorStep--">{{ editorStep === 1 ? '取消' : '上一步' }}</UiButton>
        <UiButton v-if="editorStep < 3" type="button" @click="nextStep">下一步</UiButton>
        <UiButton v-else form="protocol-form" type="submit" :disabled="!canSaveSelectedProtocol" :title="!canSaveSelectedProtocol ? selectedProtocolCapability.reason : ''" :loading="saving">{{ copySourceID ? '保存为新服务' : '保存协议服务' }}</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createProtocolBatchDeployment, createProtocolEndpoint, deleteProtocolEndpoint, deployProtocolEndpoint, fetchManagedCertificatesPage, fetchNodesPage, fetchProtocolDeployments, fetchProtocolEndpoint, fetchProtocolEndpointsPage, getVersion, updateProtocolEndpoint, updateProtocolEndpointsBatch, type AdminNodeListItem, type ManagedCertificate, type ProtocolEndpointListItem, type ProtocolKernelCapability } from '../api/client'
import DataWorkbench from '../components/DataWorkbench.vue'
import DataTable from '../components/DataTable.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EndpointLookup from '../components/EndpointLookup.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import MultiplierInput from '../components/MultiplierInput.vue'
import NodeLookup from '../components/NodeLookup.vue'
import OverviewCard from '../components/OverviewCard.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PortInput from '../components/PortInput.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import SortableHeader from '../components/SortableHeader.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiStepNav from '../components/UiStepNav.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useSelectionScope } from '../composables/useSelectionScope'
import { nextSortDirection, resolveSortDirection, resolveSortField, resolveTableDensity } from '../composables/tableState'
import { confirmAction } from '../utils/feedback'
import { formatBytes, formatNumber, formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'
import { normalizeOutput, truncateOutput } from '../utils/output'
import { trackAdminTask } from '../utils/taskTracker'
import { isIntegerInRange } from '../utils/validation'

const protocols = ['vmess', 'vless', 'trojan', 'shadowsocks', 'hysteria2', 'mieru']
const defaultMieruUnavailableReason = 'Mieru 托管归属需要 Zero 0.0.15-rc.4 或更高版本；请先升级所选节点内核。'
const protocolCapabilities = reactive<Record<string, ProtocolKernelCapability>>({
  vmess: { supported: true },
  vless: { supported: true },
  trojan: { supported: true },
  shadowsocks: { supported: true },
  hysteria2: { supported: true },
  mieru: { supported: false, reason: defaultMieruUnavailableReason },
})
const protocolOptions = computed(() => protocols.map(value => ({
  label: value === 'mieru' && !protocolCapabilities.mieru?.supported ? 'Mieru（当前内核不支持）' : protocolLabel(value),
  value,
  disabled: !protocolCapabilities[value]?.supported,
})))
const protocolFilterOptions = [{ label: '全部协议', value: '' }, ...protocols.map(value => ({ label: protocolLabel(value), value }))]
const activeFilterOptions = [{ label: '全部服务状态', value: '' }, { label: '运行中', value: 'active' }, { label: '已停用', value: 'inactive' }]
const deploymentFilterOptions = [{ label: '全部发布状态', value: '' }, { label: '已生效', value: 'succeeded' }, { label: '发布中', value: 'running' }, { label: '发布失败', value: 'failed' }, { label: '未发布', value: 'never' }]
const vmessCipherOptions = [{ label: 'AES-128-GCM（推荐）', value: 'aes-128-gcm' }, { label: 'ChaCha20-Poly1305', value: 'chacha20-poly1305' }, { label: '不额外加密', value: 'none' }]
const shadowsocksCipherOptions = [{ label: 'AES-128-GCM', value: 'aes-128-gcm' }, { label: 'AES-256-GCM', value: 'aes-256-gcm' }, { label: 'ChaCha20-Poly1305（推荐）', value: 'chacha20-ietf-poly1305' }]
const securityOptions = [{ label: '无 TLS（直接连接）', value: 'none' }, { label: 'TLS 证书', value: 'tls' }]
const wizardSteps = [{ id: 1, title: '节点与入口', caption: '选择 VPS' }, { id: 2, title: '服务参数', caption: '系统管凭证' }, { id: 3, title: '确认发布', caption: '检查配置' }]
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const filters = reactive({
  q: typeof route.query.q === 'string' ? route.query.q : '',
  protocol: typeof route.query.protocol === 'string' ? route.query.protocol : '',
  active: typeof route.query.active === 'string' ? route.query.active : '',
  deployment: typeof route.query.deployment === 'string' ? route.query.deployment : '',
})
type ProtocolSortField = 'sort_order' | 'id' | 'name' | 'node_id' | 'protocol' | 'multiplier' | 'updated_at'
const protocolSortFields = new Set<ProtocolSortField>(['sort_order', 'id', 'name', 'node_id', 'protocol', 'multiplier', 'updated_at'])
const sortField = ref(resolveSortField(route.query.sort, protocolSortFields, 'sort_order'))
const sortDirection = ref<'asc' | 'desc'>(resolveSortDirection(route.query.direction, 'asc'))
const density = ref<'compact' | 'comfortable'>(resolveTableDensity(route.query.density))
const groupedByNode = ref(route.query.view === 'nodes')
const selectedNode = ref<AdminNodeListItem | null>(null)
const managedCertificates = ref<ManagedCertificate[]>([])
const selectedEndpointDetail = ref<any | null>(null)
const selectedEndpointSummary = ref<ProtocolEndpointListItem | null>(null)
type ProtocolDeploymentStatus = '' | 'succeeded' | 'running' | 'failed' | 'never'
const overviewLoading = ref(true)
const overviewCounts = reactive<Record<ProtocolDeploymentStatus, number>>({
  '': 0,
  succeeded: 0,
  running: 0,
  failed: 0,
  never: 0,
})
const deploymentOffset = ref((Math.max(1, Number(route.query.deployment_page) || 1) - 1) * 25)
const deploymentLimit = ref(allowedPageSizes.includes(Number(route.query.deployment_limit)) ? Number(route.query.deployment_limit) : 25)
const saving = ref(false), deployingID = ref(0), deletingID = ref(0), detailLoadingID = ref(0), editorOpen = ref(false), editorStep = ref(1)
const copySourceID = ref(0)
const originalNodeID = ref(0)
const bulkBusy = ref<'' | 'deploy' | 'enable' | 'disable'>('')
const message = ref(''), editorError = ref('')
const emptyForm = () => ({ id: 0, node_id: 0, name: '', protocol: 'vless', address: '', port: 443, public_port: 443, multiplier_milli: 1000, sort_order: 0, parent_protocol_id: 0, managed_certificate_id: 0, is_active: true, config: '{}', client_config: '{}', optional_config: '{}', tags: '[]' })
const emptyStructured = () => ({ credential: randomUUID(), username: 'subscriber', password: randomSecret(), cipher: 'aes-128-gcm', security: 'none', cert_path: '', key_path: '', server_name: '' })
const form = reactive<any>(emptyForm())
const structured = reactive<any>(emptyStructured())
const protocolFormElement = ref<HTMLElement | null>(null)
const editorErrors = useFormErrors()
const protocolFieldMap: Record<string, string> = {
  node_id: 'node_id', name: 'name', protocol: 'protocol', address: 'address', port: 'port', public_port: 'public_port',
  multiplier_milli: 'multiplier_milli', sort_order: 'sort_order', parent_protocol_id: 'parent_protocol_id', managed_certificate_id: 'managed_certificate_id',
  config: 'config', client_config: 'client_config', optional_config: 'optional_config', tags: 'tags', is_active: 'is_active',
}
const editorState = useDirtyForm(() => ({ form, structured }))
useUnsavedChangesGuard(
  () => editorOpen.value && editorState.dirty.value,
  () => editorState.confirmDiscard({
    title: '放弃协议配置草稿？',
    message: '离开协议管理后，向导中尚未保存的服务参数将丢失。',
    confirmText: '离开页面',
  }),
)
const { items: endpoints, total, loading, initialLoading, refreshing, error, load: loadEndpoints } = useRemoteTable<ProtocolEndpointListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchProtocolEndpointsPage({
    offset: offset.value,
    limit: limit.value,
    q: filters.q || undefined,
    protocol: filters.protocol || undefined,
    active: filters.active ? filters.active === 'active' : undefined,
    deploymentStatus: filters.deployment || undefined,
    sort: groupedByNode.value ? 'node_id' : sortField.value,
    direction: groupedByNode.value ? 'asc' : sortDirection.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '协议服务加载失败。',
  onOffsetCorrected: () => syncURL(true),
  onPageLoaded: (page) => {
    if (!selectedEndpointDetail.value) return
    const summary = page.items.find(item => item.id === selectedEndpointDetail.value?.id)
    if (summary) selectedEndpointSummary.value = summary
  },
})
const {
  selectedIDs: selectedEndpointIDs,
  allMatching: selectionAllMatching,
  selectedCount: selectedEndpointCount,
  allPageSelected: allPageEndpointsSelected,
  pageSelectionIndeterminate: pageEndpointSelectionIndeterminate,
  canSelectAllMatching,
  isSelected: isEndpointSelected,
  toggle: toggleEndpointSelection,
  togglePage: toggleCurrentEndpointPage,
  selectAllMatching,
  clear: clearSelection,
} = useSelectionScope({ items: endpoints, total, key: endpoint => endpoint.id })
const { items: deployments, total: deploymentTotal, loading: deploymentLoading, error: deploymentError, load: loadDeployments } = useRemoteTable<any>({
  offset: deploymentOffset,
  limit: deploymentLimit,
  fetchPage: ({ signal }) => selectedEndpointDetail.value
    ? fetchProtocolDeployments({ protocolEndpointId: selectedEndpointDetail.value.id, offset: deploymentOffset.value, limit: deploymentLimit.value }, { signal })
    : Promise.resolve({ items: [], total: 0 }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '发布历史加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const usesManagedCredentials = computed(() => ['vless', 'vmess', 'shadowsocks'].includes(form.protocol))
const usesTLS = computed(() => ['vmess', 'trojan', 'hysteria2'].includes(form.protocol) || (form.protocol === 'vless' && structured.security === 'tls'))
const requiresTLSFiles = computed(() => ['vmess', 'trojan'].includes(form.protocol) || (form.protocol === 'vless' && structured.security === 'tls'))
const managedCertificateOptions = computed(() => [
  { label: '手动维护节点文件路径', value: 0 },
  ...managedCertificates.value
    .filter(item => item.not_after && new Date(item.not_after).getTime() > Date.now() && (item.status === 'active' || item.status === 'failed'))
    .map(item => ({ label: `${item.name} · ${item.domains.join('、')}`, value: item.id })),
])
const hasConfigError = computed(() => ['config', 'client_config', 'optional_config', 'tags', 'parent_protocol_id', 'multiplier_milli', 'sort_order'].some(field => Boolean(editorErrors.fields[field])))
const selectedProtocolCapability = computed<ProtocolKernelCapability>(() => {
  const capability = protocolCapabilities[form.protocol] || { supported: false, reason: '无法确认当前内核是否支持该协议。' }
  if (form.protocol !== 'mieru' || !capability.supported) return capability
  const minimum = capability.minimum_zero_version || '0.0.15-rc.4'
  const installed = selectedNode.value?.kernel_state?.installed_version || ''
  if (!installed || compareZeroVersions(installed, minimum) < 0) {
    return { supported: false, minimum_zero_version: minimum, reason: defaultMieruUnavailableReason }
  }
  return capability
})
const canSaveSelectedProtocol = computed(() => selectedProtocolCapability.value.supported || (Boolean(form.id) && !form.is_active))
const mieruUnavailableReason = computed(() => protocolCapabilities.mieru?.supported ? '' : protocolCapabilities.mieru?.reason || defaultMieruUnavailableReason)

for (const field of Object.keys(protocolFieldMap)) {
  watch(() => form[field], () => editorErrors.clear(field))
}
for (const [source, field] of [
  [() => structured.username, 'structured.username'], [() => structured.password, 'structured.password'],
  [() => structured.cert_path, 'structured.cert_path'], [() => structured.key_path, 'structured.key_path'],
] as Array<[() => unknown, string]>) watch(source, () => editorErrors.clear(field))
const effectiveSortField = computed(() => groupedByNode.value ? 'node_id' : sortField.value)
const effectiveSortDirection = computed<'asc' | 'desc'>(() => groupedByNode.value ? 'asc' : sortDirection.value)
const protocolStatusOverview = computed(() => [
  { value: '' as ProtocolDeploymentStatus, label: '全部服务', caption: '当前筛选范围', icon: 'activity', tone: 'neutral', count: overviewCounts[''] },
  { value: 'succeeded' as ProtocolDeploymentStatus, label: '已生效', caption: '配置发布成功', icon: 'check', tone: 'success', count: overviewCounts.succeeded },
  { value: 'running' as ProtocolDeploymentStatus, label: '发布中', caption: '任务正在执行', icon: 'refresh', tone: 'warning', count: overviewCounts.running },
  { value: 'failed' as ProtocolDeploymentStatus, label: '发布失败', caption: '需要立即处理', icon: 'alert', tone: 'danger', count: overviewCounts.failed },
  { value: 'never' as ProtocolDeploymentStatus, label: '等待发布', caption: '尚无发布记录', icon: 'clock', tone: 'neutral', count: overviewCounts.never },
] as const)

function protocolLabel(protocol: string) { return ({ vmess: 'VMess', vless: 'VLESS', trojan: 'Trojan', shadowsocks: 'Shadowsocks', hysteria2: 'Hysteria 2', mieru: 'Mieru' } as Record<string, string>)[protocol] || protocol }
function compareZeroVersions(left: string, right: string) {
  const tokenize = (value: string) => value.replace(/^v/, '').split(/[.-]/).map(part => /^\d+$/.test(part) ? Number(part) : part)
  const a = tokenize(left), b = tokenize(right)
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const av = a[index] ?? Number.POSITIVE_INFINITY
    const bv = b[index] ?? Number.POSITIVE_INFINITY
    if (av === bv) continue
    if (typeof av === 'number' && typeof bv === 'number') return av < bv ? -1 : 1
    return String(av).localeCompare(String(bv), undefined, { numeric: true })
  }
  return 0
}
function formatMultiplier(value: number) { return `${Number(value || 1000) / 1000}×` }
function formatMultiplierNumber(value: number) { return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(Number(value || 1000) / 1000) }
function deploymentLabel(status?: string) { return !status ? '等待首次发布' : status === 'succeeded' ? '已生效' : status === 'failed' ? '发布失败' : status === 'running' ? '发布中' : formatUnknownValue('状态', status) }
function deploymentTone(status?: string): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : status === 'running' ? 'warning' : 'neutral' }
function deploymentIcon(status?: string) { return status === 'succeeded' ? 'check' : status === 'failed' ? 'alert' : status === 'running' ? 'refresh' : 'minus' }
function deploymentSummary(item: any) { return truncateOutput(normalizeOutput(item.error || item.output), 360) }
function isFirstNodeGroup(index: number) { return index === 0 || endpoints.value[index - 1]?.node_id !== endpoints.value[index]?.node_id }
function protocolBatchScope() { return selectionAllMatching.value ? { all_matching: true, filters: { q: filters.q || undefined, protocol: filters.protocol || undefined, active: filters.active ? filters.active === 'active' : undefined, deployment_status: filters.deployment || undefined } } : { protocol_endpoint_ids: selectedEndpointIDs.value } }

async function runProtocolBatch(action: 'deploy' | 'enable' | 'disable') {
  const label = action === 'deploy' ? '发布协议配置' : action === 'enable' ? '启用协议服务' : '停用协议服务'
  const accepted = await confirmAction({ title: `批量${label}`, message: `将对 ${selectedEndpointCount.value} 个协议服务创建后台任务。系统会按受影响节点去重，同一节点只发布一次完整配置并保留回滚；部分失败不会掩盖其他节点结果。`, confirmText: '创建任务', tone: action === 'disable' ? 'danger' : 'primary' })
  if (!accepted) return
  bulkBusy.value = action; error.value = ''; message.value = ''
  try {
    const scope = protocolBatchScope()
    const task = action === 'deploy' ? await createProtocolBatchDeployment(scope) : await updateProtocolEndpointsBatch({ ...scope, is_active: action === 'enable' })
    trackAdminTask(task); clearSelection(); message.value = `后台任务 #${task.id} 已接受；最终结果会显示在任务托盘。`
  } catch (e: any) { error.value = e?.response?.data?.message || '批量协议任务创建失败。' }
  finally { bulkBusy.value = '' }
}
function randomUUID() { if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID(); return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, char => { const value = Math.random() * 16 | 0; return (char === 'x' ? value : (value & 0x3) | 0x8).toString(16) }) }
function randomSecret(length = 24) { const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'; const values = new Uint8Array(length); if (globalThis.crypto?.getRandomValues) globalThis.crypto.getRandomValues(values); else for (let index = 0; index < length; index++) values[index] = Math.floor(Math.random() * 256); return Array.from(values, value => chars[value % chars.length]).join('') }
function parseObject(value: string) { try { const parsed = JSON.parse(value || '{}'); return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {} } catch { return {} } }
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }

async function loadProtocolOverview() {
  overviewLoading.value = true
  const statuses: ProtocolDeploymentStatus[] = ['', 'succeeded', 'running', 'failed', 'never']
  try {
    const results = await Promise.all(statuses.map(deploymentStatus => fetchProtocolEndpointsPage({
      limit: 1,
      q: filters.q || undefined,
      protocol: filters.protocol || undefined,
      active: filters.active ? filters.active === 'active' : undefined,
      deploymentStatus: deploymentStatus || undefined,
    })))
    results.forEach((page, index) => { overviewCounts[statuses[index]] = page.total })
  } catch {
    // The overview is secondary navigation; a failed count request must not hide the service table.
  } finally {
    overviewLoading.value = false
  }
}
async function loadProtocolCapabilities() {
  try {
    const version = await getVersion()
    for (const protocol of protocols) {
      const capability = version.protocol_capabilities?.[protocol]
      if (capability) protocolCapabilities[protocol] = capability
    }
  } catch {
    // Fail closed for Mieru. Other protocols retain their established panel
    // defaults so a transient version request does not disable the page.
  }
}
async function refresh() { await Promise.all([loadEndpoints(), loadProtocolOverview()]) }
async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = { query: {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(filters.q ? { q: filters.q } : {}),
    ...(filters.protocol ? { protocol: filters.protocol } : {}),
    ...(filters.active ? { active: filters.active } : {}),
    ...(filters.deployment ? { deployment: filters.deployment } : {}),
    ...(groupedByNode.value ? { view: 'nodes' } : {}),
    ...(sortField.value !== 'sort_order' ? { sort: sortField.value } : {}),
    ...(sortField.value !== 'sort_order' || sortDirection.value !== 'asc' ? { direction: sortDirection.value } : {}),
    ...(density.value !== 'compact' ? { density: density.value } : {}),
    ...(page > 1 ? { page: String(page) } : {}),
    ...(limit.value !== 50 ? { limit: String(limit.value) } : {}),
    ...(selectedEndpointDetail.value ? { endpoint: String(selectedEndpointDetail.value.id) } : {}),
    ...(selectedEndpointDetail.value && deploymentOffset.value > 0 ? { deployment_page: String(Math.floor(deploymentOffset.value / deploymentLimit.value) + 1) } : {}),
    ...(selectedEndpointDetail.value && deploymentLimit.value !== 25 ? { deployment_limit: String(deploymentLimit.value) } : {}),
  } }
  await (replace ? router.replace(location) : router.push(location))
}
async function applyFilters() { clearSelection(); offset.value = 0; await syncURL(); await refresh() }
async function resetFilters() { Object.assign(filters, { q: '', protocol: '', active: '', deployment: '' }); await applyFilters() }
async function selectDeploymentStatus(value: ProtocolDeploymentStatus) {
  if (filters.deployment === value) return
  filters.deployment = value
  await applyFilters()
}
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await refresh() }
async function setGroupedView(grouped: boolean) { groupedByNode.value = grouped; offset.value = 0; clearSelection(); await syncURL(); await refresh() }
async function setSort(field: string) { const next = resolveSortField(field, protocolSortFields, 'sort_order'); const currentField = groupedByNode.value ? 'node_id' : sortField.value; const currentDirection = groupedByNode.value ? 'asc' : sortDirection.value; sortDirection.value = nextSortDirection(currentField, next, currentDirection, next === 'name' || next === 'protocol' || next === 'node_id' ? 'asc' : 'desc'); sortField.value = next; groupedByNode.value = false; offset.value = 0; clearSelection(); await syncURL(); await refresh() }
async function setDensity(value: 'compact' | 'comfortable') { density.value = value; await syncURL(true) }
async function loadDetail(endpointID: number, summary?: ProtocolEndpointListItem | null, updateURL = false) {
  const requestID = endpointID
  detailLoadingID.value = requestID
  error.value = ''
  try {
    const detail = await fetchProtocolEndpoint(endpointID)
    if (detailLoadingID.value !== requestID) return
    selectedEndpointDetail.value = detail
    selectedEndpointSummary.value = summary || endpoints.value.find(item => item.id === endpointID) || null
    if (updateURL) await syncURL()
    await loadDeployments()
  } catch (cause: any) {
    if (detailLoadingID.value === requestID) error.value = cause?.response?.data?.message || '协议服务详情加载失败。'
  } finally {
    if (detailLoadingID.value === requestID) detailLoadingID.value = 0
  }
}
async function openDetail(endpoint: ProtocolEndpointListItem) { deploymentOffset.value = 0; deploymentLimit.value = 25; await loadDetail(endpoint.id, endpoint, true) }
async function closeDetail() { selectedEndpointDetail.value = null; selectedEndpointSummary.value = null; deploymentOffset.value = 0; await syncURL() }
async function editSelectedEndpoint() { const endpoint = selectedEndpointSummary.value; if (!endpoint) return; await closeDetail(); openEdit(endpoint) }
async function copySelectedEndpoint() { const endpoint = selectedEndpointSummary.value; if (!endpoint) return; await closeDetail(); await openCopy(endpoint) }
async function changeDeploymentPage(value: { offset: number; limit: number }) { deploymentOffset.value = value.offset; deploymentLimit.value = value.limit; await syncURL(); await loadDeployments() }
function handleNodeSelect(node: AdminNodeListItem | null) {
  const previousNodeID = selectedNode.value?.id || 0
  selectedNode.value = node
  if (previousNodeID && node?.id !== previousNodeID) { form.parent_protocol_id = 0; form.managed_certificate_id = 0 }
  void loadManagedCertificates(node?.id || 0)
  if (!form.id && !copySourceID.value && node?.address) form.address = node.address
  if (!form.id && !copySourceID.value) suggestName()
}
function useNodeAddress() { if (selectedNode.value?.address) form.address = selectedNode.value.address }
function suggestName() { if (!selectedNode.value) return; form.name = `${selectedNode.value.name} ${protocolLabel(form.protocol)}` }
function handleProtocolChange() { if (!form.id) suggestName(); structured.cipher = form.protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : 'aes-128-gcm'; structured.security = form.protocol === 'vless' ? 'none' : 'tls'; if (!['vless', 'vmess', 'trojan', 'hysteria2'].includes(form.protocol)) form.managed_certificate_id = 0 }
async function loadManagedCertificates(nodeID: number) {
  if (!nodeID) { managedCertificates.value = []; return }
  try { managedCertificates.value = (await fetchManagedCertificatesPage({ nodeId: nodeID, limit: 200 })).items }
  catch { managedCertificates.value = [] }
}
function closeEditor() { if (!saving.value) editorOpen.value = false }
async function openCreate() {
  copySourceID.value = 0
  originalNodeID.value = 0
  selectedNode.value = null
  Object.assign(form, emptyForm())
  Object.assign(structured, emptyStructured())
  editorStep.value = 1
  editorError.value = ''
  editorErrors.clear()
  try {
    const node = (await fetchNodesPage({ enabled: true, limit: 1, sort: 'name', direction: 'asc' })).items[0] || null
    selectedNode.value = node
    Object.assign(form, { node_id: node?.id || 0, address: node?.address || '' })
    await loadManagedCertificates(node?.id || 0)
    suggestName()
  } catch {
    editorError.value = '节点列表暂时无法加载；恢复连接后可在上方搜索并选择承载节点。'
  }
  editorState.markClean()
  editorOpen.value = true
}
async function openEdit(endpoint: any) {
  copySourceID.value = 0
  error.value = ''
  try { const [detail, nodePage] = await Promise.all([fetchProtocolEndpoint(endpoint.id), fetchNodesPage({ nodeId: endpoint.node_id, limit: 1 })]); selectedNode.value = nodePage.items[0] || null; originalNodeID.value = detail.node_id; Object.assign(form, emptyForm(), detail, { parent_protocol_id: detail.parent_protocol_id || 0, managed_certificate_id: detail.managed_certificate_id || 0, config: detail.config || '{}', client_config: detail.client_config || '{}', optional_config: detail.optional_config || '{}', tags: detail.tags || '[]' }); await loadManagedCertificates(detail.node_id); readStructuredConfig(); editorState.markClean(); editorStep.value = 1; editorError.value = ''; editorErrors.clear(); editorOpen.value = true }
  catch (e: any) { error.value = e?.response?.data?.message || '协议详情加载失败。' }
}
async function openCopy(endpoint: ProtocolEndpointListItem) {
  if (!endpoint.kernel_supported) {
    error.value = endpoint.kernel_unsupported_reason || '当前内核不支持复制此协议。'
    return
  }
  copySourceID.value = endpoint.id
  originalNodeID.value = 0
  error.value = ''
  try {
    const [detail, nodePage] = await Promise.all([
      fetchProtocolEndpoint(endpoint.id),
      fetchNodesPage({ nodeId: endpoint.node_id, limit: 1 }),
    ])
    selectedNode.value = nodePage.items[0] || null
    await loadManagedCertificates(detail.node_id)
    Object.assign(form, emptyForm(), detail, {
      id: 0,
      name: `${detail.name} 副本`,
      is_active: false,
      parent_protocol_id: detail.parent_protocol_id || 0,
      config: detail.config || '{}',
      client_config: detail.client_config || '{}',
      optional_config: detail.optional_config || '{}',
      tags: detail.tags || '[]',
    })
    readStructuredConfig()
    editorStep.value = 1
    editorError.value = ''
    editorErrors.clear()
    editorState.markClean()
    editorOpen.value = true
  } catch (e: any) {
    copySourceID.value = 0
    error.value = e?.response?.data?.message || '协议配置复制失败。'
  }
}
function readStructuredConfig() {
  const server: any = parseObject(form.config), client: any = parseObject(form.client_config)
  structured.credential = server.users?.[0]?.id || client.id || randomUUID()
  structured.username = server.users?.[0]?.username || client.username || 'subscriber'
  structured.password = server.password || server.users?.[0]?.password || client.password || randomSecret()
  structured.cipher = server.cipher || server.users?.[0]?.cipher || client.cipher || (form.protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : 'aes-128-gcm')
  structured.security = server.tls ? 'tls' : 'none'
  structured.cert_path = server.tls?.cert_path || server.cert_path || ''
  structured.key_path = server.tls?.key_path || server.key_path || ''
  structured.server_name = client.tls?.server_name || client.sni || server.sni || form.address || ''
}
function buildGeneratedConfigs() {
  const existingServer: any = parseObject(form.config), existingClient: any = parseObject(form.client_config)
  const server: any = String(existingServer.type || '').toLowerCase() === form.protocol ? { ...existingServer, type: form.protocol } : { type: form.protocol }
  const client: any = String(existingClient.type || '').toLowerCase() === form.protocol ? { ...existingClient, type: form.protocol } : { type: form.protocol }
  client.server = form.address; client.port = Number(form.public_port)
  if (form.protocol === 'vless') { server.users = [{ ...(server.users?.[0] || {}), id: structured.credential }]; client.id = structured.credential; if (structured.security === 'tls') { server.tls = { ...(server.tls || {}), cert_path: structured.cert_path, key_path: structured.key_path }; client.tls = { ...(client.tls || {}), server_name: structured.server_name || form.address } } else { delete server.tls; delete client.tls } }
  else if (form.protocol === 'vmess') { server.users = [{ ...(server.users?.[0] || {}), id: structured.credential, cipher: structured.cipher }]; server.tls = { ...(server.tls || {}), cert_path: structured.cert_path, key_path: structured.key_path }; Object.assign(client, { id: structured.credential, cipher: structured.cipher, tls: { ...(client.tls || {}), server_name: structured.server_name || form.address } }) }
  else if (form.protocol === 'trojan') { Object.assign(server, { password: structured.password, sni: structured.server_name || form.address, tls: { ...(server.tls || {}), cert_path: structured.cert_path, key_path: structured.key_path } }); Object.assign(client, { password: structured.password, sni: structured.server_name || form.address }) }
  else if (form.protocol === 'shadowsocks') { Object.assign(server, { password: structured.password, cipher: structured.cipher }); Object.assign(client, { password: structured.password, cipher: structured.cipher }) }
  else if (form.protocol === 'hysteria2') { Object.assign(server, { password: structured.password }); if (structured.cert_path) server.cert_path = structured.cert_path; else delete server.cert_path; if (structured.key_path) server.key_path = structured.key_path; else delete server.key_path; Object.assign(client, { password: structured.password, insecure: false }) }
  else if (form.protocol === 'mieru') { server.users = []; delete client.username; delete client.password }
  form.config = JSON.stringify(server, null, 2); form.client_config = JSON.stringify(client, null, 2)
}
async function validateStep(step: number) {
  editorError.value = ''
  editorErrors.clear()
  const fields: Record<string, string> = {}
  if (step === 1) {
    if (!form.node_id) fields.node_id = '请选择承载 VPS。'
    if (!form.name.trim()) fields.name = '请输入服务名称。'
    if (!form.address.trim()) fields.address = '请输入客户端可访问的对外地址。'
    if (!isIntegerInRange(form.port, 1, 65535)) fields.port = '监听端口必须为 1–65535 之间的整数。'
    if (!isIntegerInRange(form.public_port, 1, 65535)) fields.public_port = '客户端连接端口必须为 1–65535 之间的整数。'
  }
  if (step === 2) {
    if (!usesManagedCredentials.value && form.protocol !== 'mieru' && !structured.password) fields['structured.password'] = '请输入连接密码。'
    if (!form.managed_certificate_id && requiresTLSFiles.value && !structured.cert_path.trim()) fields['structured.cert_path'] = '请选择托管证书或输入证书文件路径。'
    if (!form.managed_certificate_id && requiresTLSFiles.value && !structured.key_path.trim()) fields['structured.key_path'] = '请选择托管证书或输入私钥文件路径。'
    if (!form.managed_certificate_id && form.protocol === 'hysteria2' && Boolean(structured.cert_path.trim()) !== Boolean(structured.key_path.trim())) {
      fields['structured.cert_path'] = '证书和私钥路径需要同时填写，或同时留空。'
      fields['structured.key_path'] = '证书和私钥路径需要同时填写，或同时留空。'
    }
  }
  editorStep.value = step
  return editorErrors.applyValidation(fields, protocolFormElement, '请修正标出的字段后继续。')
}
async function nextStep() { if (!await validateStep(editorStep.value)) return; if (editorStep.value === 2) buildGeneratedConfigs(); editorStep.value++ }
function goToStep(step: number) { if (step === editorStep.value) return; if (!form.id && step > editorStep.value) return; if (step === 3) buildGeneratedConfigs(); editorError.value = ''; editorStep.value = step }
function validateJSONFields() {
  const fields: Record<string, string> = {}
  try { const server = JSON.parse(form.config); if (!server || Array.isArray(server) || typeof server !== 'object') fields.config = '服务端配置必须是 JSON 对象。'; else if (String(server.type || '').toLowerCase() !== form.protocol) fields.config = '服务端配置的 type 必须与协议一致。' } catch { fields.config = '服务端配置必须是有效 JSON 对象。' }
  for (const [field, label, value, array] of [['client_config', '客户端配置', form.client_config, false], ['optional_config', '可选配置', form.optional_config || '{}', false], ['tags', '标签', form.tags || '[]', true]] as const) {
    try { const parsed = JSON.parse(value); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) !== array) fields[field] = `${label} JSON 格式不正确。` } catch { fields[field] = `${label}必须是有效 JSON。` }
  }
  return fields
}
async function save() {
  if (!canSaveSelectedProtocol.value) {
    editorError.value = selectedProtocolCapability.value.reason || '当前内核不支持所选协议。'
    return
  }
  if (!await validateStep(1) || !await validateStep(2)) return
  const jsonFields = validateJSONFields()
  if (Object.keys(jsonFields).length) { editorStep.value = 3; await editorErrors.applyValidation(jsonFields, protocolFormElement, '高级配置格式不正确。'); return }
  if (form.id && originalNodeID.value && form.node_id !== originalNodeID.value) {
    const accepted = await confirmAction({
      title: '切换协议服务的承载节点？',
      message: '现有订阅凭证会随服务迁移；系统将重新分配目标节点上的专用端口，并分别发布原节点和目标节点的完整配置。',
      confirmText: '确认切换并保存',
      tone: 'danger',
    })
    if (!accepted) return
  }
  saving.value = true; editorError.value = ''; editorErrors.clear(); error.value = ''; message.value = ''
  try {
    const creatingCopy = Boolean(copySourceID.value)
    const payload = { node_id: form.node_id, name: form.name, protocol: form.protocol, address: form.address, port: form.port, public_port: form.public_port, multiplier_milli: form.multiplier_milli, sort_order: form.sort_order, parent_protocol_id: form.parent_protocol_id || null, managed_certificate_id: form.managed_certificate_id || null, is_active: Boolean(form.is_active), config: form.config, client_config: form.client_config, optional_config: form.optional_config || '{}', tags: form.tags || '[]' }
    if (form.id) await updateProtocolEndpoint(form.id, payload); else await createProtocolEndpoint(payload)
    editorOpen.value = false
    copySourceID.value = 0
    message.value = creatingCopy ? '协议配置已复制为独立服务；可按需启用并发布。' : '协议服务已保存，完整配置正在自动发布；可在列表中查看结果。'
    await refresh()
  }
  catch (e: any) {
    if (e?.response?.data) {
      const normalized = await editorErrors.applyApiError(e, '协议服务保存失败，请检查表单内容。', null, protocolFieldMap)
      const fields = Object.keys(normalized.fields)
      if (fields.some(field => ['node_id', 'protocol', 'name', 'address', 'port', 'public_port'].includes(field))) editorStep.value = 1
      else editorStep.value = 3
      await editorErrors.focusFirst(protocolFormElement)
    } else editorError.value = e?.message || '协议服务保存失败。'
  }
  finally { saving.value = false }
}
async function deploy(endpoint: ProtocolEndpointListItem) {
  if (!endpoint.kernel_supported) {
    error.value = endpoint.kernel_unsupported_reason || '当前内核不支持发布此协议。'
    return
  }
  deployingID.value = endpoint.id; error.value = ''; message.value = ''
  try { const result = await deployProtocolEndpoint(endpoint.id); message.value = `${endpoint.name} 的完整配置已在 ${endpoint.node_name || `VPS #${endpoint.node_id}`} 生效，耗时 ${result.latency_ms || 0}ms，并已通过 Zero 状态与心跳检查。`; await refresh() }
  catch (e: any) { error.value = e?.response?.data?.message || '配置发布失败，节点已尝试回滚；请检查发布记录、SSH 和 Zero 状态。'; await refresh() }
  finally { deployingID.value = 0 }
}
async function removeEndpoint(endpoint: ProtocolEndpointListItem) {
  const accepted = await confirmAction({
    title: '删除协议服务？',
    message: endpoint.is_active
      ? `系统会先从 ${endpoint.node_name || `VPS #${endpoint.node_id}`} 的完整 Zero 配置中移除“${endpoint.name}”，验证生效后再删除面板记录。`
      : `将永久删除“${endpoint.name}”的服务记录并吊销其凭证；历史发布和流量记录会保留用于审计。`,
    confirmText: '确认删除',
    tone: 'danger',
  })
  if (!accepted) return
  deletingID.value = endpoint.id
  error.value = ''
  message.value = ''
  try {
    await deleteProtocolEndpoint(endpoint.id)
    if (selectedEndpointDetail.value?.id === endpoint.id) await closeDetail()
    message.value = `协议服务“${endpoint.name}”已删除。`
    await refresh()
  } catch (e: any) {
    error.value = e?.response?.data?.message || '协议服务删除失败。'
  } finally {
    deletingID.value = 0
  }
}
watch(() => route.fullPath, async () => {
  const nextLimit = Number(route.query.limit)
  const resolvedLimit = allowedPageSizes.includes(nextLimit) ? nextLimit : 50
  const resolvedOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * resolvedLimit
  const nextFilters = {
    q: typeof route.query.q === 'string' ? route.query.q : '',
    protocol: typeof route.query.protocol === 'string' ? route.query.protocol : '',
    active: typeof route.query.active === 'string' ? route.query.active : '',
    deployment: typeof route.query.deployment === 'string' ? route.query.deployment : '',
  }
  const nextGroupedByNode = route.query.view === 'nodes'
  const nextSortField = resolveSortField(route.query.sort, protocolSortFields, 'sort_order')
  const nextSortDirection = resolveSortDirection(route.query.direction, 'asc')
  const nextDensity = resolveTableDensity(route.query.density)
  const filterChanged = Object.keys(nextFilters).some(key => nextFilters[key as keyof typeof nextFilters] !== filters[key as keyof typeof filters])
  const sortChanged = nextSortField !== sortField.value || nextSortDirection !== sortDirection.value
  density.value = nextDensity
  if (resolvedLimit !== limit.value || resolvedOffset !== offset.value || nextGroupedByNode !== groupedByNode.value || filterChanged || sortChanged) {
    if (filterChanged || sortChanged || nextGroupedByNode !== groupedByNode.value) clearSelection()
    limit.value = resolvedLimit; offset.value = resolvedOffset; groupedByNode.value = nextGroupedByNode; sortField.value = nextSortField; sortDirection.value = nextSortDirection; Object.assign(filters, nextFilters); await refresh()
  }
  const nextDeploymentLimitValue = Number(route.query.deployment_limit)
  const nextDeploymentLimit = allowedPageSizes.includes(nextDeploymentLimitValue) ? nextDeploymentLimitValue : 25
  const nextDeploymentOffset = (Math.max(1, Number(route.query.deployment_page) || 1) - 1) * nextDeploymentLimit
  const deploymentPageChanged = nextDeploymentLimit !== deploymentLimit.value || nextDeploymentOffset !== deploymentOffset.value
  deploymentLimit.value = nextDeploymentLimit
  deploymentOffset.value = nextDeploymentOffset
  const endpointID = Number(route.query.endpoint) || 0
  if (!endpointID) { selectedEndpointDetail.value = null; selectedEndpointSummary.value = null }
  else if (selectedEndpointDetail.value?.id !== endpointID) await loadDetail(endpointID)
  else if (deploymentPageChanged) await loadDeployments()
})
onMounted(async () => {
  await Promise.all([refresh(), loadProtocolCapabilities()])
  const endpointID = Number(route.query.endpoint) || 0
  if (endpointID) await loadDetail(endpointID)
})
</script>

<style scoped>
.protocol-status-overview{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:8px}
:deep(.protocol-table th),:deep(.protocol-table td){height:40px;padding-block:7px}:deep(.protocol-table .mono){font-size:11px}:deep(.protocol-table a){color:var(--primary);font-weight:650;text-decoration:none}:deep(.protocol-table a:hover){text-decoration:underline}.numeric-column{text-align:right!important;font-variant-numeric:tabular-nums}:deep(.protocol-table .cell-actions .button){min-height:30px}:deep(.protocol-table .time-badge){margin-left:auto}
.protocol-group-row td{height:34px!important;padding:7px 12px!important;color:var(--text);background:var(--surface-soft)!important}.protocol-group-content{display:flex;align-items:center;gap:7px;min-width:0}.protocol-group-row .ui-icon{flex:0 0 auto;color:var(--primary)}.protocol-group-row strong{min-width:0;overflow:hidden;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.protocol-group-row span{flex:0 0 auto;color:var(--muted);font-size:9px}
.protocol-metrics{margin-bottom:16px}.protocol-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.protocol-card{display:grid;overflow:hidden}.protocol-header{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;padding:18px}.protocol-title{display:flex;gap:12px;min-width:0}.protocol-icon{width:40px;height:40px;display:grid;place-items:center;flex:0 0 auto;border-radius:10px;color:var(--primary);background:var(--primary-soft);font-size:19px}.title-line{display:flex;align-items:center;flex-wrap:wrap;gap:8px}.title-line h2{margin:0;font-size:16px}.protocol-title p{margin:5px 0 0;color:var(--muted);font-family:var(--font-mono);font-size:11px;overflow-wrap:anywhere}.multiplier{color:var(--primary);font-size:18px}.protocol-meta{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));margin:0;padding:14px 18px;border-block:1px solid var(--line);background:var(--surface-soft)}.protocol-meta div{display:grid;gap:4px}.protocol-meta dt{color:var(--muted);font-size:10px}.protocol-meta dd{margin:0;font-size:11px;font-weight:650}.usage-summary{display:flex;flex-wrap:wrap;gap:14px;padding:10px 18px;color:var(--muted);border-bottom:1px solid var(--line);font-size:9px}.deployment-error{margin:12px 18px 0;padding:9px;border-radius:8px;color:var(--danger);background:var(--danger-soft);font-size:11px;overflow-wrap:anywhere}.protocol-actions{display:flex;justify-content:flex-end;gap:8px;padding:14px 18px}
.protocol-editor{display:grid;gap:20px}
.wizard-panel{display:grid;gap:20px}
.wizard-heading{display:flex;align-items:flex-start;gap:12px;padding-bottom:16px;border-bottom:1px solid var(--line)}
.wizard-heading>span{width:38px;height:38px;display:grid;place-items:center;flex:0 0 auto;border-radius:10px;color:var(--primary);background:var(--primary-soft);font-size:18px}
.wizard-heading h3{margin:1px 0 4px;font-size:15px}
.wizard-heading p{margin:0;color:var(--muted);font-size:10px;line-height:1.6}
.guided-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}
.selected-node-card{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:12px;padding:13px 14px;border:1px solid var(--primary-border);border-radius:10px;background:var(--surface-selected)}
.selected-node-card strong{font-size:12px}
.selected-node-card p{margin:3px 0 0;color:var(--muted);font-size:10px}
.input-with-action{display:grid;grid-template-columns:minmax(0,1fr) auto}
.input-with-action input{border-radius:8px 0 0 8px!important}
.input-with-action button{padding:0 12px;border:1px solid var(--line-strong);border-left:0;border-radius:0 8px 8px 0;color:var(--primary);background:var(--surface-soft);font-size:10px;font-weight:700;white-space:nowrap;cursor:pointer}
.input-with-action button:hover{background:var(--primary-soft)}
.generated-config-note{display:flex;align-items:flex-start;gap:10px;padding:13px;border:1px solid var(--success-border);border-radius:10px;color:var(--success);background:var(--success-soft)}
.generated-config-note>.ui-icon{margin-top:1px}
.generated-config-note strong{font-size:11px}
.generated-config-note p{margin:3px 0 0;font-size:9px;line-height:1.6}
.review-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
.review-grid article{display:grid;gap:4px;padding:14px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}
.review-grid span,.review-grid small{color:var(--muted);font-size:9px}
.review-grid strong{font-size:12px}
.advanced-settings{border:1px solid var(--line);border-radius:10px;overflow:hidden}
.advanced-settings summary{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:13px 14px;cursor:pointer;list-style:none}
.advanced-settings summary::-webkit-details-marker{display:none}
.advanced-settings summary>span{width:30px;height:30px;display:grid;place-items:center;border-radius:8px;color:var(--primary);background:var(--primary-soft)}
.advanced-settings summary strong,.advanced-settings summary small{display:block}
.advanced-settings summary strong{font-size:11px}
.advanced-settings summary small{margin-top:2px;color:var(--muted);font-size:9px}
.advanced-settings[open] summary>.ui-icon{transform:rotate(90deg)}
.advanced-body{display:grid;gap:16px;padding:16px;border-top:1px solid var(--line);background:var(--surface-soft)}
.compact-grid{max-width:520px}
.config-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.config-grid textarea{font-family:var(--font-mono);font-size:10px}
@media(max-width:1080px){.protocol-status-overview{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:900px){.protocol-grid,.guided-grid,.config-grid{grid-template-columns:1fr}.protocol-meta,.review-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.protocol-meta{row-gap:12px}}@media(max-width:680px){.protocol-status-overview{display:flex;overflow-x:auto;padding:1px 1px 6px;scroll-snap-type:x proximity}.protocol-status-overview :deep(.overview-card){min-width:178px;scroll-snap-align:start}.protocol-grid{gap:8px}.protocol-card{border-inline:0;border-radius:0;box-shadow:none}.protocol-meta{background:transparent}.review-grid{grid-template-columns:1fr}.selected-node-card{grid-template-columns:auto minmax(0,1fr)}.selected-node-card .status-badge{grid-column:2}.input-with-action{grid-template-columns:1fr}.input-with-action input{border-radius:8px!important}.input-with-action button{min-height:36px;border:1px solid var(--line-strong);border-top:0;border-radius:0 0 8px 8px}}.protocol-mobile-detail-actions{display:none}@media(max-width:560px){:deep(.page-actions .p-button:last-child){flex:1}:deep(.protocol-table .table-primary-column){min-width:200px}.protocol-mobile-detail-actions{display:flex;flex-wrap:wrap;gap:7px}}
.protocol-editor .p-select{min-height:var(--control-height)}
.protocol-grid{grid-template-columns:1fr;gap:0;border-block:1px solid var(--line)}.protocol-card{border:0;border-radius:0}.protocol-card+.protocol-card{border-top:1px solid var(--line)}
.selected-node-card{border-color:var(--primary-border);background:var(--primary-soft)}
.protocol-detail{min-width:0}.detail-status-strip{display:flex;flex-wrap:wrap;gap:8px}.detail-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));overflow:hidden}.detail-facts>div{display:grid;gap:5px;padding:14px 16px;border-bottom:1px solid var(--line)}.detail-facts>div:nth-child(odd){border-right:1px solid var(--line)}.detail-facts span{color:var(--muted);font-size:10px}.detail-facts strong,.detail-facts a{font-size:12px}.detail-facts a{color:var(--primary);font-weight:650;text-decoration:none}.numeric-summary{color:var(--primary);font-size:14px;font-weight:750;font-variant-numeric:tabular-nums}.deployment-history{overflow:hidden}.deployment-history>.page-alert{margin:12px}.deployment-history :deep(.data-table-shell){border:0;border-radius:0}.deployment-output{max-width:360px;margin:0;color:var(--muted);font-family:var(--font-mono);font-size:10px;line-height:1.5;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:560px){.detail-facts{grid-template-columns:1fr}.detail-facts>div:nth-child(odd){border-right:0}}
</style>
