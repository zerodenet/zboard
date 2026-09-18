package networkstore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type BatchScopes struct{ DB *gorm.DB }

func (s BatchScopes) ResolveBatchNodes(ctx context.Context, all bool, ids []uint, filter network.BatchNodeFilter, onlineAfter time.Time, limit int) ([]uint, error) {
	query := s.DB.WithContext(ctx).Model(&model.Node{}).Select("nodes.id")
	if all {
		pattern := "%" + filter.Query + "%"
		if filter.Query != "" {
			query = query.Where("LOWER(nodes.name) LIKE ? OR LOWER(nodes.address) LIKE ? OR LOWER(nodes.region) LIKE ?", pattern, pattern, pattern)
		}
		if filter.LifecycleStatus != "" {
			query = query.Where("nodes.lifecycle_status = ?", filter.LifecycleStatus)
		}
		if filter.ConnectorOnline != nil {
			if *filter.ConnectorOnline {
				query = query.Where("nodes.connector_last_seen_at >= ?", onlineAfter)
			} else {
				query = query.Where("nodes.connector_last_seen_at IS NULL OR nodes.connector_last_seen_at < ?", onlineAfter)
			}
		}
		if filter.Enabled != nil {
			query = query.Where("nodes.is_enabled = ?", *filter.Enabled)
		}
		if filter.KernelStatus != "" {
			query = query.Joins("JOIN node_kernel_states ON node_kernel_states.node_id = nodes.id").Where("node_kernel_states.status = ?", filter.KernelStatus)
		}
	} else {
		query = query.Where("nodes.id IN ?", ids)
	}
	resolved := []uint{}
	if err := query.Order("nodes.id asc").Limit(limit).Pluck("nodes.id", &resolved).Error; err != nil {
		return nil, err
	}
	if !all && len(resolved) != len(ids) {
		return nil, errors.New("one or more nodes do not exist")
	}
	return resolved, nil
}

func (s BatchScopes) ResolveBatchProtocols(ctx context.Context, all bool, ids []uint, filter network.BatchProtocolFilter, limit int) ([]uint, error) {
	query := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).Select("protocol_endpoints.id")
	if all {
		if filter.Query != "" {
			pattern := "%" + filter.Query + "%"
			query = query.Where("LOWER(protocol_endpoints.name) LIKE ? OR LOWER(protocol_endpoints.address) LIKE ?", pattern, pattern)
		}
		if filter.NodeID != 0 {
			query = query.Where("protocol_endpoints.node_id = ?", filter.NodeID)
		}
		if filter.Protocol != "" {
			query = query.Where("protocol_endpoints.protocol = ?", filter.Protocol)
		}
		if filter.Active != nil {
			query = query.Where("protocol_endpoints.is_active = ?", *filter.Active)
		}
		if filter.DeploymentStatus != "" {
			latestIDs := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("MAX(id)").Group("protocol_endpoint_id")
			if filter.DeploymentStatus == "never" {
				deployedIDs := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("DISTINCT protocol_endpoint_id")
				query = query.Where("protocol_endpoints.id NOT IN (?)", deployedIDs)
			} else {
				matchingIDs := s.DB.WithContext(ctx).Model(&model.ProtocolDeployment{}).Select("protocol_endpoint_id").Where("id IN (?) AND status = ?", latestIDs, filter.DeploymentStatus)
				query = query.Where("protocol_endpoints.id IN (?)", matchingIDs)
			}
		}
	} else {
		query = query.Where("protocol_endpoints.id IN ?", ids)
	}
	resolved := []uint{}
	if err := query.Order("protocol_endpoints.id asc").Limit(limit).Pluck("protocol_endpoints.id", &resolved).Error; err != nil {
		return nil, err
	}
	if !all && len(resolved) != len(ids) {
		return nil, errors.New("one or more protocol endpoints do not exist")
	}
	return resolved, nil
}

func (s BatchScopes) GroupBatchProtocols(ctx context.Context, endpointIDs []uint) ([]uint, map[string][]uint, error) {
	var rows []struct{ ID, NodeID uint }
	if err := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).Select("id, node_id").Where("id IN ?", endpointIDs).Order("node_id asc, id asc").Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	groups := make(map[string][]uint)
	nodeIDs := make([]uint, 0)
	for _, row := range rows {
		key := strconv.FormatUint(uint64(row.NodeID), 10)
		if _, exists := groups[key]; !exists {
			nodeIDs = append(nodeIDs, row.NodeID)
		}
		groups[key] = append(groups[key], row.ID)
	}
	return nodeIDs, groups, nil
}
