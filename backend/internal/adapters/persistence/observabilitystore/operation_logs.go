package observabilitystore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type OperationLogs struct{ DB *gorm.DB }

func operationTaskStatus(status string) int16 {
	return map[string]int16{"queued": 0, "running": 1, "succeeded": 2, "failed": 3}[status]
}
func normalizeTaskStatus(status int16) string {
	return map[int16]string{0: "queued", 1: "running", 2: "succeeded", 3: "failed"}[status]
}

func operationCursor(query *gorm.DB, q observability.OperationLogQuery) *gorm.DB {
	if q.CursorID == 0 {
		return query
	}
	if q.CursorDirection == "older" {
		switch strings.Compare(q.Source, q.CursorSource) {
		case -1:
			return query.Where("created_at < ?", q.CursorAt)
		case 0:
			return query.Where("(created_at < ?) OR (created_at = ? AND id < ?)", q.CursorAt, q.CursorAt, q.CursorID)
		default:
			return query.Where("created_at <= ?", q.CursorAt)
		}
	}
	switch strings.Compare(q.Source, q.CursorSource) {
	case -1:
		return query.Where("created_at >= ?", q.CursorAt)
	case 0:
		return query.Where("(created_at > ?) OR (created_at = ? AND id > ?)", q.CursorAt, q.CursorAt, q.CursorID)
	default:
		return query.Where("created_at > ?", q.CursorAt)
	}
}

func operationOrder(q observability.OperationLogQuery) string {
	if q.CursorID > 0 && q.CursorDirection == "newer" {
		return "created_at asc, id asc"
	}
	return "created_at desc, id desc"
}

func (s OperationLogs) ListSource(ctx context.Context, q observability.OperationLogQuery) (observability.OperationLogSourcePage, error) {
	page := observability.OperationLogSourcePage{}
	base := func(modelValue any) *gorm.DB {
		return s.DB.WithContext(ctx).Model(modelValue).Where("created_at >= ? AND created_at < ?", q.From, q.To)
	}
	switch q.Source {
	case "protocol_publish":
		query := base(&model.ProtocolDeployment{})
		if q.Status != "" {
			query = query.Where("status = ?", q.Status)
		}
		if q.NodeID != 0 {
			query = query.Where("node_id = ?", q.NodeID)
		}
		if q.ProtocolEndpointID != 0 {
			query = query.Where("protocol_endpoint_id = ?", q.ProtocolEndpointID)
		}
		if err := query.Count(&page.Total).Error; err != nil {
			return page, err
		}
		var rows []model.ProtocolDeployment
		if err := operationCursor(query, q).Order(operationOrder(q)).Limit(q.FetchLimit).Find(&rows).Error; err != nil {
			return page, err
		}
		for _, row := range rows {
			page.Items = append(page.Items, observability.OperationLogItem{ID: row.ID, Source: q.Source, Action: "protocol.publish", Status: row.Status, TargetType: "protocol_endpoint", TargetID: row.ProtocolEndpointID, NodeID: row.NodeID, ProtocolEndpointID: row.ProtocolEndpointID, Summary: fmt.Sprintf("config revision %d", row.ConfigRevision), HasOutput: row.Output != "", HasError: row.Error != "", StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt})
		}
	case "node_kernel":
		query := base(&model.NodeOperation{})
		if q.Status != "" {
			query = query.Where("status = ?", q.Status)
		}
		if q.NodeID != 0 {
			query = query.Where("node_id = ?", q.NodeID)
		}
		if err := query.Count(&page.Total).Error; err != nil {
			return page, err
		}
		var rows []model.NodeOperation
		if err := operationCursor(query, q).Order(operationOrder(q)).Limit(q.FetchLimit).Find(&rows).Error; err != nil {
			return page, err
		}
		for _, row := range rows {
			page.Items = append(page.Items, observability.OperationLogItem{ID: row.ID, Source: q.Source, Action: "node.kernel." + row.OperationType, Status: row.Status, TargetType: "node", TargetID: row.NodeID, NodeID: row.NodeID, Summary: row.ResultSummary, HasError: row.Error != "", StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt})
		}
	case "task":
		query := base(&model.Task{})
		if q.Status != "" {
			query = query.Where("status = ?", operationTaskStatus(q.Status))
		}
		if err := query.Count(&page.Total).Error; err != nil {
			return page, err
		}
		var rows []model.Task
		if err := operationCursor(query, q).Order(operationOrder(q)).Limit(q.FetchLimit).Find(&rows).Error; err != nil {
			return page, err
		}
		for _, row := range rows {
			page.Items = append(page.Items, observability.OperationLogItem{ID: row.ID, Source: q.Source, Action: "task." + row.Type, Status: normalizeTaskStatus(row.Status), TargetType: "task", TargetID: row.ID, Summary: fmt.Sprintf("progress %d/%d; attempt %d/%d", row.Current, row.Total, row.Attempts, row.MaxAttempts), HasError: row.Errors != "", StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt})
		}
	}
	return page, nil
}

func (s OperationLogs) Detail(ctx context.Context, source string, id uint) (observability.OperationLogItem, error) {
	item := observability.OperationLogItem{ID: id, Source: source}
	notFound := func(err error) error {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return observability.ErrOperationLogNotFound
		}
		return err
	}
	switch source {
	case "protocol_publish":
		var row model.ProtocolDeployment
		if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
			return item, notFound(err)
		}
		item.Action, item.Status, item.TargetType, item.TargetID, item.NodeID, item.ProtocolEndpointID = "protocol.publish", row.Status, "protocol_endpoint", row.ProtocolEndpointID, row.NodeID, row.ProtocolEndpointID
		item.Summary, item.Output, item.Error, item.StartedAt, item.FinishedAt, item.CreatedAt = fmt.Sprintf("config revision %d", row.ConfigRevision), row.Output, row.Error, row.StartedAt, row.FinishedAt, row.CreatedAt
	case "node_kernel":
		var row model.NodeOperation
		if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
			return item, notFound(err)
		}
		item.Action, item.Status, item.TargetType, item.TargetID, item.NodeID = "node.kernel."+row.OperationType, row.Status, "node", row.NodeID, row.NodeID
		item.Summary, item.Error, item.StartedAt, item.FinishedAt, item.CreatedAt = row.ResultSummary, row.Error, row.StartedAt, row.FinishedAt, row.CreatedAt
	case "task":
		var row model.Task
		if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
			return item, notFound(err)
		}
		item.Action, item.Status, item.TargetType, item.TargetID = "task."+row.Type, normalizeTaskStatus(row.Status), "task", row.ID
		item.Summary, item.Error, item.StartedAt, item.FinishedAt, item.CreatedAt = fmt.Sprintf("progress %d/%d; attempt %d/%d", row.Current, row.Total, row.Attempts, row.MaxAttempts), row.Errors, row.StartedAt, row.FinishedAt, row.CreatedAt
	default:
		return item, observability.ErrOperationLogNotFound
	}
	item.HasOutput, item.HasError = item.Output != "", item.Error != ""
	return item, nil
}
