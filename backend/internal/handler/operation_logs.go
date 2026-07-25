package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type protocolMultiplierWriteReq struct {
	MultiplierMilli int64 `json:"multiplier_milli"`
}

func (h *handlers) ProtocolEndpointMultiplierHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req protocolMultiplierWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.MultiplierMilli < 1 || req.MultiplierMilli > 100000 {
		BadRequest(w, "multiplier_milli must be between 1 and 100000 (1000 means 1x)")
		return
	}
	var endpoint model.ProtocolEndpoint
	var previous int64
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, id).Error; err != nil {
			return err
		}
		previous = endpoint.MultiplierMilli
		if previous == req.MultiplierMilli {
			return nil
		}
		if err := tx.Model(&endpoint).Update("multiplier_milli", req.MultiplierMilli).Error; err != nil {
			return err
		}
		endpoint.MultiplierMilli = req.MultiplierMilli
		return createAuditLog(tx, claims, "protocol_endpoint.multiplier.update", fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), fmt.Sprintf("from=%d to=%d", previous, req.MultiplierMilli))
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, endpoint)
}

type operationLogItem struct {
	ID                 uint       `json:"id"`
	Source             string     `json:"source"`
	Action             string     `json:"action"`
	Status             string     `json:"status"`
	TargetType         string     `json:"target_type"`
	TargetID           uint       `json:"target_id"`
	NodeID             uint       `json:"node_id,omitempty"`
	ProtocolEndpointID uint       `json:"protocol_endpoint_id,omitempty"`
	Summary            string     `json:"summary,omitempty"`
	HasOutput          bool       `json:"has_output"`
	HasError           bool       `json:"has_error"`
	Output             string     `json:"output,omitempty"`
	Error              string     `json:"error,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (h *handlers) OperationLogsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source != "" && source != "protocol_publish" && source != "node_kernel" && source != "task" {
		BadRequest(w, "invalid source")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "queued" && status != "running" && status != "succeeded" && status != "failed" {
		BadRequest(w, "invalid status")
		return
	}
	nodeID, err := optionalUintQuery(r, "node_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	endpointID, err := optionalUintQuery(r, "protocol_endpoint_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	window, err := parseHistoryWindow(r.URL.Query(), 30)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	cursor, err := decodeHistoryCursor(r.URL.Query().Get("cursor"), map[string]struct{}{
		"protocol_publish": {}, "node_kernel": {}, "task": {},
	})
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	legacyOffset := cursor == nil && offset > 0
	fetchLimit := limit + 1
	if legacyOffset {
		fetchLimit = offset + limit
	}
	items := make([]operationLogItem, 0, fetchLimit*3)
	total := int64(0)
	if source == "" || source == "protocol_publish" {
		query := applyHistoryWindow(h.db.Model(&model.ProtocolDeployment{}), "created_at", window)
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if nodeID != 0 {
			query = query.Where("node_id = ?", nodeID)
		}
		if endpointID != 0 {
			query = query.Where("protocol_endpoint_id = ?", endpointID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		total += count
		var records []model.ProtocolDeployment
		query = applyOperationHistoryCursor(query, "protocol_publish", cursor)
		if err := query.Order(operationHistoryOrder(cursor)).Limit(fetchLimit).Find(&records).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, record := range records {
			items = append(items, operationLogItem{
				ID: record.ID, Source: "protocol_publish", Action: "protocol.publish", Status: record.Status,
				TargetType: "protocol_endpoint", TargetID: record.ProtocolEndpointID, NodeID: record.NodeID, ProtocolEndpointID: record.ProtocolEndpointID,
				Summary: fmt.Sprintf("config revision %d", record.ConfigRevision), HasOutput: record.Output != "", HasError: record.Error != "",
				StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, CreatedAt: record.CreatedAt,
			})
		}
	}
	if endpointID == 0 && (source == "" || source == "node_kernel") {
		query := applyHistoryWindow(h.db.Model(&model.NodeOperation{}), "created_at", window)
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if nodeID != 0 {
			query = query.Where("node_id = ?", nodeID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		total += count
		var records []model.NodeOperation
		query = applyOperationHistoryCursor(query, "node_kernel", cursor)
		if err := query.Order(operationHistoryOrder(cursor)).Limit(fetchLimit).Find(&records).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, record := range records {
			items = append(items, operationLogItem{
				ID: record.ID, Source: "node_kernel", Action: "node.kernel." + record.OperationType, Status: record.Status,
				TargetType: "node", TargetID: record.NodeID, NodeID: record.NodeID,
				Summary: record.ResultSummary, HasError: record.Error != "", StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, CreatedAt: record.CreatedAt,
			})
		}
	}
	if nodeID == 0 && endpointID == 0 && (source == "" || source == "task") {
		query := applyHistoryWindow(h.db.Model(&model.Task{}), "created_at", window)
		if status != "" {
			query = query.Where("status = ?", operationTaskStatus(status))
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		total += count
		var records []model.Task
		query = applyOperationHistoryCursor(query, "task", cursor)
		if err := query.Order(operationHistoryOrder(cursor)).Limit(fetchLimit).Find(&records).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, record := range records {
			items = append(items, operationLogItem{
				ID: record.ID, Source: "task", Action: "task." + record.Type, Status: normalizeTaskStatus(record.Status),
				TargetType: "task", TargetID: record.ID, Summary: fmt.Sprintf("progress %d/%d; attempt %d/%d", record.Current, record.Total, record.Attempts, record.MaxAttempts),
				HasError: record.Errors != "", StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, CreatedAt: record.CreatedAt,
			})
		}
	}
	ascending := cursor != nil && cursor.Direction == historyDirectionNewer
	sort.SliceStable(items, func(i, j int) bool {
		if ascending {
			return operationLogComesBefore(items[j], items[i])
		}
		return operationLogComesBefore(items[i], items[j])
	})
	if legacyOffset {
		if offset >= len(items) {
			items = []operationLogItem{}
		} else {
			end := offset + limit
			if end > len(items) {
				end = len(items)
			}
			items = items[offset:end]
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	if ascending {
		reverseHistoryPage(items)
	}
	var nextCursor, previousCursor *string
	if len(items) > 0 {
		nextCursor, previousCursor, err = historyPageCursorValues(
			historyKey{At: items[0].CreatedAt, ID: items[0].ID, Source: items[0].Source},
			historyKey{At: items[len(items)-1].CreatedAt, ID: items[len(items)-1].ID, Source: items[len(items)-1].Source},
			cursor,
			hasMore,
		)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, cursorPagedData(items, total, limit, nextCursor, previousCursor))
}

func operationLogComesBefore(left, right operationLogItem) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		if left.Source == right.Source {
			return left.ID > right.ID
		}
		return left.Source < right.Source
	}
	return left.CreatedAt.After(right.CreatedAt)
}

func (h *handlers) OperationLogDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/operation-logs/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		BadRequest(w, "operation log source and id are required")
		return
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		BadRequest(w, "invalid operation log id")
		return
	}
	item := operationLogItem{ID: uint(id), Source: parts[0]}
	switch parts[0] {
	case "protocol_publish":
		var record model.ProtocolDeployment
		if err := h.db.First(&record, uint(id)).Error; err != nil {
			h.operationLogReadError(w, err)
			return
		}
		item.Action = "protocol.publish"
		item.Status = record.Status
		item.TargetType = "protocol_endpoint"
		item.TargetID = record.ProtocolEndpointID
		item.NodeID = record.NodeID
		item.ProtocolEndpointID = record.ProtocolEndpointID
		item.Summary = fmt.Sprintf("config revision %d", record.ConfigRevision)
		item.Output = record.Output
		item.Error = record.Error
		item.StartedAt = record.StartedAt
		item.FinishedAt = record.FinishedAt
		item.CreatedAt = record.CreatedAt
	case "node_kernel":
		var record model.NodeOperation
		if err := h.db.First(&record, uint(id)).Error; err != nil {
			h.operationLogReadError(w, err)
			return
		}
		item.Action = "node.kernel." + record.OperationType
		item.Status = record.Status
		item.TargetType = "node"
		item.TargetID = record.NodeID
		item.NodeID = record.NodeID
		item.Summary = record.ResultSummary
		item.Error = record.Error
		item.StartedAt = record.StartedAt
		item.FinishedAt = record.FinishedAt
		item.CreatedAt = record.CreatedAt
	case "task":
		var record model.Task
		if err := h.db.First(&record, uint(id)).Error; err != nil {
			h.operationLogReadError(w, err)
			return
		}
		item.Action = "task." + record.Type
		item.Status = normalizeTaskStatus(record.Status)
		item.TargetType = "task"
		item.TargetID = record.ID
		item.Summary = fmt.Sprintf("progress %d/%d; attempt %d/%d", record.Current, record.Total, record.Attempts, record.MaxAttempts)
		item.Error = record.Errors
		item.StartedAt = record.StartedAt
		item.FinishedAt = record.FinishedAt
		item.CreatedAt = record.CreatedAt
	default:
		BadRequest(w, "invalid operation log source")
		return
	}
	item.HasOutput = item.Output != ""
	item.HasError = item.Error != ""
	OK(w, item)
}

func (h *handlers) operationLogReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	ServerError(w, err)
}

func optionalUintQuery(r *http.Request, name string) (uint, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return uint(parsed), nil
}

func operationTaskStatus(status string) int16 {
	return map[string]int16{"queued": 0, "running": 1, "succeeded": 2, "failed": 3}[status]
}

func normalizeTaskStatus(status int16) string {
	return map[int16]string{0: "queued", 1: "running", 2: "succeeded", 3: "failed"}[status]
}
