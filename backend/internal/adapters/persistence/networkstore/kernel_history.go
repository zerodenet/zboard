package networkstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type KernelHistory struct{ DB *gorm.DB }

func (s KernelHistory) ReadKernelHistory(ctx context.Context, request network.KernelDetectionRequest, limit int) (out network.KernelHistoryResult, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor model.User
		err := tx.Select("id").Where("id = ? AND is_admin = ? AND status = ?", request.ActorID, true, "active").First(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrKernelPermission
		}
		if err != nil {
			return err
		}
		var node model.Node
		if err := tx.Select("id").First(&node, request.NodeID).Error; err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, node.ID)
		if err != nil {
			return err
		}
		var rows []model.NodeOperation
		if err := tx.Where("node_id = ?", node.ID).Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		operations := make([]network.KernelOperation, 0, len(rows))
		for _, row := range rows {
			operations = append(operations, kernelOperationView(row))
		}
		out = network.KernelHistoryResult{State: kernelStateView(state), Operations: operations}
		return nil
	})
	return
}
