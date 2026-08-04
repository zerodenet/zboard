from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text(encoding='utf-8')
    if old not in text:
        raise SystemExit(f'missing expected text in {path}: {old[:120]!r}')
    file.write_text(text.replace(old, new, 1), encoding='utf-8')


replace_once(
    'backend/internal/handler/protocol_endpoint_effects.go',
    '''type protocolEndpointMutationResponse struct {
\tProtocolEndpoint     model.ProtocolEndpoint                   `json:"protocol_endpoint"`
\tNodeGroupMemberships []protocolEndpointNodeGroupMembership    `json:"node_group_memberships"`
\tNodeGroupMembership  *protocolEndpointNodeGroupMutationResult `json:"node_group_membership,omitempty"`
\tprotocolEndpointChangeEffects
}''',
    '''type protocolEndpointMutationResponse struct {
\tProtocolEndpoint     model.ProtocolEndpoint                   `json:"protocol_endpoint"`
\tNodeGroupMemberships []protocolEndpointNodeGroupMembership    `json:"node_group_memberships"`
\tNodeGroupMembership  *protocolEndpointNodeGroupMutationResult `json:"node_group_membership,omitempty"`
\tTiming               protocolEndpointMutationTiming           `json:"timing"`
\tprotocolEndpointChangeEffects
}''',
)

handlers = Path('backend/internal/handler/handlers.go')
text = handlers.read_text(encoding='utf-8')
start = text.index('func (h *handlers) saveProtocolEndpoint(')
end = text.index('\nfunc (h *handlers) ProtocolEndpointDeployHandler', start)
segment = text[start:end]
segment = segment.replace(
    '''func (h *handlers) saveProtocolEndpoint(w http.ResponseWriter, r *http.Request, endpointID uint) {
\tclaims, err := h.requireAdmin(w, r)''',
    '''func (h *handlers) saveProtocolEndpoint(w http.ResponseWriter, r *http.Request, endpointID uint) {
\trequestStartedAt := time.Now()
\tclaims, err := h.requireAdmin(w, r)''',
    1,
)
segment = segment.replace(
    '''\tvar membershipMutation *protocolEndpointNodeGroupMutationResult
\tremovedMemberships := protocolEndpointNodeGroupRemovalSet(membershipChanges)
\tif err := h.db.Transaction(func(tx *gorm.DB) error {''',
    '''\tvar membershipMutation *protocolEndpointNodeGroupMutationResult
\tremovedMemberships := protocolEndpointNodeGroupRemovalSet(membershipChanges)
\tvalidationFinishedAt := time.Now()
\ttransactionErr := h.db.Transaction(func(tx *gorm.DB) error {''',
    1,
)
segment = segment.replace(
    '''\t\treturn createAuditLog(tx, claims, action, fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), detail)
\t}); err != nil {
\t\tvar conflict *protocolEndpointNodeGroupRevisionConflictError''',
    '''\t\treturn createAuditLog(tx, claims, action, fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), detail)
\t})
\ttransactionFinishedAt := time.Now()
\tif err := transactionErr; err != nil {
\t\tvar conflict *protocolEndpointNodeGroupRevisionConflictError''',
    1,
)
old_tail = '''\tif membershipMutation != nil {
\t\tfor index := range membershipMutation.ReconcileTasks {
\t\t\t_ = h.startPersistedAdminTask(&membershipMutation.ReconcileTasks[index])
\t\t}
\t}
\tmemberships, err := loadProtocolEndpointNodeGroupMemberships(h.db, endpoint.ID)
\tif err != nil {
\t\tServerError(w, err)
\t\treturn
\t}
\tmembershipPublishedNodeIDs := []uint(nil)
\tif membershipMutation != nil {
\t\tmembershipPublishedNodeIDs = membershipMutation.AffectedNodeIDs
\t}
\tfor _, affectedNodeID := range protocolEndpointDirectPublishNodeIDs(changeEffects.AffectedNodeIDs, membershipPublishedNodeIDs) {
\t\th.scheduleNodeConfigPublish(affectedNodeID, endpoint.ID, claims.UserID)
\t}
\tOK(w, protocolEndpointMutationResponse{
\t\tProtocolEndpoint:              endpoint,
\t\tprotocolEndpointChangeEffects: changeEffects,
\t\tNodeGroupMemberships:          memberships,
\t\tNodeGroupMembership:           membershipMutation,
\t})'''
new_tail = '''\tif membershipMutation != nil {
\t\tfor index := range membershipMutation.ReconcileTasks {
\t\t\t_ = h.startPersistedAdminTask(&membershipMutation.ReconcileTasks[index])
\t\t}
\t}
\tmembershipPublishedNodeIDs := []uint(nil)
\tif membershipMutation != nil {
\t\tmembershipPublishedNodeIDs = membershipMutation.AffectedNodeIDs
\t}
\tfor _, affectedNodeID := range protocolEndpointDirectPublishNodeIDs(changeEffects.AffectedNodeIDs, membershipPublishedNodeIDs) {
\t\th.scheduleNodeConfigPublish(affectedNodeID, endpoint.ID, claims.UserID)
\t}
\ttaskEnqueueFinishedAt := time.Now()
\tmemberships, err := loadProtocolEndpointNodeGroupMemberships(h.db, endpoint.ID)
\tif err != nil {
\t\tServerError(w, err)
\t\treturn
\t}
\tresponseFinishedAt := time.Now()
\ttiming := newProtocolEndpointMutationTiming(requestStartedAt, validationFinishedAt, transactionFinishedAt, taskEnqueueFinishedAt, responseFinishedAt)
\tw.Header().Set("Server-Timing", fmt.Sprintf("validation;dur=%d, transaction;dur=%d, task_enqueue;dur=%d, response_preparation;dur=%d", timing.ValidationMS, timing.TransactionMS, timing.TaskEnqueueMS, timing.ResponsePreparationMS))
\tOK(w, protocolEndpointMutationResponse{
\t\tProtocolEndpoint:              endpoint,
\t\tprotocolEndpointChangeEffects: changeEffects,
\t\tNodeGroupMemberships:          memberships,
\t\tNodeGroupMembership:           membershipMutation,
\t\tTiming:                        timing,
\t})'''
if old_tail not in segment:
    raise SystemExit('missing protocol handler tail')
segment = segment.replace(old_tail, new_tail, 1)
handlers.write_text(text[:start] + segment + text[end:], encoding='utf-8')

replace_once(
    'frontend/src/api/client.ts',
    '''export interface ProtocolEndpointMutationResult {
\tprotocol_endpoint: Record<string, unknown>
\teffect: ProtocolEndpointChangeEffect
\teffects: ProtocolEndpointChangeEffect[]
\tpublish_status: 'queued' | 'not_required'
\taffected_node_ids?: number[]
\tnode_group_memberships: ProtocolEndpointNodeGroupMembership[]
\tnode_group_membership?: ProtocolEndpointNodeGroupMutationResult
}''',
    '''export interface ProtocolEndpointMutationTiming {
\tvalidation_ms: number
\ttransaction_ms: number
\ttask_enqueue_ms: number
\tresponse_preparation_ms: number
\tserver_total_ms: number
}

export interface ProtocolEndpointMutationResult {
\tprotocol_endpoint: Record<string, unknown>
\teffect: ProtocolEndpointChangeEffect
\teffects: ProtocolEndpointChangeEffect[]
\tpublish_status: 'queued' | 'not_required'
\taffected_node_ids?: number[]
\tnode_group_memberships: ProtocolEndpointNodeGroupMembership[]
\tnode_group_membership?: ProtocolEndpointNodeGroupMutationResult
\ttiming: ProtocolEndpointMutationTiming
}''',
)

replace_once(
    'frontend/src/views/Protocols.vue',
    '''import { protocolEndpointMutationMessage } from '../utils/protocolEndpointEffects'
import { buildProtocolNodeGroupMembershipChanges } from '../utils/protocolNodeGroupMembership' ''',
    '''import { protocolEndpointMutationMessage } from '../utils/protocolEndpointEffects'
import { formatProtocolSaveTiming, summarizeProtocolSaveTiming } from '../utils/protocolSaveTiming'
import { buildProtocolNodeGroupMembershipChanges } from '../utils/protocolNodeGroupMembership' ''',
)

protocols = Path('frontend/src/views/Protocols.vue')
text = protocols.read_text(encoding='utf-8')
old_save = '''  saving.value = true; editorError.value = ''; editorErrors.clear(); error.value = ''; message.value = ''
  try {
    const creatingCopy = Boolean(copySourceID.value)
    const payload = { node_id: form.node_id, name: form.name, protocol: form.protocol, address: form.address, port: form.port, public_port: form.public_port, multiplier_milli: form.multiplier_milli, sort_order: form.sort_order, parent_protocol_id: form.parent_protocol_id || null, managed_certificate_id: form.managed_certificate_id || null, is_active: Boolean(form.is_active), config: form.config, client_config: form.client_config, optional_config: form.optional_config || '{}', tags: form.tags || '[]', node_group_membership_changes: membershipChanges }
    const result = form.id ? await updateProtocolEndpoint(form.id, payload) : await createProtocolEndpoint(payload)
    for (const task of result.node_group_membership?.reconcile_tasks || []) trackAdminTask(task)
    editorOpen.value = false
    copySourceID.value = 0
    message.value = protocolEndpointMutationMessage(result, creatingCopy)
    await refresh()
  }'''
new_save = '''  saving.value = true; editorError.value = ''; editorErrors.clear(); error.value = ''; message.value = ''
  const saveStartedAt = performance.now()
  try {
    const creatingCopy = Boolean(copySourceID.value)
    const payload = { node_id: form.node_id, name: form.name, protocol: form.protocol, address: form.address, port: form.port, public_port: form.public_port, multiplier_milli: form.multiplier_milli, sort_order: form.sort_order, parent_protocol_id: form.parent_protocol_id || null, managed_certificate_id: form.managed_certificate_id || null, is_active: Boolean(form.is_active), config: form.config, client_config: form.client_config, optional_config: form.optional_config || '{}', tags: form.tags || '[]', node_group_membership_changes: membershipChanges }
    const requestStartedAt = performance.now()
    const result = form.id ? await updateProtocolEndpoint(form.id, payload) : await createProtocolEndpoint(payload)
    const requestMS = performance.now() - requestStartedAt
    for (const task of result.node_group_membership?.reconcile_tasks || []) trackAdminTask(task)
    editorOpen.value = false
    copySourceID.value = 0
    const refreshStartedAt = performance.now()
    await refresh()
    const refreshMS = performance.now() - refreshStartedAt
    const timing = summarizeProtocolSaveTiming(result.timing, requestMS, refreshMS, performance.now() - saveStartedAt)
    message.value = `${protocolEndpointMutationMessage(result, creatingCopy)} ${formatProtocolSaveTiming(timing)}`
    console.info('protocol endpoint save timing', { endpoint_id: result.protocol_endpoint?.id, effect: result.effect, publish_status: result.publish_status, ...timing })
  }'''
if old_save not in text:
    raise SystemExit('missing protocol save block')
protocols.write_text(text.replace(old_save, new_save, 1), encoding='utf-8')

replace_once(
    'backend/api/openapi.yaml',
    '''    ProtocolEndpointMutationResult:
      type: object
      required: [protocol_endpoint, effect, effects, publish_status, node_group_memberships]''',
    '''    ProtocolEndpointMutationTiming:
      type: object
      required: [validation_ms, transaction_ms, task_enqueue_ms, response_preparation_ms, server_total_ms]
      description: Server-side save phase durations in milliseconds. Task enqueue measures scheduling only and never waits for node publication.
      properties:
        validation_ms: { type: integer, format: int64, minimum: 0 }
        transaction_ms: { type: integer, format: int64, minimum: 0 }
        task_enqueue_ms: { type: integer, format: int64, minimum: 0 }
        response_preparation_ms: { type: integer, format: int64, minimum: 0 }
        server_total_ms: { type: integer, format: int64, minimum: 0 }
    ProtocolEndpointMutationResult:
      type: object
      required: [protocol_endpoint, effect, effects, publish_status, node_group_memberships, timing]''',
)
replace_once(
    'backend/api/openapi.yaml',
    '''        node_group_membership:
          $ref: "#/components/schemas/ProtocolEndpointNodeGroupMutationResult"
    ProtocolEndpointSelectionSnapshot:''',
    '''        node_group_membership:
          $ref: "#/components/schemas/ProtocolEndpointNodeGroupMutationResult"
        timing: { $ref: "#/components/schemas/ProtocolEndpointMutationTiming" }
    ProtocolEndpointSelectionSnapshot:''',
)

model = Path('docs/data-model.md')
text = model.read_text(encoding='utf-8')
anchor = '业务交付顺序的变化不会触发节点运行配置发布。'
if anchor in text and '协议保存耗时' not in text:
    text = text.replace(anchor, anchor + '\n\n协议保存响应同时返回管理员可观测的阶段耗时：校验与读取、数据库事务、持久化任务入队、响应准备和服务端总耗时。前端另外记录完整请求与列表刷新耗时；任务入队耗时不包含异步节点发布执行时间。', 1)
model.write_text(text, encoding='utf-8')
