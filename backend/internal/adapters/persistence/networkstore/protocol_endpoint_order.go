package networkstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProtocolEndpointOrder struct{ DB *gorm.DB }

func (s ProtocolEndpointOrder) ReadProtocolEndpointOrder(ctx context.Context, actor uint) (out network.ProtocolEndpointOrderSnapshot, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProtocolEndpointOrderPermission
			}
			return err
		}
		out, err = loadProtocolEndpointOrder(tx, false)
		return err
	})
	return
}

func (s ProtocolEndpointOrder) UpdateProtocolEndpointOrder(ctx context.Context, actor uint, request network.ProtocolEndpointOrderRequest) (out network.ProtocolEndpointOrderSnapshot, changed bool, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProtocolEndpointOrderPermission
			}
			return err
		}
		current, err := loadProtocolEndpointOrder(tx, true)
		if err != nil {
			return err
		}
		if current.Version != request.ExpectedVersion {
			out = current
			return network.ErrProtocolEndpointOrderConflict
		}
		if err := network.ValidateCompleteProtocolEndpointOrder(current.Items, request.OrderedIDs); err != nil {
			return err
		}
		if len(current.Items) == len(request.OrderedIDs) {
			for index, item := range current.Items {
				if item.ID != request.OrderedIDs[index] || item.SortOrder != index {
					changed = true
					break
				}
			}
		}
		if !changed {
			out = current
			return nil
		}
		caseExpression := strings.Builder{}
		caseExpression.WriteString("CASE id")
		args := make([]any, 0, len(request.OrderedIDs)*2)
		for index, endpointID := range request.OrderedIDs {
			caseExpression.WriteString(" WHEN ? THEN ?")
			args = append(args, endpointID, index)
		}
		caseExpression.WriteString(" END")
		if len(request.OrderedIDs) > 0 {
			if err := tx.Model(&model.ProtocolEndpoint{}).Where("id IN ?", request.OrderedIDs).
				UpdateColumn("sort_order", gorm.Expr(caseExpression.String(), args...)).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{
			UserID: &admin.ID, Actor: admin.Email, Action: "protocol_endpoint.order", Target: "protocol_endpoints",
			Detail: fmt.Sprintf("endpoint_count=%d publish_status=not_required", len(request.OrderedIDs)),
		}).Error; err != nil {
			return err
		}
		out, err = loadProtocolEndpointOrder(tx, true)
		return err
	})
	return
}

func loadProtocolEndpointOrder(db *gorm.DB, lock bool) (network.ProtocolEndpointOrderSnapshot, error) {
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []model.ProtocolEndpoint
	if err := query.Select("id", "node_id", "name", "protocol", "is_active", "sort_order").Order("id asc").Find(&rows).Error; err != nil {
		return network.ProtocolEndpointOrderSnapshot{}, err
	}
	items := make([]network.ProtocolEndpointOrderItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, network.ProtocolEndpointOrderItem{
			ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol,
			IsActive: row.IsActive, SortOrder: row.SortOrder,
		})
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].SortOrder != items[right].SortOrder {
			return items[left].SortOrder < items[right].SortOrder
		}
		return items[left].ID < items[right].ID
	})
	return network.ProtocolEndpointOrderSnapshot{Items: items, Version: network.ProtocolEndpointOrderVersion(items), Total: len(items)}, nil
}
