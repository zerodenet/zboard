package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
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
	endpoint, err := h.services.ProtocolEndpointMultiplier.Update(r.Context(), claims.UserID, id, req.MultiplierMilli)
	if err != nil {
		var validation *networkcap.ProtocolEndpointMultiplierValidation
		switch {
		case errors.As(err, &validation):
			BadRequest(w, validation.Fields["multiplier_milli"])
			return
		case errors.Is(err, networkcap.ErrProtocolEndpointNotFound):
			NotFound(w)
			return
		case errors.Is(err, networkcap.ErrProtocolEndpointMultiplierPermission):
			Forbidden(w, "管理员权限已失效。")
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, endpoint)
}

type operationLogItem = observability.OperationLogItem

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
	sources := []string{"protocol_publish", "node_kernel", "task"}
	for _, candidate := range sources {
		if source != "" && source != candidate {
			continue
		}
		if candidate == "node_kernel" && endpointID != 0 {
			continue
		}
		if candidate == "task" && (nodeID != 0 || endpointID != 0) {
			continue
		}
		query := observability.OperationLogQuery{Source: candidate, Status: status, NodeID: nodeID, ProtocolEndpointID: endpointID, From: window.From, To: window.To, FetchLimit: fetchLimit}
		if cursor != nil {
			query.CursorAt, query.CursorID, query.CursorSource, query.CursorDirection = cursor.At, cursor.ID, cursor.Source, cursor.Direction
		}
		page, readErr := h.services.OperationLogs.ListSource(r.Context(), query)
		if readErr != nil {
			ServerError(w, readErr)
			return
		}
		total += page.Total
		items = append(items, page.Items...)
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
	if parts[0] != "protocol_publish" && parts[0] != "node_kernel" && parts[0] != "task" {
		BadRequest(w, "invalid operation log source")
		return
	}
	item, err := h.services.OperationLogs.Detail(r.Context(), parts[0], uint(id))
	if err != nil {
		h.operationLogReadError(w, err)
		return
	}
	OK(w, item)
}

func (h *handlers) operationLogReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, observability.ErrOperationLogNotFound) {
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

// These compatibility projections remain transport-local for tests and JSON
// contracts; operation-log persistence owns their use in database queries.
func operationTaskStatus(status string) int16 {
	return map[string]int16{"queued": 0, "running": 1, "succeeded": 2, "failed": 3}[status]
}

func normalizeTaskStatus(status int16) string {
	return map[int16]string{0: "queued", 1: "running", 2: "succeeded", 3: "failed"}[status]
}
