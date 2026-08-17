<template>
  <section class="standard-page">
    <PageHeader title="节点资产" description="独立管理 VPS、运维通道、Zero 连接与计量凭证；协议服务在单独页面绑定节点。" eyebrow="Infrastructure">
      <template #actions>
        <PageRefreshButton label="刷新节点资产" :loading="loading" @click="refresh" />
        <UiButton  type="button" @click="openCreate"><UiIcon name="plus" />登记 VPS</UiButton>
      </template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="节点操作已完成" error-title="节点操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing" :density="density" show-density @update:density="setDensity">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(filters.q || filters.lifecycle || filters.connector)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="filters.q" label="搜索" placeholder="名称、区域或地址" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="filters.lifecycle" label="生命周期" :options="lifecycleFilterOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="filters.connector" label="Zero 连接" :options="connectorFilterOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #selection>
        <div v-if="selectedNodeIDs.length || selectionAllMatching" class="bulk-action-bar">
          <div><strong>已选择 {{ selectedNodeCount }} 个节点</strong><span v-if="selectionAllMatching">范围：当前全部筛选结果</span><span v-else>范围：已勾选行</span><UiButton v-if="canSelectAllMatching" variant="ghost" size="sm" type="button" @click="selectAllMatching">选择全部 {{ total }} 条筛选结果</UiButton></div>
          <div><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'detect'" @click="runBatch('detect')"><UiIcon name="search" />批量检测</UiButton><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'reconcile'" @click="openBatchKernelRollout"><UiIcon name="play" />批量升级 Zero</UiButton><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'maintenance'" @click="runBatch('maintenance')">设为维护</UiButton><UiButton variant="secondary" size="sm" type="button" :loading="bulkBusy === 'activate'" @click="runBatch('activate')">恢复启用</UiButton><UiButton variant="danger" size="sm" type="button" :loading="bulkBusy === 'retire'" @click="runBatch('retire')">批量退役</UiButton><UiButton variant="ghost" size="sm" type="button" @click="clearSelection">清除</UiButton></div>
        </div>
      </template>
      <DataTable v-if="nodes.length" caption="节点资产列表；可按节点、区域和最近心跳排序，选择“查看”打开节点详情" :row-count="total" :density="density" :min-width="900" selectable table-class="workbench-table">
          <thead><tr>
            <th class="selection-column"><UiCheckbox :model-value="allPageNodesSelected" :indeterminate="pageNodeSelectionIndeterminate" :disabled="selectionAllMatching" aria-label="选择当前页全部节点" @update:model-value="toggleCurrentNodePage" /></th>
            <SortableHeader field="name" label="节点" :sort-field="sortField" :direction="sortDirection" pinned="start" @sort="setSort" />
            <SortableHeader field="region" label="区域" :sort-field="sortField" :direction="sortDirection" :priority="3" @sort="setSort" />
            <th data-column-priority="2">资产状态</th><th>Zero</th><th data-column-priority="3">SSH</th><th data-column-priority="2">内核</th><th class="numeric-column" data-column-priority="3">协议数</th>
            <SortableHeader field="last_seen_at" label="最近心跳" :sort-field="sortField" :direction="sortDirection" :priority="3" @sort="setSort" />
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr></thead>
          <tbody>
            <tr v-for="node in nodes" :key="node.id" :class="{ selected: selectedNode?.id === node.id, 'batch-selected': isNodeSelected(node.id) }">
              <td class="selection-column"><UiCheckbox :model-value="isNodeSelected(node.id)" :disabled="selectionAllMatching" :aria-label="`选择节点 ${node.name}`" @update:model-value="toggleNodeSelection(node.id, $event)" /></td>
              <td class="table-primary-column"><div class="cell-title"><strong>{{ node.name }}</strong><span class="mono">{{ node.address || '未设置地址' }}</span></div></td>
              <td data-column-priority="3">{{ node.region || '—' }}</td>
              <td data-column-priority="2"><StatusBadge :tone="lifecycleTone(node)" :icon="node.lifecycle_status === 'maintenance' ? 'settings' : undefined">{{ lifecycleLabel(node.lifecycle_status) }}</StatusBadge></td>
              <td><StatusBadge :tone="node.connector_online ? 'success' : 'warning'" :icon="node.connector_online ? 'wifi' : 'alert'">{{ node.connector_online ? '在线' : '离线' }}</StatusBadge></td>
              <td data-column-priority="3"><StatusBadge :tone="node.ssh_verified_at ? 'success' : node.ssh_configured ? 'warning' : 'neutral'" icon="key">{{ node.ssh_verified_at ? '已验证' : node.ssh_configured ? '待验证' : '未配置' }}</StatusBadge></td>
              <td data-column-priority="2"><StatusBadge :tone="kernelListTone(node.kernel_state?.status)" icon="activity">{{ kernelListLabel(node.kernel_state?.status) }}</StatusBadge></td>
              <td class="numeric-column" data-column-priority="3">{{ formatNumber(node.enabled_protocol_count) }}</td>
              <td data-column-priority="3"><TimeBadge :value="node.connector_last_seen_at" /></td>
              <td class="table-action-column"><UiButton variant="ghost" size="sm" type="button" :loading="detailLoadingID === node.id" :aria-label="`查看节点 ${node.name}`" @click="selectNode(node)">查看<UiIcon name="chevron" /></UiButton></td>
            </tr>
          </tbody>
      </DataTable>
      <EmptyState v-else-if="!initialLoading" icon="nodes" :title="filters.q || filters.lifecycle || filters.connector ? '没有匹配节点' : '还没有 VPS'" :description="filters.q || filters.lifecycle || filters.connector ? '调整或清除筛选条件后重试。' : '先登记主机资产，协议和套餐可以稍后配置。'" />
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(selectedNode)" :title="selectedNode?.name || '节点详情'" eyebrow="Node asset" :description="selectedNode ? `${selectedNode.region || '未设置区域'} · ${selectedNode.address || '未设置地址'}` : ''" @close="closeDetail">
      <main v-if="selectedNode" class="stack node-detail">
        <TransientFeedback :success="detailMessage" :error="detailError" success-title="节点操作已完成" error-title="节点操作失败" />
        <article class="panel node-summary">
          <div class="node-summary-main">
            <span class="node-avatar"><UiIcon name="nodes" /></span>
            <div><div class="title-line"><h2>{{ selectedNode.name }}</h2><StatusBadge :tone="lifecycleTone(selectedNode)">{{ lifecycleLabel(selectedNode.lifecycle_status) }}</StatusBadge></div><p>{{ selectedNode.region || '未设置区域' }} · <span class="mono">{{ selectedNode.address || '未设置默认业务地址' }}</span></p><small>{{ selectedNode.remark || '暂无备注' }}</small></div>
          </div>
          <div class="node-actions">
            <UiButton variant="secondary" size="sm" type="button" @click="openEdit(selectedNode)"><UiIcon name="edit" />编辑资产</UiButton>
            <UiButton variant="secondary" size="sm" type="button" @click="openSSH(selectedNode)"><UiIcon name="key" />SSH 连接</UiButton>
            <UiButton variant="secondary" size="sm" type="button" :disabled="testingNode === selectedNode.id" @click="testSSH(selectedNode.id)"><UiIcon name="activity" />{{ testingNode === selectedNode.id ? '验证中…' : '验证 SSH' }}</UiButton>
            <UiButton variant="secondary" size="sm" type="button" :disabled="!selectedNode.ssh_host || !selectedNode.ssh_user" @click="openTerminal(selectedNode)"><UiIcon name="terminal" />打开终端</UiButton>
            <UiButton variant="secondary" size="sm" type="button" :disabled="!selectedNode.ssh_verified_at" @click="diagnosticsOpen = true"><UiIcon name="search" />运行诊断</UiButton>
            <UiButton v-if="selectedNode.ssh_host_key_fingerprint" variant="danger" size="sm" type="button" @click="resetSSHHostKey(selectedNode)">重新信任主机</UiButton>
            <UiButton variant="danger" size="sm" type="button" :loading="deletingNode" @click="removeNode(selectedNode)"><UiIcon name="trash" />删除节点</UiButton>
          </div>
        </article>

        <UiTabs v-model="detailSection" :items="nodeDetailTabs" label="节点详情分区" />

        <section v-if="detailSection === 'overview'" class="node-health-strip" aria-label="节点状态概览">
          <StatusOverviewItem icon="activity" label="Zero 连接" :description="selectedNode.connector_last_seen_at ? '最近心跳' : '尚未连接'" :tone="selectedNode.connector_online ? 'success' : 'warning'" :status="selectedNode.connector_online ? '在线' : '离线'">
            <template #description><template v-if="selectedNode.connector_last_seen_at">最近心跳 <TimeBadge :value="selectedNode.connector_last_seen_at" mode="relative" /></template><template v-else>尚未连接</template></template>
          </StatusOverviewItem>
          <StatusOverviewItem icon="key" label="SSH 通道" :description="sshDescription(selectedNode)" :tone="selectedNode.ssh_verified_at ? 'success' : 'warning'" :status="selectedNode.ssh_verified_at ? '已验证' : '待验证'" />
          <StatusOverviewItem icon="shield" label="可信计量" :description="selectedNode.traffic_secret_prefix ? `凭证 ${selectedNode.traffic_secret_prefix}…` : '尚未创建凭证'" :tone="selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at ? 'success' : 'warning'" :status="selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at ? '已配置' : '未配置'" />
          <StatusOverviewItem icon="nodes" label="资产状态" :description="selectedNode.is_enabled ? '允许承载服务' : '已停止交付'" :tone="selectedNode.is_enabled ? 'success' : 'neutral'" :status="selectedNode.is_enabled ? '启用' : '停用'" />
          <StatusOverviewItem icon="activity" label="Zero 内核" :description="kernelDescription" :tone="kernelTone" :status="kernelStatusLabel" />
          <article class="node-load-summary">
            <header><div><strong>主机资源</strong><p>仅在点击“刷新资源”时通过已验证的 SSH 通道读取；页面不会自动采样。</p></div><UiButton variant="secondary" size="sm" type="button" :loading="nodeLoadLoading" :disabled="!selectedNode.ssh_host || !selectedNode.ssh_user" @click="loadNodeLoad(selectedNode.id)"><UiIcon name="refresh" />刷新资源</UiButton></header>
            <PageAlert v-if="nodeLoadError" tone="warning" title="暂时无法读取负载">{{ nodeLoadError }}</PageAlert>
            <div v-if="nodeLoad" class="node-load-grid">
              <div><span>系统负载（1 / 5 / 15 分钟）</span><strong>{{ nodeLoad.load_average_1.toFixed(2) }} / {{ nodeLoad.load_average_5.toFixed(2) }} / {{ nodeLoad.load_average_15.toFixed(2) }}</strong><small>{{ formatNumber(nodeLoad.cpu_core_count) }} 核 · 1 分钟负载率 {{ formatLoadRatio(nodeLoad) }}</small></div>
              <div><span>内存</span><strong>{{ formatBytes(nodeLoad.memory_total_bytes - nodeLoad.memory_available_bytes) }} / {{ formatBytes(nodeLoad.memory_total_bytes) }}</strong><small>可用 {{ formatBytes(nodeLoad.memory_available_bytes) }}</small></div>
              <div><span>根文件系统</span><strong>{{ formatBytes(nodeLoad.root_total_bytes - nodeLoad.root_available_bytes) }} / {{ formatBytes(nodeLoad.root_total_bytes) }}</strong><small>可用 {{ formatBytes(nodeLoad.root_available_bytes) }}</small></div>
              <div><span>主机运行时间</span><strong>{{ formatUptime(nodeLoad.uptime_seconds) }}</strong><small>采样 <TimeBadge :value="nodeLoad.sampled_at" mode="relative" /> · SSH {{ nodeLoad.latency_ms }} ms</small></div>
            </div>
          </article>
        </section>

        <article v-else-if="detailSection === 'kernel'" class="panel kernel-panel">
          <header class="panel-header">
            <div><h2>Zero 内核自动化</h2><p>检测真实平台与服务状态，锁定当前部署契约指定的受信任制品后执行安装、升级或配置对齐；任一验收失败都会恢复上一版。</p></div>
            <StatusBadge :tone="kernelTone">{{ kernelStatusLabel }}</StatusBadge>
          </header>
          <div class="panel-body kernel-body">
            <div class="kernel-facts">
              <div><span>已安装版本</span><strong>{{ kernelState?.installed_version || '未检测' }}</strong></div>
              <div><span>最新发布版</span><strong>{{ latestPublishedRelease?.tag || (releaseLoading ? '查询中…' : '未查询') }}</strong></div>
              <div><span>本次目标版本</span><strong>{{ selectedRelease?.tag || '尚未选择' }}</strong></div>
              <div><span>平台</span><strong>{{ [kernelState?.platform_os, kernelState?.architecture].filter(Boolean).join(' · ') || '未检测' }}</strong></div>
              <div><span>运行库</span><strong>{{ kernelState?.libc || '未检测' }}</strong></div>
              <div><span>systemd / 进程</span><StatusBadge :tone="serviceTone(kernelState?.service_status)" :icon="kernelState?.service_status === 'active' ? 'check' : 'alert'">{{ serviceLabel(kernelState?.service_status) }}</StatusBadge></div>
              <div><span>控制通道</span><StatusBadge :tone="controlTone(kernelState?.control_status)" :icon="kernelState?.control_status === 'healthy' ? 'check' : 'alert'">{{ controlLabel(kernelState?.control_status) }}</StatusBadge></div>
              <div><span>最后检测</span><strong><TimeBadge v-if="kernelState?.last_detected_at" :value="kernelState.last_detected_at" /><template v-else>从未</template></strong></div>
              <div><span>建议动作</span><strong>{{ kernelActionLabel(kernelState?.recommended_action) }}</strong></div>
            </div>
            <PageAlert tone="info" :title="selectedNode.node_credential_prefix ? 'Zero 连接凭证已就绪' : '首次安装会自动准备连接凭证'">
              <template v-if="selectedNode.node_credential_prefix">本次自动化将复用现有连接凭证 <code>{{ selectedNode.node_credential_prefix }}…</code>，不会自动轮换。</template>
              <template v-else>Zboard 会在 Zero 启动前自动生成并激活连接凭证；如果安装或验收失败，该凭证会随本次 generation 一起回滚，无需先到“连接凭证”手工生成。</template>
            </PageAlert>
            <OutputBlock v-if="kernelState?.last_error" :value="kernelState.last_error" label="内核错误" tone="danger" :max-length="360" />
            <div class="kernel-release-picker">
              <label for="node-kernel-release">安装版本</label>
              <UiSelect id="node-kernel-release" v-model="selectedReleaseVersion" :options="releaseOptions" :disabled="releaseLoading || Boolean(kernelBusy)" />
              <small v-if="selectedRelease">
                {{ selectedRelease.gnu_available ? 'GNU/glibc' : '' }}{{ selectedRelease.gnu_available && selectedRelease.musl_available ? '、' : '' }}{{ selectedRelease.musl_available ? 'musl' : '' }} 制品可用
                <template v-if="selectedIsDowngrade"> · 将执行显式降级</template>
                <template v-if="!selectedReleaseCompatible"> · 与当前节点 libc 不兼容</template>
              </small>
              <small v-else>{{ releaseLoading ? '正在查询可安装版本…' : '没有与当前节点平台兼容的已发布版本。' }}</small>
            </div>
            <div class="kernel-actions">
              <UiButton variant="secondary" size="sm" type="button" :disabled="Boolean(kernelBusy) || !selectedNode.ssh_verified_at" @click="detectKernel"><UiIcon name="search" />{{ kernelBusy === 'detect' ? '检测中…' : '检测内核' }}</UiButton>
              <UiButton size="sm" type="button" :disabled="Boolean(kernelBusy) || !selectedNode.ssh_verified_at || releaseLoading || !selectedReleaseVersion || !selectedReleaseCompatible || kernelState?.status === 'unsupported'" @click="reconcileKernel"><UiIcon name="play" />{{ kernelBusy === 'reconcile' ? kernelPhaseLabel(kernelState?.phase) : reconcileButtonLabel }}</UiButton>
              <RouterLink v-if="kernelState?.last_error" class="button button-secondary button-sm" :to="adminContextLink('/admin/operation-logs', { source: 'node_kernel', status: 'failed', node_id: String(selectedNode.id) })"><UiIcon name="terminal" />查看运行日志</RouterLink>
              <small v-if="!selectedNode.ssh_verified_at">请先完成 SSH 验证，自动化不会绕过运维通道校验。</small>
              <small v-else>现代 glibc 节点使用官方 GNU 制品，旧版 Linux 使用面板托管并校验 SHA-256 的 musl 制品；不会升级系统 libc，也不会自动降级内核。</small>
            </div>
            <div class="action-card">
              <div class="title-line"><strong>VPS 网络优化 · BBR</strong><StatusBadge :tone="bbrTone">{{ bbrStatusLabel }}</StatusBadge></div>
              <p v-if="bbrState">当前拥塞控制 <code>{{ bbrState.congestion_control || '未知' }}</code> · qdisc <code>{{ bbrState.default_qdisc || '未知' }}</code> · Linux {{ bbrState.kernel_release || '未知' }}<template v-if="bbrState.persistent"> · 已持久化</template></p>
              <p v-else-if="bbrLoading">正在通过已验证 SSH 读取 BBR 状态…</p>
              <p v-else-if="!selectedNode.ssh_verified_at">完成 SSH 验证后可检测和启用 BBR。</p>
              <p v-else>尚未读取 BBR 状态。</p>
              <div class="kernel-actions">
                <UiButton variant="secondary" size="sm" type="button" :loading="bbrLoading" :disabled="bbrBusy || !selectedNode.ssh_verified_at" @click="loadBBR(selectedNode.id)"><UiIcon name="refresh" />检测 BBR</UiButton>
                <UiButton size="sm" type="button" :loading="bbrBusy" :disabled="bbrLoading || !selectedNode.ssh_verified_at" @click="applyBBR"><UiIcon name="play" />{{ bbrState?.active && bbrState?.persistent ? '重新应用 BBR' : '启用 BBR' }}</UiButton>
                <small>只维护 <code>/etc/sysctl.d/99-zboard-bbr.conf</code>，不会执行全局 <code>sysctl --system</code>。</small>
              </div>
            </div>
            <div v-if="kernelOperations.length" class="kernel-history">
              <div v-for="operation in kernelOperations.slice(0, 5)" :key="operation.id">
                <StatusBadge :tone="operationStatusTone(operation.status)" :icon="operation.status === 'succeeded' ? 'check' : operation.status === 'running' ? 'refresh' : 'alert'">{{ operationStatusLabel(operation.status) }}</StatusBadge>
                <div><strong>{{ operationLabel(operation.operation_type) }} · #{{ operation.id }}</strong><p>{{ operationSummary(operation) }}</p></div>
                <TimeBadge :value="operation.created_at" />
              </div>
            </div>
          </div>
        </article>

        <section v-else-if="detailSection === 'protocols'" class="panel node-protocols">
          <header class="panel-header"><div><h2>协议服务与倍率</h2><p>倍率属于这台 VPS 承载的协议端点。修改只影响后续流量计费与订阅展示，不会重启 Zero。</p></div><span class="count-label">{{ formatNumber(nodeProtocolTotal) }}</span></header>
          <DataTable v-if="nodeEndpoints.length" caption="当前节点协议服务与计费倍率" :row-count="nodeProtocolTotal" :min-width="820">
            <thead>
              <tr>
                <th class="table-primary-column">协议服务</th>
                <th>状态</th>
                <th data-column-priority="2">对外入口</th>
                <th>计费倍率</th>
                <th class="table-action-column"><span class="sr-only">操作</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="endpoint in nodeEndpoints" :key="endpoint.id">
                <td class="table-primary-column">
                  <div class="cell-title"><strong>{{ endpoint.name }}</strong><span>{{ endpoint.protocol }} · #{{ endpoint.id }}</span></div>
                </td>
                <td><StatusBadge :tone="endpoint.is_active ? 'success' : 'neutral'" :icon="endpoint.is_active ? 'check' : 'minus'">{{ endpoint.is_active ? '运行中' : '已停用' }}</StatusBadge></td>
                <td class="mono" data-column-priority="2">{{ endpoint.address }}:{{ endpoint.public_port || endpoint.port }}</td>
                <td><MultiplierInput v-model="multiplierDrafts[endpoint.id]" :aria-label="`${endpoint.name} 的计费倍率`" /></td>
                <td class="table-action-column">
                  <UiButton variant="secondary" size="sm" type="button" :loading="savingMultiplierID === endpoint.id" @click="saveMultiplier(endpoint)">保存倍率</UiButton>
                </td>
              </tr>
            </tbody>
          </DataTable>
          <div v-else-if="nodeProtocolsLoading" class="panel-body muted">正在加载协议服务…</div>
          <EmptyState v-else icon="activity" title="这台 VPS 还没有协议服务" description="请先在协议服务页面创建并绑定端点。" />
          <TablePager v-if="nodeProtocolTotal" :total="nodeProtocolTotal" :offset="nodeProtocolOffset" :limit="nodeProtocolLimit" :loading="nodeProtocolsLoading" @change="changeNodeProtocolPage" />
        </section>

        <section v-else class="panel credential-workspace">
          <header class="panel-header"><div><h2>连接凭证</h2><p>凭证只在需要配置或轮换时操作，不与日常状态混排。</p></div></header>
          <div class="credential-row"><span class="credential-icon"><UiIcon name="activity" /></span><div><strong>Zero 主动连接</strong><p v-if="selectedNode.node_credential_prefix">当前前缀 <code>{{ selectedNode.node_credential_prefix }}…</code></p><p v-else>用于心跳和命令领取，当前尚未生成；首次 Zero 安装会自动生成并激活，无需提前手工创建。安装失败时会随本次 generation 一起回滚。</p></div><div class="credential-actions"><UiButton variant="secondary" size="sm" type="button" @click="rotateConnector(selectedNode)">{{ selectedNode.node_credential_prefix ? '轮换' : '手动生成' }}</UiButton><UiButton v-if="selectedNode.node_credential_prefix && !selectedNode.node_credential_revoked_at" variant="danger" size="sm" type="button" @click="revokeConnector(selectedNode)">吊销</UiButton></div></div>
          <div class="credential-row"><span class="credential-icon"><UiIcon name="shield" /></span><div><strong>流量上报</strong><p v-if="selectedNode.traffic_secret_prefix">当前前缀 <code>{{ selectedNode.traffic_secret_prefix }}…</code></p><p v-else>与 SSH、Zero 连接凭证独立，当前尚未生成。</p></div><div class="credential-actions"><UiButton variant="secondary" size="sm" type="button" @click="rotateReport(selectedNode)">{{ selectedNode.traffic_secret_prefix ? '轮换' : '生成' }}</UiButton><UiButton v-if="selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at" variant="danger" size="sm" type="button" @click="revokeReport(selectedNode)">吊销</UiButton></div></div>
        </section>
      </main>
    </DetailDrawer>

    <ModalDialog :open="createOpen" :dirty="createState.dirty.value" title="登记 VPS" description="建立主机资产；可选择在 SSH 验证后执行受控的系统初始化。" :busy="saving" @close="createOpen = false">
      <form id="node-create-form" ref="createFormElement" class="form-grid" novalidate @submit.prevent="create">
        <PageAlert v-if="createErrors.formError.value" class="field-full" tone="danger" title="无法登记 VPS">{{ createErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="主机名称" name="create-node-name" :error="createErrors.fields.name" required><UiInput v-model.trim="createForm.name" v-bind="controlAttrs" placeholder="香港 VPS 01" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="区域" name="create-node-region" :error="createErrors.fields.region"><UiInput v-model.trim="createForm.region" v-bind="controlAttrs" placeholder="Hong Kong" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="默认业务地址" name="create-node-address" hint="可选；新建协议时用作对外地址的初始值。" :error="createErrors.fields.address" full><UiInput v-model.trim="createForm.address" v-bind="controlAttrs" placeholder="edge.example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="备注" hint="记录供应商、机房、用途或到期时间；不填写也保持与其他字段对齐。" full><UiTextarea v-model.trim="createForm.remark" v-bind="controlAttrs" rows="3" placeholder="供应商、机房、用途、到期时间等"></UiTextarea></FormField>
        <label class="check-field field-full"><UiCheckbox v-model="createForm.enable_bbr" /><span>节点接入后尝试启用 BBR（可选）</span></label>
        <p class="field-hint field-full">保存 VPS 后会继续引导 SSH 验证与 Zero 初始化。BBR 仅在勾选时尝试启用，失败不会阻塞 Zero 安装，可稍后在“内核与运维”中重试。</p>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="node-create-form" type="submit" :loading="saving">保存 VPS</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" :dirty="editState.dirty.value" title="编辑 VPS" description="维护资产元数据和生命周期；维护或退役会自动停止对外交付。" :busy="saving" @close="editOpen = false">
      <form id="node-edit-form" ref="editFormElement" class="form-grid" novalidate @submit.prevent="saveNode">
        <PageAlert v-if="editErrors.formError.value" class="field-full" tone="danger" title="无法保存 VPS">{{ editErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="主机名称" name="edit-node-name" :error="editErrors.fields.name" required><UiInput v-model.trim="editForm.name" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="区域" name="edit-node-region" :error="editErrors.fields.region"><UiInput v-model.trim="editForm.region" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="默认业务地址" name="edit-node-address" hint="用于新建协议时预填，不会自动修改已有协议。" :error="editErrors.fields.address" full><UiInput v-model.trim="editForm.address" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="生命周期" name="edit-node-lifecycle" :error="editErrors.fields.lifecycle_status"><UiSelect v-model="editForm.lifecycle_status" v-bind="controlAttrs" :options="lifecycleOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="承载状态" name="edit-node-enabled" :error="editErrors.fields.is_enabled"><div class="check-field"><UiCheckbox v-model="editForm.is_enabled" v-bind="controlAttrs" :disabled="editForm.lifecycle_status !== 'active'" /><span>允许承载对外服务</span></div></FormField>
        <FormField v-slot="{ controlAttrs }" label="备注" full><UiTextarea v-model.trim="editForm.remark" v-bind="controlAttrs" rows="3"></UiTextarea></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="node-edit-form" type="submit" :loading="saving">保存</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="batchKernelOpen" title="批量升级 Zero" description="为所选 VPS 固定一个目标版本，并由 Zboard 后台任务并发执行；关闭或刷新浏览器不会取消已接受的升级。" :busy="bulkBusy === 'reconcile'" @close="batchKernelOpen = false">
    <div class="form-grid">
      <FormField label="目标版本" full>
        <UiSelect v-model="batchReleaseVersion" :options="batchReleaseOptions" :disabled="releaseLoading || bulkBusy === 'reconcile'" />
      </FormField>
      <PageAlert class="field-full" tone="info" title="后台滚动执行">将对 {{ selectedNodeCount }} 台 VPS 创建一个持久化任务，服务端最多并发处理 4 台。单台失败只回滚该节点，其余节点继续。</PageAlert>
      <label class="check-field field-full"><UiCheckbox v-model="batchAllowDowngrade" /><span>允许将高于目标版本的节点显式降级</span></label>
      <p class="field-hint field-full">未勾选时，高于目标版本的节点会单独失败，不会自动降级。目标版本在任务创建时固定为 {{ batchSelectedRelease?.tag || '尚未选择' }}。</p>
    </div>
    <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="bulkBusy === 'reconcile'" @click="requestClose">取消</UiButton><UiButton type="button" :loading="bulkBusy === 'reconcile'" :disabled="!batchReleaseVersion" @click="submitBatchKernelRollout">创建升级任务</UiButton></template>
  </ModalDialog>

    <ModalDialog :open="sshOpen" :dirty="sshState.dirty.value" title="SSH 与系统权限" :description="pendingBBRNodeID === sshForm.node_id ? '保存后会先验证 SSH 与提权能力并继续 Zero 初始化；BBR 仅作为可选优化独立尝试，失败不会阻塞节点接入。' : '登录凭证和系统提权分开管理；只有安装、systemd 与协议配置等系统操作会提权。'" size="lg" :busy="saving" @close="sshOpen = false">
      <form id="ssh-form" ref="sshFormElement" class="form-grid" novalidate @submit.prevent="saveSSH">
        <PageAlert v-if="sshErrors.formError.value" class="field-full" tone="danger" title="无法保存 SSH 配置">{{ sshErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="SSH 主机" name="node-ssh-host" :error="sshErrors.fields.ssh_host" required><UiInput v-model.trim="sshForm.ssh_host" v-bind="controlAttrs" placeholder="192.0.2.10" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="端口" name="node-ssh-port" :error="sshErrors.fields.ssh_port" required><PortInput v-model="sshForm.ssh_port" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="用户" name="node-ssh-user" :error="sshErrors.fields.ssh_user" required><UiInput v-model.trim="sshForm.ssh_user" v-bind="controlAttrs" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="认证方式" name="node-ssh-auth-method" :error="sshErrors.fields.ssh_auth_method"><UiSelect v-model="sshForm.ssh_auth_method" v-bind="controlAttrs" :options="sshAuthOptions" /></FormField>
        <FormField v-if="sshForm.ssh_auth_method === 'password'" v-slot="{ controlAttrs }" name="node-ssh-password" :label="requiresSSHCredential ? 'SSH 密码' : '替换密码（可留空）'" :error="sshErrors.fields.ssh_password" :required="requiresSSHCredential" full><UiInput v-model="sshForm.ssh_password" v-bind="controlAttrs" type="password" autocomplete="new-password" /></FormField>
        <template v-else><FormField v-slot="{ controlAttrs }" name="node-ssh-private-key" :label="requiresSSHCredential ? 'SSH 私钥' : '替换私钥（可留空）'" :error="sshErrors.fields.ssh_private_key" :required="requiresSSHCredential" full><UiTextarea v-model="sshForm.ssh_private_key" v-bind="controlAttrs" rows="8" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></UiTextarea></FormField><FormField v-slot="{ controlAttrs }" label="私钥口令（可选）" name="node-ssh-passphrase" hint="留空表示保留已保存口令；替换私钥时可同时填写新口令。" :error="sshErrors.fields.ssh_private_key_passphrase" full><UiInput v-model="sshForm.ssh_private_key_passphrase" v-bind="controlAttrs" type="password" autocomplete="new-password" /></FormField><label v-if="sshForm.hasCredential" class="check-field field-full"><UiCheckbox v-model="sshForm.clearPassphrase" /><span>清除已保存的私钥口令</span></label></template>
        <FormField v-slot="{ controlAttrs }" label="系统提权" name="node-ssh-privilege-mode" :error="sshErrors.fields.ssh_privilege_mode"><UiSelect v-model="sshForm.ssh_privilege_mode" v-bind="controlAttrs" :options="sshPrivilegeOptions" /></FormField>
        <FormField v-if="sshForm.ssh_privilege_mode !== 'none'" v-slot="{ controlAttrs }" name="node-ssh-privilege-password" :label="sshForm.hasPrivilegePassword ? '替换提权密码（可留空）' : sshForm.ssh_privilege_mode === 'su' ? 'root 密码' : 'sudo 密码（可选）'" :error="sshErrors.fields.ssh_privilege_password" :required="sshForm.ssh_privilege_mode === 'su' && !sshForm.hasPrivilegePassword"><UiInput v-model="sshForm.ssh_privilege_password" v-bind="controlAttrs" type="password" autocomplete="new-password" /></FormField>
        <label v-if="sshForm.ssh_privilege_mode === 'sudo'" class="check-field field-full"><UiCheckbox v-model="sshForm.passwordlessSudo" /><span>该用户已配置免密 sudo（启用后会清除已保存的提权密码）</span></label>
        <p v-if="sshForm.ssh_privilege_mode === 'su'" class="field-hint field-full">适用于可使用 <code>su root -c</code> 的主机；root 密码独立加密保存，不会写入命令、任务输出或审计日志。</p>
        <p class="field-hint field-full">首次验证会自动固定服务器身份；以后发现主机密钥变化时会停止连接。仅在确认 VPS 已重装或更换后，使用“重新信任主机”。</p>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="ssh-form" type="submit" :loading="saving">{{ pendingBBRNodeID === sshForm.node_id ? '保存并初始化 BBR' : '保存连接配置' }}</UiButton></template>
    </ModalDialog>

    <ModalDialog :open="Boolean(secretModal.value)" :title="secretModal.title" description="完整凭证只显示一次，请立即复制并安全保存。" @close="closeSecretModal">
      <div class="secret-card"><UiTextarea :value="secretModal.value" rows="8" readonly></UiTextarea><UiButton variant="secondary" type="button" @click="copySecret"><UiIcon name="copy" />{{ secretModal.copied ? '已复制' : '复制' }}</UiButton></div>
      <template #footer><UiButton type="button" @click="closeSecretModal">完成</UiButton></template>
    </ModalDialog>

    <NodeRuntimeDiagnosticsModal :open="diagnosticsOpen" :node-id="selectedNode?.id || 0" :node-name="selectedNode?.name" :ssh-ready="Boolean(selectedNode?.ssh_verified_at)" @close="diagnosticsOpen = false" />
    <SshTerminalDialog :open="terminalOpen" :node="terminalNode" @close="terminalOpen = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createNode, createNodeBatchOperation, deleteNode, detectNodeKernel, fetchAdminTask, fetchNode, fetchNodeKernel, fetchNodeLoad, fetchNodesPage, fetchProtocolEndpointsPage, fetchZeroReleases, reconcileNodeKernel, resetNodeSSHHostKey, revokeNodeConnectorCredential, revokeNodeReportCredential, rotateNodeConnectorCredential, rotateNodeReportCredential, testNodeSSH, updateNode, updateNodeSSH, updateProtocolEndpointMultiplier, type AdminNodeDetail, type AdminNodeListItem, type NodeKernelOperation, type NodeKernelState, type NodeLoadSnapshot, type ZeroReleaseOption } from '../api/client'
import { enableNodeBBR, fetchNodeSystemActions, type NodeBBRState } from '../api/nodeSystemActions'
import DataWorkbench from '../components/DataWorkbench.vue'
import DataTable from '../components/DataTable.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import MultiplierInput from '../components/MultiplierInput.vue'
import NodeRuntimeDiagnosticsModal from '../components/NodeRuntimeDiagnosticsModal.vue'
import OutputBlock from '../components/OutputBlock.vue'
import PageAlert from '../components/PageAlert.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PortInput from '../components/PortInput.vue'
import SshTerminalDialog from '../components/SshTerminalDialog.vue'
import SortableHeader from '../components/SortableHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import StatusOverviewItem from '../components/StatusOverviewItem.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import UiTabs from '../components/UiTabs.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useSelectionScope } from '../composables/useSelectionScope'
import { nextSortDirection, resolveSortDirection, resolveSortField, resolveTableDensity } from '../composables/tableState'
import { confirmAction } from '../utils/feedback'
import { formatBytes, formatNumber, formatUnknownValue } from '../utils/format'
import { normalizeOutput, truncateOutput } from '../utils/output'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'
import { createRequestGuard } from '../utils/request'
import { trackAdminTask, updateTrackedTask } from '../utils/taskTracker'
import { collectFieldErrors, isBlank, isIntegerInRange, isOneOf } from '../utils/validation'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const initialPage = Math.max(1, Number(route.query.page) || 1)
const offset = ref((initialPage - 1) * limit.value)
const filters = reactive({
  q: typeof route.query.q === 'string' ? route.query.q : '',
  lifecycle: typeof route.query.lifecycle === 'string' ? route.query.lifecycle : '',
  connector: typeof route.query.connector === 'string' ? route.query.connector : '',
})
type NodeSortField = 'id' | 'name' | 'region' | 'last_seen_at'
const nodeSortFields = new Set<NodeSortField>(['id', 'name', 'region', 'last_seen_at'])
const sortField = ref(resolveSortField(route.query.sort, nodeSortFields, 'id'))
const sortDirection = ref<'asc' | 'desc'>(resolveSortDirection(route.query.direction, 'desc'))
const density = ref<'compact' | 'comfortable'>(resolveTableDensity(route.query.density))
const lifecycleOptions = [{ label: '正常', value: 'active' }, { label: '维护', value: 'maintenance' }, { label: '退役', value: 'retired' }]
const lifecycleFilterOptions = [{ label: '全部生命周期', value: '' }, ...lifecycleOptions]
const connectorFilterOptions = [{ label: '全部连接状态', value: '' }, { label: 'Zero 在线', value: 'online' }, { label: 'Zero 离线', value: 'offline' }]
const sshAuthOptions = [{ label: '密码', value: 'password' }, { label: '私钥', value: 'private_key' }]
const sshPrivilegeOptions = [{ label: '直接登录 root', value: 'none' }, { label: 'sudo 提权', value: 'sudo' }, { label: 'su 切换 root', value: 'su' }]
const selectedNode = ref<AdminNodeDetail | null>(null)
const deletingNode = ref(false)
const nodeLoad = ref<NodeLoadSnapshot | null>(null)
const nodeLoadLoading = ref(false)
const nodeLoadError = ref('')
const diagnosticsOpen = ref(false)
const detailSection = ref<'overview' | 'kernel' | 'protocols' | 'credentials'>('overview')
const nodeDetailTabs = [
  { value: 'overview', label: '状态概览', icon: 'dashboard' },
  { value: 'kernel', label: '内核与运维', icon: 'activity' },
  { value: 'protocols', label: '协议与倍率', icon: 'plans' },
  { value: 'credentials', label: '连接凭证', icon: 'key' },
]
const saving = ref(false), testingNode = ref(0), detailLoadingID = ref(0)
const bulkBusy = ref<'' | 'detect' | 'reconcile' | 'activate' | 'maintenance' | 'retire'>('')
const message = ref('')
const detailMessage = ref('')
const detailError = ref('')
const createOpen = ref(false), editOpen = ref(false), sshOpen = ref(false)
const batchKernelOpen = ref(false), batchReleaseVersion = ref(''), batchAllowDowngrade = ref(false)
const terminalOpen = ref(false), terminalNode = ref<any>(null)
const kernelState = ref<NodeKernelState | null>(null), kernelOperations = ref<NodeKernelOperation[]>([])
const bbrState = ref<NodeBBRState | null>(null), bbrLoading = ref(false), bbrBusy = ref(false)
const pendingBBRNodeID = ref(0)
const zeroReleases = ref<ZeroReleaseOption[]>([]), releaseLoading = ref(false)
const selectedReleaseVersion = ref('')
const latestPublishedRelease = computed(() => zeroReleases.value[0] || null)
const selectedRelease = computed(() => zeroReleases.value.find(item => item.version === selectedReleaseVersion.value) || null)
const nodeRequiresMusl = computed(() => {
  const libc = kernelState.value?.libc?.toLowerCase() || ''
  if (libc.includes('musl')) return true
  const match = libc.match(/glibc\s+(\d+)\.(\d+)/)
  if (!match) return false
  return Number(match[1]) < 2 || (Number(match[1]) === 2 && Number(match[2]) < 34)
})
const releaseCompatible = (release: ZeroReleaseOption) => nodeRequiresMusl.value ? release.musl_available : release.gnu_available
const selectedReleaseCompatible = computed(() => Boolean(selectedRelease.value && releaseCompatible(selectedRelease.value)))
const releaseOptions = computed(() => zeroReleases.value.map(item => ({
  label: `${item.tag}${item.prerelease ? '（预发布）' : ''} · ${[item.gnu_available ? 'GNU' : '', item.musl_available ? 'musl' : ''].filter(Boolean).join(' / ')}`,
  value: item.version,
  disabled: !releaseCompatible(item),
})))
const batchReleaseOptions = computed(() => zeroReleases.value.map(item => ({
  label: `${item.tag}${item.prerelease ? '（预发布）' : ''} · ${[item.gnu_available ? 'GNU' : '', item.musl_available ? 'musl' : ''].filter(Boolean).join(' / ')}`,
  value: item.version,
  disabled: !item.gnu_available && !item.musl_available,
})))
const batchSelectedRelease = computed(() => zeroReleases.value.find(item => item.version === batchReleaseVersion.value) || null)
const selectedIsDowngrade = computed(() => Boolean(
  selectedReleaseVersion.value
  && kernelState.value?.installed_version
  && compareKernelVersions(kernelState.value.installed_version, selectedReleaseVersion.value) > 0,
))
const bbrTone = computed<'success' | 'warning' | 'neutral'>(() => bbrState.value?.active && bbrState.value?.persistent ? 'success' : bbrState.value?.available ? 'warning' : 'neutral')
const bbrStatusLabel = computed(() => {
  if (bbrLoading.value) return '检测中'
  if (!bbrState.value) return '未检测'
  if (bbrState.value.active && bbrState.value.persistent && bbrState.value.default_qdisc === 'fq') return '已启用'
  if (bbrState.value.active) return '已生效，未完整持久化'
  return bbrState.value.available ? '可启用' : '当前未提供'
})
const kernelBusy = ref<'' | 'detect' | 'reconcile'>('')
const nodeEndpoints = ref<any[]>([]), nodeProtocolsLoading = ref(false), savingMultiplierID = ref(0)
const nodeProtocolLimit = ref(allowedPageSizes.includes(Number(route.query.protocol_limit)) ? Number(route.query.protocol_limit) : 25)
const nodeProtocolOffset = ref((Math.max(1, Number(route.query.protocol_page) || 1) - 1) * nodeProtocolLimit.value)
const nodeProtocolTotal = ref(0)
const multiplierDrafts = reactive<Record<number, number>>({})
const kernelRequests = createRequestGuard()
const protocolRequests = createRequestGuard()
let kernelPollTimer: number | undefined
const createForm = reactive({ name: '', region: '', address: '', remark: '', enable_bbr: false })
const editForm = reactive({ id: 0, name: '', region: '', address: '', remark: '', lifecycle_status: 'active', is_enabled: true })
const sshForm = reactive({ node_id: 0, ssh_host: '', ssh_port: 22, ssh_user: 'root', ssh_auth_method: 'password' as 'password' | 'private_key', original_auth_method: 'password' as 'password' | 'private_key', ssh_password: '', ssh_private_key: '', ssh_private_key_passphrase: '', clearPassphrase: false, hasCredential: false, ssh_privilege_mode: 'none' as 'none' | 'sudo' | 'su', ssh_privilege_password: '', hasPrivilegePassword: false, passwordlessSudo: false })
const requiresSSHCredential = computed(() => !sshForm.hasCredential || sshForm.ssh_auth_method !== sshForm.original_auth_method)
const createFormElement = ref<HTMLElement | null>(null)
const editFormElement = ref<HTMLElement | null>(null)
const sshFormElement = ref<HTMLElement | null>(null)
const createErrors = useFormErrors()
const editErrors = useFormErrors()
const sshErrors = useFormErrors()
const nodeCreateFieldMap: Record<string, string> = { name: 'name', region: 'region', address: 'address' }
const nodeEditFieldMap: Record<string, string> = { name: 'name', region: 'region', address: 'address', lifecycle_status: 'lifecycle_status', is_enabled: 'is_enabled' }
const sshFieldMap: Record<string, string> = {
  ssh_host: 'ssh_host', ssh_port: 'ssh_port', ssh_user: 'ssh_user', ssh_auth_method: 'ssh_auth_method',
  ssh_password: 'ssh_password', ssh_private_key: 'ssh_private_key', ssh_private_key_passphrase: 'ssh_private_key_passphrase',
  ssh_privilege_mode: 'ssh_privilege_mode', ssh_privilege_password: 'ssh_privilege_password',
}
const createState = useDirtyForm(() => createForm)
const editState = useDirtyForm(() => editForm)
const sshState = useDirtyForm(() => sshForm)
useUnsavedChangesGuard(
  () => (createOpen.value && createState.dirty.value)
    || (editOpen.value && editState.dirty.value)
    || (sshOpen.value && sshState.dirty.value),
  async () => {
    if (createOpen.value && !await createState.confirmDiscard({
      title: '放弃 VPS 登记草稿？',
      message: '离开节点资产后，尚未登记的主机信息将丢失。',
      confirmText: '离开页面',
    })) return false
    if (editOpen.value && !await editState.confirmDiscard({
      title: '放弃 VPS 修改？',
      message: '离开节点资产后，尚未保存的资产修改将丢失。',
      confirmText: '离开页面',
    })) return false
    if (sshOpen.value && !await sshState.confirmDiscard({
      title: '放弃 SSH 配置修改？',
      message: '离开节点资产后，尚未保存的 SSH 连接配置将丢失。',
      confirmText: '离开页面',
    })) return false
    return true
  },
)

for (const [source, field] of [
  [() => createForm.name, 'name'], [() => createForm.region, 'region'], [() => createForm.address, 'address'],
] as Array<[() => unknown, string]>) watch(source, () => createErrors.clear(field))
for (const [source, field] of [
  [() => editForm.name, 'name'], [() => editForm.region, 'region'], [() => editForm.address, 'address'],
  [() => editForm.lifecycle_status, 'lifecycle_status'], [() => editForm.is_enabled, 'is_enabled'],
] as Array<[() => unknown, string]>) watch(source, () => editErrors.clear(field))
for (const [source, field] of [
  [() => sshForm.ssh_host, 'ssh_host'], [() => sshForm.ssh_port, 'ssh_port'], [() => sshForm.ssh_user, 'ssh_user'],
  [() => sshForm.ssh_password, 'ssh_password'], [() => sshForm.ssh_private_key, 'ssh_private_key'],
  [() => sshForm.ssh_private_key_passphrase, 'ssh_private_key_passphrase'],
  [() => sshForm.ssh_privilege_mode, 'ssh_privilege_mode'], [() => sshForm.ssh_privilege_password, 'ssh_privilege_password'],
] as Array<[() => unknown, string]>) watch(source, () => sshErrors.clear(field))
watch(() => sshForm.ssh_auth_method, () => { sshErrors.clear('ssh_auth_method'); sshErrors.clear('ssh_password'); sshErrors.clear('ssh_private_key') })
const secretModal = reactive({ title: '', value: '', copied: false, nodeID: 0 })
const { items: nodes, total, loading, initialLoading, refreshing, error, load: refresh } = useRemoteTable<AdminNodeListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchNodesPage({
    offset: offset.value,
    limit: limit.value,
    q: filters.q || undefined,
    lifecycleStatus: filters.lifecycle || undefined,
    connectorOnline: filters.connector ? filters.connector === 'online' : undefined,
    sort: sortField.value,
    direction: sortDirection.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '节点加载失败。',
  onOffsetCorrected: () => syncURL(true),
  onPageLoaded: (page) => {
    if (!selectedNode.value) return
    const summary = page.items.find(item => item.id === selectedNode.value?.id)
    if (summary) selectedNode.value = { ...selectedNode.value, ...summary }
  },
})
const {
  selectedIDs: selectedNodeIDs,
  allMatching: selectionAllMatching,
  selectedCount: selectedNodeCount,
  allPageSelected: allPageNodesSelected,
  pageSelectionIndeterminate: pageNodeSelectionIndeterminate,
  canSelectAllMatching,
  isSelected: isNodeSelected,
  toggle: toggleNodeSelection,
  togglePage: toggleCurrentNodePage,
  selectAllMatching,
  clear: clearSelection,
} = useSelectionScope({ items: nodes, total, key: node => node.id })

const kernelTone = computed<'success' | 'warning' | 'danger' | 'neutral'>(() => {
  const status = kernelState.value?.status || 'unknown'
  if (status === 'healthy') return 'success'
  if (status === 'unknown') return 'neutral'
  if (status === 'not_installed' || status === 'unsupported' || status === 'publishing') return 'warning'
  return 'danger'
})
const kernelStatusLabel = computed(() => ({
  unknown: '未检测',
  not_installed: '未安装',
  healthy: '健康',
  degraded: '运行异常',
  failed: '操作失败',
  unsupported: '制品不兼容',
  publishing: '配置发布中',
  apply_failed: '配置应用失败',
} as Record<string, string>)[kernelState.value?.status || 'unknown'] || formatUnknownValue('状态', kernelState.value?.status))
const kernelDescription = computed(() => kernelState.value?.installed_version ? `Zero ${kernelState.value.installed_version} · ${serviceLabel(kernelState.value.service_status)}` : kernelStatusLabel.value)
const reconcileButtonLabel = computed(() => kernelState.value?.status === 'unsupported' ? '当前制品不兼容' : kernelState.value?.status === 'not_installed' ? '自动安装' : kernelState.value?.recommended_action === 'configure' ? '应用配置' : '安装 / 升级 / 修复')
function lifecycleLabel(value?: string) { return ({ active: '正常', maintenance: '维护', retired: '退役' } as Record<string, string>)[value || ''] || formatUnknownValue('状态', value) }
function lifecycleTone(node: any): 'success' | 'warning' | 'neutral' { return node.lifecycle_status === 'maintenance' ? 'warning' : node.lifecycle_status === 'retired' ? 'neutral' : node.is_enabled ? 'success' : 'neutral' }
function kernelListLabel(value?: string) { return ({ unknown: '未检测', not_installed: '未安装', healthy: '健康', degraded: '异常', failed: '失败', unsupported: '不兼容', publishing: '发布中', apply_failed: '应用失败' } as Record<string, string>)[value || 'unknown'] || formatUnknownValue('状态', value) }
function kernelListTone(value?: string): 'success' | 'warning' | 'danger' | 'neutral' { if (value === 'healthy') return 'success'; if (!value || value === 'unknown') return 'neutral'; if (value === 'not_installed' || value === 'unsupported' || value === 'publishing') return 'warning'; return 'danger' }
function sshDescription(node: any) { if (!node.ssh_host) return '尚未配置'; const auth = node.ssh_auth_method === 'private_key' ? '私钥' : '密码'; const privilege = ({ none: 'root 直连', sudo: 'sudo 提权', su: 'su 提权' } as Record<string,string>)[node.ssh_privilege_mode || 'none']; const host = node.ssh_host_key_fingerprint ? '主机身份已固定' : '等待首次连接'; return `${auth}认证 · ${privilege} · ${host}` }
function serviceLabel(value?: string) { return ({ active: '运行中', inactive: '已停止', failed: '启动失败', not_found: '未安装', unknown: '未检测' } as Record<string, string>)[value || 'unknown'] || formatUnknownValue('状态', value) }
function controlLabel(value?: string) { return value === 'healthy' ? '健康' : value === 'unavailable' ? '不可用' : !value || value === 'unknown' ? '未检测' : formatUnknownValue('状态', value) }
function serviceTone(value?: string): 'success' | 'warning' | 'danger' | 'neutral' { return value === 'active' ? 'success' : value === 'inactive' || value === 'not_found' ? 'warning' : value === 'failed' ? 'danger' : 'neutral' }
function controlTone(value?: string): 'success' | 'danger' | 'neutral' { return value === 'healthy' ? 'success' : value === 'unavailable' ? 'danger' : 'neutral' }
function operationStatusLabel(value: string) { return ({ running: '执行中', succeeded: '已完成', failed: '执行失败' } as Record<string, string>)[value] || formatUnknownValue('状态', value) }
function operationStatusTone(value: string): 'info' | 'success' | 'danger' { return value === 'running' ? 'info' : value === 'succeeded' ? 'success' : 'danger' }
function kernelActionLabel(value?: string) { return ({ detect: '先检测', install: '安装', upgrade: '升级', repair: '修复', configure: '同步配置', check_release: '检查新版本', manual_review: '人工确认', none: '无需操作' } as Record<string, string>)[value || 'detect'] || formatUnknownValue('动作', value) }
function compareKernelVersions(left: string, right: string) {
  const parse = (value: string) => {
    const [core, suffix = ''] = value.trim().replace(/^v/, '').split('-', 2)
    return { numbers: core.split('.').map(item => Number(item) || 0), suffix }
  }
  const a = parse(left)
  const b = parse(right)
  for (let index = 0; index < Math.max(a.numbers.length, b.numbers.length); index += 1) {
    const difference = (a.numbers[index] || 0) - (b.numbers[index] || 0)
    if (difference) return difference > 0 ? 1 : -1
  }
  if (a.suffix === b.suffix) return 0
  if (!a.suffix) return 1
  if (!b.suffix) return -1
  const leftIdentifiers = a.suffix.split('.')
  const rightIdentifiers = b.suffix.split('.')
  for (let index = 0; index < Math.min(leftIdentifiers.length, rightIdentifiers.length); index += 1) {
    if (leftIdentifiers[index] === rightIdentifiers[index]) continue
    const leftNumber = /^\d+$/.test(leftIdentifiers[index]) ? Number(leftIdentifiers[index]) : null
    const rightNumber = /^\d+$/.test(rightIdentifiers[index]) ? Number(rightIdentifiers[index]) : null
    if (leftNumber !== null && rightNumber !== null) return leftNumber > rightNumber ? 1 : -1
    if (leftNumber !== null) return -1
    if (rightNumber !== null) return 1
    return leftIdentifiers[index].localeCompare(rightIdentifiers[index])
  }
  return leftIdentifiers.length === rightIdentifiers.length ? 0 : leftIdentifiers.length > rightIdentifiers.length ? 1 : -1
}
function operationLabel(value?: string) { return ({ detect: '环境检测', reconcile: '状态对齐', install: '安装', upgrade: '升级', downgrade: '降级', repair: '修复', configure: '配置同步', none: '状态确认' } as Record<string, string>)[value || 'reconcile'] || formatUnknownValue('操作', value) }
function operationSummary(operation: NodeKernelOperation) { return truncateOutput(normalizeOutput(operation.result_summary || operation.error || kernelPhaseLabel(operation.phase)), 220) }
async function runBatch(action: 'detect' | 'activate' | 'maintenance' | 'retire') {
  const labels = { detect: '检测节点状态', activate: '恢复启用节点', maintenance: '将节点设为维护', retire: '退役节点' }
  const count = selectedNodeCount.value
  const accepted = await confirmAction({ title: labels[action], message: `将对 ${count} 个节点创建后台任务。请求被接受后可以离开页面，最终成功、失败和部分失败会持续显示。`, confirmText: action === 'retire' ? '确认退役' : '创建任务', tone: action === 'retire' ? 'danger' : 'primary' })
  if (!accepted) return
  bulkBusy.value = action; error.value = ''; message.value = ''
  try {
    const task = await createNodeBatchOperation({ action, ...(selectionAllMatching.value ? { all_matching: true, filters: { q: filters.q || undefined, lifecycle_status: filters.lifecycle || undefined, connector_online: filters.connector ? filters.connector === 'online' : undefined } } : { node_ids: selectedNodeIDs.value }) })
    trackAdminTask(task); clearSelection(); message.value = `后台任务 #${task.id} 已接受；最终结果会显示在任务托盘。`
  } catch (e: any) { error.value = e?.response?.data?.message || '批量节点任务创建失败。' }
  finally { bulkBusy.value = '' }
}
async function openBatchKernelRollout() {
  bulkBusy.value = 'reconcile'; error.value = ''; message.value = ''
  await loadLatestRelease()
  bulkBusy.value = ''
  batchReleaseVersion.value = zeroReleases.value[0]?.version || ''
  batchAllowDowngrade.value = false
  if (!batchReleaseVersion.value) { error.value = '没有可用于批量升级的 Zero 发布版本。'; return }
  batchKernelOpen.value = true
}
async function submitBatchKernelRollout() {
  if (!batchReleaseVersion.value) return
  bulkBusy.value = 'reconcile'; error.value = ''; message.value = ''
  try {
    const task = await createNodeBatchOperation({
      action: 'reconcile',
      version: batchReleaseVersion.value,
      allow_downgrade: batchAllowDowngrade.value,
      ...(selectionAllMatching.value ? { all_matching: true, filters: { q: filters.q || undefined, lifecycle_status: filters.lifecycle || undefined, connector_online: filters.connector ? filters.connector === 'online' : undefined } } : { node_ids: selectedNodeIDs.value }),
    })
    trackAdminTask(task)
    batchKernelOpen.value = false
    clearSelection()
    message.value = `Zero 批量升级任务 #${task.id} 已接受；目标 ${batchSelectedRelease.value?.tag || batchReleaseVersion.value}，浏览器可安全离开。`
  } catch (e: any) { error.value = e?.response?.data?.message || 'Zero 批量升级任务创建失败。' }
  finally { bulkBusy.value = '' }
}
function kernelPhaseLabel(value?: string) { return ({ queued: '排队中…', detecting: '检测中…', resolving_release: '匹配制品…', preparing_connector_credential: '准备连接凭证…', downloading: '校验制品…', staging: '暂存并切换…', verifying: '本地健康检查…', waiting_connector_event: '等待 Connector 事件…', waiting_heartbeat: '等待兼容心跳…', completed: '已完成' } as Record<string, string>)[value || 'queued'] || '处理中…' }

async function loadKernel(nodeID?: number) {
  const request = kernelRequests.begin()
  if (!nodeID) { kernelState.value = null; kernelOperations.value = []; return }
  try {
    const result = await fetchNodeKernel(nodeID)
    if (!kernelRequests.isCurrent(request) || selectedNode.value?.id !== nodeID || detailSection.value !== 'kernel') return
    kernelState.value = result.state
    kernelOperations.value = result.operations || []
  } catch (e: any) {
    if (kernelRequests.isCurrent(request) && selectedNode.value?.id === nodeID) detailError.value = e?.response?.data?.message || '内核状态加载失败。'
  }
}
async function loadBBR(nodeID?: number) {
  if (!nodeID || !selectedNode.value?.ssh_verified_at) { bbrState.value = null; return }
  bbrLoading.value = true
  try {
    const snapshot = await fetchNodeSystemActions(nodeID)
    if (selectedNode.value?.id === nodeID) bbrState.value = snapshot.bbr
  } catch (e: any) {
    if (selectedNode.value?.id === nodeID) detailError.value = e?.response?.data?.message || 'BBR 状态读取失败。'
  } finally {
    if (selectedNode.value?.id === nodeID) bbrLoading.value = false
  }
}
async function applyBBR() {
  if (!selectedNode.value?.ssh_verified_at) return
  const nodeID = selectedNode.value.id
  const accepted = await confirmAction({
    title: bbrState.value?.active && bbrState.value?.persistent ? '重新应用 BBR？' : '启用 BBR？',
    message: '将通过已验证的 SSH 与系统提权修改 TCP 拥塞控制和默认 qdisc，只维护 zboard 自己的 sysctl 配置；失败时恢复原值。',
    confirmText: bbrState.value?.active && bbrState.value?.persistent ? '重新应用' : '启用 BBR',
    tone: 'primary',
  })
  if (!accepted) return
  bbrBusy.value = true; detailError.value = ''; detailMessage.value = ''
  try {
    const snapshot = await enableNodeBBR(nodeID)
    bbrState.value = snapshot.bbr
    detailMessage.value = 'BBR 已启用并通过运行时与持久化配置复核。'
  } catch (e: any) {
    detailError.value = e?.response?.data?.message || 'BBR 自动化操作失败。'
    await loadBBR(nodeID)
  } finally { bbrBusy.value = false }
}
async function loadLatestRelease(force = false) {
  if (zeroReleases.value.length && !force) return
  releaseLoading.value = true
  try {
    zeroReleases.value = await fetchZeroReleases()
    if (!zeroReleases.value.some(item => item.version === selectedReleaseVersion.value && releaseCompatible(item))) {
      selectedReleaseVersion.value = zeroReleases.value.find(releaseCompatible)?.version || ''
    }
  } catch (e: any) {
    detailError.value = e?.response?.data?.message || 'Zero 已发布版本查询失败。'
  } finally {
    releaseLoading.value = false
  }
}
async function loadNodeProtocols(nodeID?: number) {
  const request = protocolRequests.begin()
  if (!nodeID) { nodeEndpoints.value = []; nodeProtocolTotal.value = 0; return }
  nodeProtocolsLoading.value = true
  try {
    const page = await fetchProtocolEndpointsPage({ nodeId: nodeID, offset: nodeProtocolOffset.value, limit: nodeProtocolLimit.value })
    if (!protocolRequests.isCurrent(request) || selectedNode.value?.id !== nodeID || detailSection.value !== 'protocols') return
    nodeProtocolTotal.value = page.total
    if (nodeProtocolOffset.value >= page.total && nodeProtocolOffset.value > 0) {
      nodeProtocolOffset.value = Math.max(0, Math.floor(Math.max(0, page.total - 1) / nodeProtocolLimit.value) * nodeProtocolLimit.value)
      await syncURL(true)
      return
    }
    nodeEndpoints.value = page.items
    for (const endpoint of nodeEndpoints.value) multiplierDrafts[endpoint.id] = Number(endpoint.multiplier_milli || 1000)
  } catch (e: any) {
    if (protocolRequests.isCurrent(request) && selectedNode.value?.id === nodeID) detailError.value = e?.response?.data?.message || '节点协议服务加载失败。'
  } finally {
    if (protocolRequests.isCurrent(request)) nodeProtocolsLoading.value = false
  }
}
async function loadNodeLoad(nodeID?: number) {
  if (!nodeID || nodeLoadLoading.value) return
  nodeLoadLoading.value = true
  nodeLoadError.value = ''
  try {
    const snapshot = await fetchNodeLoad(nodeID)
    if (selectedNode.value?.id === nodeID) nodeLoad.value = snapshot
  } catch (cause: any) {
    if (selectedNode.value?.id === nodeID) nodeLoadError.value = cause?.response?.data?.message || '节点负载读取失败。'
  } finally {
    if (selectedNode.value?.id === nodeID) nodeLoadLoading.value = false
  }
}
function formatLoadRatio(snapshot: NodeLoadSnapshot) { return snapshot.cpu_core_count > 0 ? `${(snapshot.load_average_1 / snapshot.cpu_core_count * 100).toFixed(0)}%` : '—' }
function formatUptime(seconds: number) { const days = Math.floor(seconds / 86400); const hours = Math.floor(seconds % 86400 / 3600); const minutes = Math.floor(seconds % 3600 / 60); return days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分钟` : `${minutes} 分钟` }
async function saveMultiplier(endpoint: any) { const milli = Math.round(Number(multiplierDrafts[endpoint.id])); if (!Number.isFinite(milli) || milli < 1 || milli > 100000) { detailError.value = '计费倍率必须在 0.001× 到 100× 之间。'; return }; savingMultiplierID.value = endpoint.id; detailError.value = ''; detailMessage.value = ''; try { const updated = await updateProtocolEndpointMultiplier(endpoint.id, milli); endpoint.multiplier_milli = updated.multiplier_milli; multiplierDrafts[endpoint.id] = updated.multiplier_milli; detailMessage.value = `${endpoint.name} 的计费倍率已更新为 ${updated.multiplier_milli / 1000}×。` } catch (e: any) { detailError.value = e?.response?.data?.message || '计费倍率保存失败。' } finally { savingMultiplierID.value = 0 } }
async function detectKernel() { if (!selectedNode.value) return; kernelBusy.value = 'detect'; detailError.value = ''; detailMessage.value = ''; try { const result = await detectNodeKernel(selectedNode.value.id); kernelState.value = result.state; detailMessage.value = 'Zero 内核检测完成，页面显示的是服务器真实状态。'; await loadKernel(selectedNode.value.id) } catch (e: any) { detailError.value = e?.response?.data?.message || 'Zero 内核检测失败。'; await loadKernel(selectedNode.value.id) } finally { kernelBusy.value = '' } }
function stopKernelPolling() { if (kernelPollTimer !== undefined) { window.clearInterval(kernelPollTimer); kernelPollTimer = undefined } }
function startKernelPolling(nodeID: number, taskID: number) {
  stopKernelPolling()
  kernelPollTimer = window.setInterval(async () => {
    try {
      const [task, result] = await Promise.all([fetchAdminTask(taskID), fetchNodeKernel(nodeID)])
      updateTrackedTask(task)
      if (selectedNode.value?.id === nodeID) {
        kernelState.value = result.state
        kernelOperations.value = result.operations || []
      }
      if (task.status >= 2) {
        stopKernelPolling()
        kernelBusy.value = ''
        if (task.status === 2) detailMessage.value = `Zero 后台任务 #${taskID} 已完成并通过节点验收。`
        else detailError.value = task.errors || `Zero 后台任务 #${taskID} 执行失败，请查看任务详情。`
        await refresh()
      }
    } catch { /* TaskTray remains the durable progress surface if this page poll is interrupted. */ }
  }, 1500)
}
async function reconcileKernel() {
  if (!selectedNode.value || !selectedRelease.value) return
  const nodeID = selectedNode.value.id
  const target = selectedRelease.value.tag
  const downgrade = selectedIsDowngrade.value
  const credentialMessage = selectedNode.value.node_credential_prefix
    ? `将复用现有 Zero 连接凭证 ${selectedNode.value.node_credential_prefix}…，自动化不会轮换。`
    : '当前尚无 Zero 连接凭证；本次任务会在 Zero 启动前自动生成并激活，若安装或验收失败会自动回滚该凭证。'
  const accepted = await confirmAction({
    title: downgrade ? '确认降级 Zero 内核' : '对齐 Zero 内核',
    message: `将把这台 VPS 的 Zero ${downgrade ? '显式降级' : '对齐'}到 ${target} 及当前启用协议配置。${credentialMessage} 提交后由 Zboard 后台执行，关闭或刷新浏览器不会取消任务；单机验收失败仍自动回滚。`,
    confirmText: downgrade ? '确认降级' : '创建后台任务',
    tone: downgrade ? 'danger' : 'primary',
  })
  if (!accepted) return
  kernelBusy.value = 'reconcile'; detailError.value = ''; detailMessage.value = ''
  try {
    const task = await reconcileNodeKernel(nodeID, { version: selectedRelease.value.version, allow_downgrade: downgrade })
    trackAdminTask(task)
    detailMessage.value = `Zero 后台任务 #${task.id} 已接受，目标 ${target}；可以安全离开或刷新页面。`
    startKernelPolling(nodeID, task.id)
  } catch (e: any) {
    kernelBusy.value = ''
    detailError.value = e?.response?.data?.message || 'Zero 后台任务创建失败。'
    await loadKernel(nodeID)
  }
}
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = { query: {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(filters.q ? { q: filters.q } : {}),
    ...(filters.lifecycle ? { lifecycle: filters.lifecycle } : {}),
    ...(filters.connector ? { connector: filters.connector } : {}),
    ...(sortField.value !== 'id' ? { sort: sortField.value } : {}),
    ...(sortField.value !== 'id' || sortDirection.value !== 'desc' ? { direction: sortDirection.value } : {}),
    ...(density.value !== 'compact' ? { density: density.value } : {}),
    ...(page > 1 ? { page: String(page) } : {}),
    ...(limit.value !== 50 ? { limit: String(limit.value) } : {}),
    ...(selectedNode.value && detailSection.value === 'protocols' && nodeProtocolOffset.value > 0 ? { protocol_page: String(Math.floor(nodeProtocolOffset.value / nodeProtocolLimit.value) + 1) } : {}),
    ...(selectedNode.value && detailSection.value === 'protocols' && nodeProtocolLimit.value !== 25 ? { protocol_limit: String(nodeProtocolLimit.value) } : {}),
    ...(selectedNode.value ? { node: String(selectedNode.value.id), tab: detailSection.value } : {}),
  } }
  await (replace ? router.replace(location) : router.push(location))
}
async function applyFilters() { clearSelection(); offset.value = 0; await syncURL(); await refresh() }
async function resetFilters() { Object.assign(filters, { q: '', lifecycle: '', connector: '' }); await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await refresh() }
async function setSort(field: string) { const next = resolveSortField(field, nodeSortFields, 'id'); sortDirection.value = nextSortDirection(sortField.value, next, sortDirection.value, next === 'name' || next === 'region' ? 'asc' : 'desc'); sortField.value = next; offset.value = 0; clearSelection(); await syncURL(); await refresh() }
async function setDensity(value: 'compact' | 'comfortable') { density.value = value; await syncURL(true) }
async function selectNode(node: AdminNodeListItem) {
  diagnosticsOpen.value = false
  detailLoadingID.value = node.id; error.value = ''
  detailError.value = ''; detailMessage.value = ''
  try {
    const detail = await fetchNode(node.id)
    nodeProtocolOffset.value = 0; nodeProtocolLimit.value = 25; selectedNode.value = detail; detailSection.value = 'overview'; await syncURL()
  } catch (e: any) { error.value = e?.response?.data?.message || '节点详情加载失败。' }
  finally { detailLoadingID.value = 0 }
}
async function closeDetail() { diagnosticsOpen.value = false; selectedNode.value = null; detailError.value = ''; detailMessage.value = ''; nodeProtocolOffset.value = 0; bbrState.value = null; await syncURL() }
async function removeNode(node: AdminNodeDetail) {
  if (!await confirmAction({
    title: '删除节点资产？',
    message: `将永久删除“${node.name}”以及 zboard 中由该节点承载的协议服务、证书、DNS 管理记录和运行状态。历史流量、任务与审计记录会保留；远端 Zero、证书文件和 DNS 服务商记录不会自动删除。`,
    confirmText: '确认删除',
    tone: 'danger',
  })) return
  deletingNode.value = true
  detailError.value = ''
  detailMessage.value = ''
  try {
    await deleteNode(node.id)
    if (pendingBBRNodeID.value === node.id) pendingBBRNodeID.value = 0
    await closeDetail()
    message.value = `节点“${node.name}”已从面板删除；远端 Zero 未被卸载。`
    await refresh()
  } catch (e: any) {
    detailError.value = e?.response?.data?.message || '节点删除失败。'
  } finally {
    deletingNode.value = false
  }
}
async function changeNodeProtocolPage(value: { offset: number; limit: number }) { nodeProtocolOffset.value = value.offset; nodeProtocolLimit.value = value.limit; await syncURL() }
function openCreate() { pendingBBRNodeID.value = 0; Object.assign(createForm, { name: '', region: '', address: '', remark: '', enable_bbr: false }); createErrors.clear(); createState.markClean(); createOpen.value = true }
async function create() {
  createForm.name = createForm.name.trim()
  const valid = await createErrors.applyValidation(collectFieldErrors({
    name: isBlank(createForm.name) && '请输入主机名称。',
  }), createFormElement, '请更正标记字段后再登记 VPS。')
  if (!valid) return
  saving.value = true
  try {
    const wantsBBR = createForm.enable_bbr
    const node = await createNode({ name: createForm.name, region: createForm.region, address: createForm.address, remark: createForm.remark })
    Object.assign(createForm, { name: '', region: '', address: '', remark: '', enable_bbr: false })
    createOpen.value = false; offset.value = 0; await refresh(); selectedNode.value = await fetchNode(node.id); await syncURL(true)
    pendingBBRNodeID.value = wantsBBR ? node.id : 0
    message.value = wantsBBR
      ? 'VPS 已登记；请完成 SSH 配置。验证通过后将继续 Zero 初始化，并尝试启用 BBR。'
      : 'VPS 已登记；请完成 SSH 配置。验证通过后将继续 Zero 初始化。'
    openSSH(selectedNode.value)
  } catch (e: any) { await createErrors.applyApiError(e, '节点登记失败，请检查表单内容。', createFormElement, nodeCreateFieldMap) } finally { saving.value = false }
}
function openEdit(node: any) { Object.assign(editForm, { id: node.id, name: node.name, region: node.region || '', address: node.address || '', remark: node.remark || '', lifecycle_status: node.lifecycle_status || 'active', is_enabled: Boolean(node.is_enabled) }); editErrors.clear(); editState.markClean(); editOpen.value = true }
async function saveNode() {
  editForm.name = editForm.name.trim()
  const valid = await editErrors.applyValidation(collectFieldErrors({
    name: isBlank(editForm.name) && '请输入主机名称。',
    lifecycle_status: !isOneOf(editForm.lifecycle_status, ['active', 'maintenance', 'retired'] as const) && '请选择有效的生命周期。',
  }), editFormElement, '请更正标记字段后再保存 VPS。')
  if (!valid) return
  saving.value = true
  try { await updateNode(editForm.id, { name: editForm.name, region: editForm.region, address: editForm.address, remark: editForm.remark, lifecycle_status: editForm.lifecycle_status, is_enabled: editForm.lifecycle_status === 'active' && editForm.is_enabled }); editOpen.value = false; message.value = '节点资产已更新。'; await refresh() } catch (e: any) { await editErrors.applyApiError(e, '节点更新失败，请检查表单内容。', editFormElement, nodeEditFieldMap) } finally { saving.value = false }
}
function openSSH(node: any) { const privilegeMode = node.ssh_privilege_mode || 'none'; const authMethod = node.ssh_auth_method || 'password'; Object.assign(sshForm, { node_id: node.id, ssh_host: node.ssh_host || '', ssh_port: node.ssh_port || 22, ssh_user: node.ssh_user || 'root', ssh_auth_method: authMethod, original_auth_method: authMethod, ssh_password: '', ssh_private_key: '', ssh_private_key_passphrase: '', clearPassphrase: false, hasCredential: Boolean(node.ssh_configured), ssh_privilege_mode: privilegeMode, ssh_privilege_password: '', hasPrivilegePassword: Boolean(node.ssh_privilege_password_configured), passwordlessSudo: privilegeMode === 'sudo' && !node.ssh_privilege_password_configured }); sshErrors.clear(); sshState.markClean(); sshOpen.value = true }
function openTerminal(node: any) { terminalNode.value = node; terminalOpen.value = true }
async function runOptionalBBRInitialization(nodeID: number) {
  bbrBusy.value = true
  try {
    const snapshot = await enableNodeBBR(nodeID)
    if (selectedNode.value?.id === nodeID) {
      bbrState.value = snapshot.bbr
      detailMessage.value = 'BBR 已启用并完成状态复核；Zero 初始化不受影响。'
    }
  } catch (e: any) {
    const warning = e?.response?.data?.message || 'BBR 初始化失败，可稍后重试。'
    if (selectedNode.value?.id === nodeID) detailError.value = `BBR 未完成：${warning} Zero 初始化不受影响。`
  } finally { bbrBusy.value = false }
}
async function runNodeOnboardingAfterSSHSave(nodeID: number) {
  testingNode.value = nodeID
  detailError.value = ''
  detailMessage.value = ''
  try {
    const result = await testNodeSSH(nodeID)
    message.value = `SSH 验证成功（${result.latency_ms || 0}ms），正在准备 Zero 初始化…`
    await refresh()
    if (selectedNode.value?.id === nodeID) selectedNode.value = await fetchNode(nodeID)
  } catch (e: any) {
    detailError.value = e?.response?.data?.message || 'SSH 配置已保存，但验证失败；修正 SSH 后再继续 Zero 初始化。'
    message.value = 'VPS 已登记，但 SSH 尚未验证，Zero 初始化未开始。'
    return
  } finally { testingNode.value = 0 }

  const enableBBRAfterBootstrap = pendingBBRNodeID.value === nodeID
  pendingBBRNodeID.value = 0
  if (selectedNode.value?.id === nodeID) {
    detailSection.value = 'kernel'
    await syncURL(true)
    await Promise.all([loadKernel(nodeID), loadLatestRelease()])
  }
  message.value = enableBBRAfterBootstrap
    ? 'SSH 已验证；请继续安装 Zero。首次安装会自动生成并激活连接凭证，无需提前创建；BBR 将独立尝试启用。'
    : 'SSH 已验证；请继续安装 Zero 完成节点接入。首次安装会自动生成并激活连接凭证，无需提前创建。'
  if (enableBBRAfterBootstrap) void runOptionalBBRInitialization(nodeID)
}
async function saveSSH() {
  sshForm.ssh_host = sshForm.ssh_host.trim()
  sshForm.ssh_user = sshForm.ssh_user.trim()
  const missingCredential = requiresSSHCredential.value && (sshForm.ssh_auth_method === 'password' ? sshForm.ssh_password === '' : isBlank(sshForm.ssh_private_key))
  const valid = await sshErrors.applyValidation(collectFieldErrors({
    ssh_host: isBlank(sshForm.ssh_host) && '请输入 SSH 主机。',
    ssh_port: !isIntegerInRange(sshForm.ssh_port, 1, 65535) && '端口必须为 1–65535 之间的整数。',
    ssh_user: isBlank(sshForm.ssh_user) && '请输入 SSH 用户。',
    ssh_auth_method: !isOneOf(sshForm.ssh_auth_method, ['password', 'private_key'] as const) && '请选择密码或私钥认证。',
    ssh_password: missingCredential && sshForm.ssh_auth_method === 'password' && (sshForm.hasCredential ? '切换认证方式时必须提供新的登录密码。' : '请输入 SSH 登录密码。'),
    ssh_private_key: missingCredential && sshForm.ssh_auth_method === 'private_key' && (sshForm.hasCredential ? '切换认证方式时必须提供新的 SSH 私钥。' : '请输入 SSH 私钥。'),
    ssh_privilege_mode: !isOneOf(sshForm.ssh_privilege_mode, ['none', 'sudo', 'su'] as const) && '请选择有效的系统提权方式。',
    ssh_privilege_password: sshForm.ssh_privilege_mode === 'su' && !sshForm.hasPrivilegePassword && sshForm.ssh_privilege_password === '' && '使用 su 提权时必须提供 root 密码。',
  }), sshFormElement, '请更正标记字段后再保存 SSH 配置。')
  if (!valid) return
  saving.value = true
  try {
    const payload: any = { ssh_host: sshForm.ssh_host, ssh_port: sshForm.ssh_port, ssh_user: sshForm.ssh_user, ssh_auth_method: sshForm.ssh_auth_method, ssh_privilege_mode: sshForm.ssh_privilege_mode }
    if (sshForm.ssh_auth_method === 'password' && sshForm.ssh_password) payload.ssh_password = sshForm.ssh_password
    if (sshForm.ssh_auth_method === 'private_key' && sshForm.ssh_private_key) payload.ssh_private_key = sshForm.ssh_private_key
    if (sshForm.ssh_auth_method === 'private_key' && (sshForm.ssh_private_key_passphrase || sshForm.clearPassphrase)) payload.ssh_private_key_passphrase = sshForm.clearPassphrase ? '' : sshForm.ssh_private_key_passphrase
    if (sshForm.ssh_privilege_mode === 'none' || (sshForm.ssh_privilege_mode === 'sudo' && sshForm.passwordlessSudo)) payload.ssh_privilege_password = ''
    else if (sshForm.ssh_privilege_password) payload.ssh_privilege_password = sshForm.ssh_privilege_password
    const nodeID = sshForm.node_id
    await updateNodeSSH(nodeID, payload)
    sshOpen.value = false
    await refresh()
    await runNodeOnboardingAfterSSHSave(nodeID)
  } catch (e: any) { await sshErrors.applyApiError(e, 'SSH 配置保存失败，请检查表单内容。', sshFormElement, sshFieldMap) } finally { saving.value = false }
}
async function testSSH(id: number) { testingNode.value = id; detailError.value = ''; detailMessage.value = ''; try { const result = await testNodeSSH(id); detailMessage.value = `SSH 验证成功，耗时 ${result.latency_ms || 0}ms；主机身份已自动校验。`; await refresh(); if (selectedNode.value?.id === id) selectedNode.value = await fetchNode(id); if (detailSection.value === 'kernel') await loadBBR(id) } catch (e: any) { detailError.value = e?.response?.data?.message || 'SSH 验证失败。' } finally { testingNode.value = 0 } }
async function resetSSHHostKey(node: any) { if (!await confirmAction({ title: '重新信任主机', message: '仅当 VPS 已重装或主机密钥已确认更换时继续。重置后，下一次 SSH 连接会自动登记新的主机身份。', confirmText: '清除旧身份', tone: 'danger' })) return; detailError.value = ''; detailMessage.value = ''; try { await resetNodeSSHHostKey(node.id); detailMessage.value = '已清除旧主机身份；下一次 SSH 连接将自动重新登记。'; bbrState.value = null; await refresh() } catch (e: any) { detailError.value = e?.response?.data?.message || '重新信任主机失败。' } }
async function refreshSelectedNodeAfterCredentialChange(nodeID: number, patch: Partial<AdminNodeDetail>) {
  if (selectedNode.value?.id === nodeID) Object.assign(selectedNode.value, patch)
  await refresh()
  if (selectedNode.value?.id !== nodeID) return
  try { selectedNode.value = await fetchNode(nodeID) } catch { /* Keep the successful response patch if detail refresh is temporarily unavailable. */ }
}
async function closeSecretModal() {
  const nodeID = secretModal.nodeID
  Object.assign(secretModal, { title: '', value: '', copied: false, nodeID: 0 })
  if (!nodeID || selectedNode.value?.id !== nodeID) return
  try { selectedNode.value = await fetchNode(nodeID) } catch { /* The successful response patch already keeps the drawer accurate. */ }
}
async function rotateConnector(node: any) { if (node.node_credential_prefix && !await confirmAction({ title: '轮换 Zero 凭证', message: '轮换后旧 Zero 连接凭证会立即失效，节点需要应用新配置。', confirmText: '确认轮换', tone: 'danger' })) return; detailError.value = ''; try { const result = await rotateNodeConnectorCredential(node.id); const config = JSON.stringify({ push: { url: window.location.origin, node_id: result.node_id, api_key: result.api_key, heartbeat_interval_seconds: 30, pull_commands: true, command_poll_interval_seconds: 10 } }, null, 2); Object.assign(secretModal, { title: 'Zero 主动连接配置', value: config, copied: false, nodeID: node.id }); await refreshSelectedNodeAfterCredentialChange(node.id, { node_credential_prefix: result.api_key_prefix, node_credential_revoked_at: undefined }) } catch (e: any) { detailError.value = e?.response?.data?.message || 'Zero 连接凭证操作失败。' } }
async function revokeConnector(node: any) { if (!await confirmAction({ title: '吊销 Zero 凭证', message: '吊销后节点将无法继续向面板发送心跳或领取命令。', confirmText: '确认吊销', tone: 'danger' })) return; detailError.value = ''; detailMessage.value = ''; try { await revokeNodeConnectorCredential(node.id); detailMessage.value = 'Zero 连接凭证已吊销。'; await refreshSelectedNodeAfterCredentialChange(node.id, { node_credential_revoked_at: new Date().toISOString(), connector_online: false }) } catch (e: any) { detailError.value = e?.response?.data?.message || '吊销失败。' } }
async function rotateReport(node: any) { if (node.traffic_secret_prefix && !await confirmAction({ title: '轮换流量凭证', message: '轮换后旧流量上报凭证会立即失效。', confirmText: '确认轮换', tone: 'danger' })) return; detailError.value = ''; try { const result = await rotateNodeReportCredential(node.id); Object.assign(secretModal, { title: '流量上报凭证', value: result.secret, copied: false, nodeID: node.id }); await refreshSelectedNodeAfterCredentialChange(node.id, { traffic_secret_prefix: result.secret_prefix, traffic_secret_revoked_at: undefined }) } catch (e: any) { detailError.value = e?.response?.data?.message || '流量上报凭证操作失败。' } }
async function revokeReport(node: any) { if (!await confirmAction({ title: '吊销流量凭证', message: '吊销后节点将无法继续提交可信流量记录。', confirmText: '确认吊销', tone: 'danger' })) return; detailError.value = ''; detailMessage.value = ''; try { await revokeNodeReportCredential(node.id); detailMessage.value = '流量上报凭证已吊销。'; await refreshSelectedNodeAfterCredentialChange(node.id, { traffic_secret_revoked_at: new Date().toISOString() }) } catch (e: any) { detailError.value = e?.response?.data?.message || '吊销失败。' } }
async function copySecret() { try { await navigator.clipboard.writeText(secretModal.value); secretModal.copied = true } catch { secretModal.copied = false } }

watch([() => selectedNode.value?.id, detailSection, nodeProtocolOffset, nodeProtocolLimit], ([id, section], [previousID]) => {
  if (id !== previousID) {
    diagnosticsOpen.value = false
    kernelState.value = null
    kernelOperations.value = []
    bbrState.value = null
    nodeEndpoints.value = []
    nodeLoad.value = null
    nodeLoadError.value = ''
    nodeProtocolTotal.value = 0
  }
  if (section === 'kernel') { void loadKernel(id); void loadLatestRelease() } else kernelRequests.invalidate()
  if (section === 'protocols') void loadNodeProtocols(id); else { protocolRequests.invalidate(); nodeProtocolsLoading.value = false }
}, { immediate: true })
watch(detailSection, (section) => { if (selectedNode.value && String(route.query.tab || 'overview') !== section) void syncURL() })
watch([zeroReleases, () => kernelState.value?.libc], () => {
  if (!selectedRelease.value || !releaseCompatible(selectedRelease.value)) {
    selectedReleaseVersion.value = zeroReleases.value.find(releaseCompatible)?.version || ''
  }
})
watch(() => route.fullPath, async () => {
  const nextLimit = Number(route.query.limit)
  const resolvedLimit = allowedPageSizes.includes(nextLimit) ? nextLimit : 50
  const resolvedOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * resolvedLimit
  const nextFilters = {
    q: typeof route.query.q === 'string' ? route.query.q : '',
    lifecycle: typeof route.query.lifecycle === 'string' ? route.query.lifecycle : '',
    connector: typeof route.query.connector === 'string' ? route.query.connector : '',
  }
  const nextSortField = resolveSortField(route.query.sort, nodeSortFields, 'id')
  const nextSortDirection = resolveSortDirection(route.query.direction, 'desc')
  const nextDensity = resolveTableDensity(route.query.density)
  const filterChanged = nextFilters.q !== filters.q || nextFilters.lifecycle !== filters.lifecycle || nextFilters.connector !== filters.connector
  const sortChanged = nextSortField !== sortField.value || nextSortDirection !== sortDirection.value
  const listChanged = resolvedLimit !== limit.value || resolvedOffset !== offset.value || filterChanged || sortChanged
  density.value = nextDensity
  if (listChanged) { if (filterChanged || sortChanged) clearSelection(); limit.value = resolvedLimit; offset.value = resolvedOffset; sortField.value = nextSortField; sortDirection.value = nextSortDirection; Object.assign(filters, nextFilters); await refresh() }
  const nextProtocolLimitValue = Number(route.query.protocol_limit)
  const nextProtocolLimit = allowedPageSizes.includes(nextProtocolLimitValue) ? nextProtocolLimitValue : 25
  const nextProtocolOffset = (Math.max(1, Number(route.query.protocol_page) || 1) - 1) * nextProtocolLimit
  if (nextProtocolLimit !== nodeProtocolLimit.value || nextProtocolOffset !== nodeProtocolOffset.value) { nodeProtocolLimit.value = nextProtocolLimit; nodeProtocolOffset.value = nextProtocolOffset }
  const nodeID = Number(route.query.node) || 0
  if (!nodeID) selectedNode.value = null
  else if (selectedNode.value?.id !== nodeID) {
    const localNode = nodes.value.find(node => node.id === nodeID)
    if (localNode) {
      try { selectedNode.value = await fetchNode(localNode.id) } catch { selectedNode.value = null }
    }
    else {
      try { selectedNode.value = await fetchNode(nodeID) } catch { selectedNode.value = null }
    }
  }
  const tab = typeof route.query.tab === 'string' ? route.query.tab : 'overview'
  if (['overview', 'kernel', 'protocols', 'credentials'].includes(tab)) detailSection.value = tab as typeof detailSection.value
})
onMounted(async () => {
  await refresh()
  const nodeID = Number(route.query.node) || 0
  if (nodeID) {
    try { selectedNode.value = await fetchNode(nodeID) } catch { selectedNode.value = null }
  }
  const tab = typeof route.query.tab === 'string' ? route.query.tab : 'overview'
  if (['overview', 'kernel', 'protocols', 'credentials'].includes(tab)) detailSection.value = tab as typeof detailSection.value
})
onBeforeUnmount(stopKernelPolling)
</script>

<style scoped>
:deep(.workbench-table th),:deep(.workbench-table td){height:40px;padding-block:7px}:deep(.workbench-table tbody tr.selected){background:var(--primary-soft)}.numeric-column{text-align:right!important;font-variant-numeric:tabular-nums}.node-detail{min-width:0}.detail-drawer-body .node-summary{display:grid;grid-template-columns:minmax(0,1fr);align-items:start;gap:14px;padding:0 0 16px;border:0;border-bottom:1px solid var(--line);border-radius:0}.detail-drawer-body .node-summary-main{min-width:0;align-items:flex-start}.detail-drawer-body .title-line{flex-wrap:wrap}.detail-drawer-body .node-actions{width:100%;justify-content:flex-start}.detail-drawer-body .node-health-strip{border:1px solid var(--line);border-radius:var(--radius-md);overflow:hidden}.detail-drawer-body .panel{border-radius:var(--radius-md)}
.page-alert{margin-bottom:14px}.count-label{color:var(--muted);font-size:11px}.node-layout{display:grid;grid-template-columns:300px minmax(0,1fr);align-items:start;gap:16px}.node-list-panel{position:sticky;top:82px;overflow:hidden}.node-list{display:grid}.node-list>button{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:15px 16px;text-align:left;border:0;border-bottom:1px solid var(--line);background:var(--surface)}.node-list>button:hover,.node-list>button.active{background:var(--surface-selected)}.node-list>button.active{box-shadow:inset 3px 0 var(--primary)}.node-list strong{font-size:12px}.node-list p,.node-list small{margin:3px 0 0;color:var(--muted);font-size:9px}.node-list small{color:var(--primary)}.node-state{width:9px;height:9px;border-radius:50%;background:var(--warning-bright);box-shadow:0 0 0 4px var(--warning-soft)}.node-state.online{background:var(--success-bright);box-shadow:0 0 0 4px var(--success-soft)}.node-state.disabled{background:var(--subtle);box-shadow:0 0 0 4px var(--surface-neutral)}.node-detail{min-width:0}.node-summary{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:20px}.node-summary-main{display:flex;align-items:center;gap:14px}.node-avatar{width:46px;height:46px;display:grid;place-items:center;border-radius:12px;color:var(--primary);background:var(--primary-soft);font-size:21px}.title-line{display:flex;align-items:center;gap:10px}.title-line h2{margin:0;font-size:19px}.node-summary-main p{margin:5px 0 2px;color:var(--muted);font-size:11px}.node-summary-main small{color:var(--subtle);font-size:9px}.node-actions{display:flex;flex-wrap:wrap;gap:7px}.readiness-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.readiness-grid article{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:11px;padding:15px;border:1px solid var(--line);border-radius:var(--radius-md);background:var(--surface)}.readiness-grid article>span{width:34px;height:34px;display:grid;place-items:center;border-radius:9px;color:var(--primary);background:var(--primary-soft)}.readiness-grid strong{font-size:11px}.readiness-grid p{margin:3px 0 0;color:var(--muted);font-size:9px}.action-card{display:grid;gap:14px}.action-card p{margin:0;color:var(--muted);font-size:11px}.action-card>div{display:flex;gap:7px}.node-empty{min-height:420px;display:grid;place-items:center}.secret-card{display:grid;gap:14px}.secret-card textarea{font-family:Consolas,monospace;font-size:11px}.secret-card .button{justify-self:start}@media(max-width:1100px){.node-layout{grid-template-columns:1fr}.node-list-panel{position:static}.node-list{grid-template-columns:repeat(2,1fr)}}@media(max-width:720px){.node-list,.readiness-grid{grid-template-columns:1fr}.node-summary{align-items:flex-start;flex-direction:column}.node-actions{display:grid;width:100%}}
.kernel-panel{overflow:hidden}.kernel-body{display:grid;gap:16px}.kernel-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1px;overflow:hidden;border:1px solid var(--line);border-radius:10px;background:var(--line)}.kernel-facts>div{display:grid;gap:5px;padding:13px;background:var(--surface)}.kernel-facts span{color:var(--muted);font-size:9px}.kernel-facts strong{font-size:11px;overflow-wrap:anywhere}.kernel-error{display:flex;align-items:flex-start;gap:8px;margin:0;padding:11px;border-radius:8px;color:var(--danger);background:var(--danger-soft);font-size:10px;overflow-wrap:anywhere}.kernel-release-picker{display:grid;grid-template-columns:minmax(110px,160px) minmax(220px,360px);align-items:center;gap:7px 12px;padding:13px;border:1px solid var(--line);border-radius:10px;background:var(--surface-neutral)}.kernel-release-picker label{font-size:10px;font-weight:700}.kernel-release-picker small{grid-column:2;color:var(--muted);font-size:9px}.kernel-actions{display:flex;align-items:center;flex-wrap:wrap;gap:8px}.kernel-actions small{color:var(--muted);font-size:9px}.kernel-history{display:grid;border-top:1px solid var(--line)}.kernel-history>div{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:10px 2px}.kernel-history>div+div{border-top:1px solid var(--line)}.kernel-history strong{font-size:10px}.kernel-history p{margin:3px 0 0;color:var(--muted);font-size:9px;overflow-wrap:anywhere}.kernel-history time{color:var(--subtle);font-size:9px}.operation-dot{width:8px;height:8px;border-radius:50%;background:var(--warning)}.operation-dot.succeeded{background:var(--success)}.operation-dot.failed{background:var(--danger)}@media(max-width:720px){.kernel-facts{grid-template-columns:repeat(2,minmax(0,1fr))}.kernel-release-picker{grid-template-columns:1fr}.kernel-release-picker small{grid-column:1}.kernel-history>div{grid-template-columns:auto minmax(0,1fr)}.kernel-history time{grid-column:2}}
.detail-tabs{display:flex;gap:18px;overflow-x:auto;border-bottom:1px solid var(--line)}.detail-tabs button{min-height:42px;display:inline-flex;align-items:center;gap:7px;padding:0 2px;border:0;border-bottom:2px solid transparent;color:var(--muted);background:transparent;font-size:12px;font-weight:650;white-space:nowrap}.detail-tabs button:hover{color:var(--text)}.detail-tabs button.active{color:var(--primary);border-bottom-color:var(--primary)}.node-health-strip{display:grid;border-block:1px solid var(--line);background:var(--surface)}.node-health-strip>*+*{border-top:1px solid var(--line)}.credential-workspace{overflow:hidden}.credential-row{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:13px;padding:17px 20px}.credential-row+.credential-row{border-top:1px solid var(--line)}.credential-icon{width:34px;height:34px;display:grid;place-items:center;color:var(--primary);background:var(--primary-soft);border-radius:9px}.credential-row strong{font-size:12px}.credential-row p{margin:3px 0 0;color:var(--muted);font-size:10px}.credential-actions{display:flex;gap:7px}@media(max-width:720px){.credential-row{grid-template-columns:auto minmax(0,1fr)}.credential-actions{grid-column:2;flex-wrap:wrap}}
.node-load-summary{display:grid;gap:14px;padding:17px 20px}.node-load-summary>header{display:flex;align-items:center;justify-content:space-between;gap:12px}.node-load-summary header p{margin:3px 0 0;color:var(--muted);font-size:9px}.node-load-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1px;overflow:hidden;border:1px solid var(--line);border-radius:10px;background:var(--line)}.node-load-grid>div{display:grid;gap:5px;padding:13px;background:var(--surface)}.node-load-grid span,.node-load-grid small{color:var(--muted);font-size:9px}.node-load-grid strong{font-size:12px}@media(max-width:720px){.node-load-summary>header{align-items:flex-start;flex-direction:column}.node-load-grid{grid-template-columns:1fr}}
.node-list>button{color:var(--text)}.node-list>button:hover,.node-list>button.active{background:var(--primary-soft)}
.node-protocols{overflow:hidden}.node-protocols :deep(.data-table-shell){border:0;border-radius:0}.node-protocols :deep(.p-inputnumber){min-width:110px;max-width:160px}
</style>