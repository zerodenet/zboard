package networkstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type KernelDetection struct{ DB *gorm.DB }

func (s KernelDetection) BeginDetection(ctx context.Context, request network.KernelDetectionRequest, now time.Time) (out network.KernelOperation, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, request.NodeID).Error; err != nil {
			return err
		}
		if node.LifecycleStatus == "deleting" {
			return network.ErrKernelResourceDeleting
		}
		var actor model.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", request.ActorID, true, "active").First(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrKernelPermission
		}
		if err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, request.NodeID)
		if err != nil {
			return err
		}
		if state.ActiveOperationID != nil {
			var active model.NodeOperation
			if err := tx.First(&active, *state.ActiveOperationID).Error; err == nil && active.Status == "running" {
				return network.ErrKernelOperationRunning
			}
		}
		operation := model.NodeOperation{
			NodeID: request.NodeID, OperationType: "detect", Status: "running", Phase: "detecting",
			RequestedBy: actor.ID, StartedAt: &now,
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"phase": "detecting", "active_operation_id": operation.ID, "last_error": "",
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{
			UserID: &actor.ID, Actor: actor.Email, Action: "node.kernel.detect",
			Target: fmt.Sprintf("node:%d", request.NodeID), Detail: fmt.Sprintf("operation=%d", operation.ID),
		}).Error; err != nil {
			return err
		}
		out = kernelOperationView(operation)
		return nil
	})
	return
}

func (s KernelDetection) CompleteDetection(ctx context.Context, operation network.KernelOperation, probe network.KernelProbe, assessment network.KernelAssessment, summary string, now time.Time) (out network.KernelDetectionResult, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		stateUpdates := map[string]interface{}{
			"status": assessment.Status, "phase": "idle", "recommended_action": assessment.RecommendedAction,
			"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
			"installed_version": probe.Version, "installed_sha256": probe.BinarySHA256,
			"applied_config_sha256": probe.ConfigSHA256, "service_status": probe.ServiceStatus,
			"control_status": probe.ControlStatus, "last_detected_at": now, "last_error": "", "active_operation_id": nil,
		}
		if assessment.Status == "healthy" {
			stateUpdates["last_healthy_at"] = now
		}
		if err := tx.Model(&state).Updates(stateUpdates).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"status": "succeeded", "phase": "completed", "result_summary": summary, "error": "", "finished_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.First(&state, "node_id = ?", operation.NodeID).Error; err != nil {
			return err
		}
		if err := tx.First(&row, operation.ID).Error; err != nil {
			return err
		}
		out = network.KernelDetectionResult{State: kernelStateView(state), Operation: kernelOperationView(row)}
		return nil
	})
	return
}

func (s KernelDetection) FailDetection(ctx context.Context, operation network.KernelOperation, phase string, failure error, unsupported bool, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		status, action := "failed", "retry"
		if unsupported {
			status, action = "unsupported", "manual_review"
		}
		message := network.TruncateKernelError(failure.Error())
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"status": "failed", "phase": phase, "error": message, "finished_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&state).Updates(map[string]interface{}{
			"status": status, "phase": "idle", "recommended_action": action,
			"last_error": message, "active_operation_id": nil,
		}).Error
	})
}

func lockOrCreateKernelState(tx *gorm.DB, nodeID uint) (model.NodeKernelState, error) {
	var state model.NodeKernelState
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&state).Error
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}
	state = model.NodeKernelState{NodeID: nodeID, Status: "unknown", Phase: "idle", RecommendedAction: "detect"}
	return state, tx.Create(&state).Error
}

func lockOwnedKernelOperation(tx *gorm.DB, operation network.KernelOperation) (model.NodeKernelState, model.NodeOperation, error) {
	var state model.NodeKernelState
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND active_operation_id = ?", operation.NodeID, operation.ID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, model.NodeOperation{}, network.ErrKernelOperationLost
	}
	if err != nil {
		return state, model.NodeOperation{}, err
	}
	var row model.NodeOperation
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND node_id = ? AND status = ?", operation.ID, operation.NodeID, "running").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, row, network.ErrKernelOperationLost
	}
	return state, row, err
}

func kernelOperationView(row model.NodeOperation) network.KernelOperation {
	return network.KernelOperation{
		ID: row.ID, NodeID: row.NodeID, RequestedBy: row.RequestedBy,
		OperationType: row.OperationType, Status: row.Status, Phase: row.Phase,
		DesiredVersion: row.DesiredVersion, DesiredSHA256: row.DesiredSHA256, ArtifactURL: row.ArtifactURL,
		ResultSummary: row.ResultSummary, Error: row.Error, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func kernelStateView(row model.NodeKernelState) network.KernelState {
	out := network.KernelState{
		NodeID: row.NodeID, Status: row.Status, Phase: row.Phase, RecommendedAction: row.RecommendedAction,
		PlatformOS: row.PlatformOS, Architecture: row.Architecture, Libc: row.Libc,
		DesiredVersion: row.DesiredVersion, InstalledVersion: row.InstalledVersion,
		DesiredSHA256: row.DesiredSHA256, InstalledSHA256: row.InstalledSHA256,
		DesiredConfigSHA256: row.DesiredConfigSHA256, AppliedConfigSHA256: row.AppliedConfigSHA256,
		ServiceStatus: row.ServiceStatus, ControlStatus: row.ControlStatus, LastError: row.LastError,
		LastDetectedAt: row.LastDetectedAt, LastHealthyAt: row.LastHealthyAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ActiveOperationID != nil {
		operationID := *row.ActiveOperationID
		out.ActiveOperationID = &operationID
	}
	return out
}
