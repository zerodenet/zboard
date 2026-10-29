package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeRetirement struct{ DB *gorm.DB }

func retirementView(row model.NodeRetirement) network.Retirement {
	out := network.Retirement{NodeID: row.NodeID, NodeName: row.NodeName, Mode: row.Mode, State: row.State, RunID: row.RunID, Attempt: row.Attempt, LastError: row.LastError}
	_ = json.Unmarshal([]byte(row.Steps), &out.Steps)
	return out
}
func (s NodeRetirement) Read(ctx context.Context, id uint) (network.Retirement, error) {
	var row model.NodeRetirement
	if err := s.DB.WithContext(ctx).First(&row, "node_id = ?", id).Error; err != nil {
		return network.Retirement{}, removalError(err)
	}
	if err := validateRetirementPlan(row); err != nil {
		return network.Retirement{}, err
	}
	out := retirementView(row)
	if row.State != "succeeded" {
		var run jobstore.Record
		if err := s.DB.WithContext(ctx).First(&run, "id = ?", row.RunID).Error; err != nil {
			return out, err
		}
		if run.State != "succeeded" {
			out.State = run.State
		}
		if (out.State == "failed" || out.State == "unknown" || out.State == "interrupted") && out.LastError == "" {
			out.LastError = "清理或移除面板记录尚未完成；记录已保留，请检查任务并重试。"
		}
	}
	return out, nil
}
func (s NodeRetirement) Start(ctx context.Context, actor, id uint, mode string) (out network.Retirement, err error) {
	if mode != "retire" && mode != "detach" {
		return out, jobs.ErrInvalid
	}
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			return network.ErrResourcePermission
		}
		var row model.NodeRetirement
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "node_id = ?", id)
		if found.Error != nil && !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return found.Error
		}
		if found.Error == nil {
			if err := validateRetirementPlan(row); err != nil {
				return err
			}
			if row.Mode != mode && mode != "detach" {
				return retirementBlocked("不能将已移除管理的任务切换为远端清理")
			}
			if row.State == "succeeded" {
				out = retirementView(row)
				return nil
			}
			var run jobstore.Record
			if err := tx.First(&run, "id = ?", row.RunID).Error; err != nil {
				return err
			}
			switch jobs.State(run.State) {
			case jobs.Queued, jobs.Running, jobs.RetryWait, jobs.CancelRequested:
				if row.Mode != mode {
					return jobs.ErrConflict
				}
				out = retirementView(row)
				out.State = run.State
				return nil
			case jobs.Unknown, jobs.Interrupted:
				// Every remote command is bounded by 150 seconds. After an expired
				// lease, wait out that window before explicitly reconciling by retry.
				fence := run.ExpiresAt
				if run.FinishedAt != nil && fence != nil && run.FinishedAt.Before(*fence) {
					fence = run.FinishedAt
				}
				if fence == nil || time.Now().Before(fence.Add(3*time.Minute)) {
					return retirementBlocked("上次执行结果待核验，请等待远端命令超时后再重试")
				}
				if err := jobstore.New(tx).Resolve(ctx, run.ID, jobs.Failed); err != nil {
					return err
				}
			case jobs.Failed, jobs.Canceled:
			default:
				return jobs.ErrConflict
			}
			if row.Mode != mode {
				view := retirementView(row)
				for i := range view.Steps {
					if view.Steps[i].State != "succeeded" {
						view.Steps[i].State = "retained"
						view.Steps[i].Message = "仅移除面板管理，保留剩余外部资源。"
					}
				}
				b, _ := json.Marshal(view.Steps)
				row.Steps = string(b)
				row.Mode = mode
			}
		}
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, id).Error; err != nil {
			return removalError(err)
		}
		if mode == "retire" {
			if node.SSHHost == "" || node.SSHHostKeyFingerprint == "" {
				return retirementBlocked("请先配置并验证 SSH；无法远程清理时可选择仅移除面板管理")
			}
			if err := retirementHostExclusive(tx, node); err != nil {
				return err
			}
		}
		var sharedCertificates int64
		if err := tx.Table("certificate_protocol_endpoints AS c").Joins("JOIN managed_certificates AS m ON m.id = c.managed_certificate_id").Joins("JOIN protocol_endpoints AS p ON p.id = c.protocol_endpoint_id").Where("m.node_id = ? AND p.node_id <> ?", id, id).Count(&sharedCertificates).Error; err != nil {
			return err
		}
		if sharedCertificates > 0 {
			return retirementBlocked("证书仍被其他节点引用，请先处理共享关联；证书与节点均已保留")
		}
		var endpoints []uint
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", id).Pluck("id", &endpoints).Error; err != nil {
			return err
		}
		blockers, err := nodeRemovalBlockers(tx, id, endpoints)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return &network.ResourceRemovalBlocked{Blockers: blockers}
		}
		if found.Error != nil {
			steps := []network.RetirementStep{}
			if mode == "retire" {
				var dns []model.ManagedDNSRecord
				var certificates []model.ManagedCertificate
				if err := tx.Where("node_id = ?", id).Order("id").Find(&dns).Error; err != nil {
					return err
				}
				if err := tx.Where("node_id = ?", id).Order("id").Find(&certificates).Error; err != nil {
					return err
				}
				for _, d := range dns {
					steps = append(steps, network.RetirementStep{Kind: "dns", ID: d.ID, Label: "DNS：" + d.DomainName, State: "pending"})
				}
				for _, c := range certificates {
					steps = append(steps, network.RetirementStep{Kind: "certificate", ID: c.ID, Label: "证书：" + c.Name, State: "pending"})
				}
				steps = append(steps, network.RetirementStep{Kind: "zero", ID: id, Label: "远端 Zero", State: "pending"})
			}
			encoded, _ := json.Marshal(steps)
			row = model.NodeRetirement{NodeID: id, NodeName: node.Name, ActorID: actor, Mode: mode, Steps: string(encoded)}
		}
		row.ActorID = actor
		row.Attempt++
		row.State = "queued"
		row.LastError = ""
		payload, _ := json.Marshal(network.NodeRetirementRunInput{Revision: network.NodeRetirementRevision, NodeID: id})
		resource := network.NodeCleanupResource(node.SSHHost, node.SSHPort)
		if node.SSHHost == "" || mode == "detach" {
			resource = fmt.Sprintf("node-retirement:%d", id)
		}
		run, err := jobstore.New(tx).SubmitLocked(ctx, jobs.Submission{Owner: "system", Handler: network.NodeRetirementHandler, Key: fmt.Sprintf("node-retirement:%d:%d", id, row.Attempt), Resource: resource, Timeout: 10 * time.Minute, Payload: string(payload)})
		if err != nil {
			return err
		}
		row.RunID = run.ID
		if err := tx.Model(&node).Updates(map[string]any{"lifecycle_status": "deleting", "is_enabled": false}).Error; err != nil {
			return err
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Action: "node.retirement.start", Target: fmt.Sprintf("node:%d", id), Detail: mode + " run=" + run.ID}).Error; err != nil {
			return err
		}
		out = retirementView(row)
		return nil
	})
	return
}

func retirementLease(tx *gorm.DB, id uint, runID string) (model.NodeRetirement, error) {
	var row model.NodeRetirement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "node_id = ? AND run_id = ?", id, runID).Error; err != nil {
		return row, jobs.ErrLeaseLost
	}
	if err := validateRetirementPlan(row); err != nil {
		return row, err
	}
	var run jobstore.Record
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
		return row, err
	}
	if run.State != "running" || run.ExpiresAt == nil || !run.ExpiresAt.After(time.Now()) {
		return row, jobs.ErrLeaseLost
	}
	return row, nil
}
func (s NodeRetirement) Advance(ctx context.Context, id uint, runID string, index int, state, message string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := retirementLease(tx, id, runID)
		if err != nil {
			return err
		}
		view := retirementView(row)
		if index < 0 || index >= len(view.Steps) {
			return jobs.ErrInvalid
		}
		view.Steps[index].State = state
		view.Steps[index].Message = message
		row.State = "running"
		if state == "failed" {
			row.State = "failed"
			row.LastError = message
		}
		b, _ := json.Marshal(view.Steps)
		row.Steps = string(b)
		return tx.Save(&row).Error
	})
}
func (s NodeRetirement) Finalize(ctx context.Context, id uint, runID string) error {
	if err := s.prepareRemoval(ctx, id, runID); err != nil {
		return err
	}
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		row, err := retirementLease(tx, id, runID)
		if err != nil {
			return err
		}
		for _, step := range retirementView(row).Steps {
			if step.State != "succeeded" && step.State != "retained" {
				return jobs.ErrConflict
			}
		}
		if _, err := (NodeRemoval{DB: tx, SkipRemote: true, FinalizingRetirement: true}).RemoveNode(ctx, row.ActorID, id); err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"state": "succeeded", "last_error": ""}).Error
	})
}
