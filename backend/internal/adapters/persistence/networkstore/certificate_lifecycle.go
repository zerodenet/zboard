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

type CertificateLifecycle struct{ DB *gorm.DB }

func certificateModel(row network.CertificateRecord) model.ManagedCertificate {
	return model.ManagedCertificate{ID: row.ID, NodeID: row.NodeID, ProviderAccountID: row.ProviderAccountID, Name: row.Name, Domains: row.Domains, ContactEmail: row.ContactEmail, Environment: row.Environment, ChallengeType: row.ChallengeType, WebrootPath: row.WebrootPath, Status: row.Status, CertPath: row.CertPath, KeyPath: row.KeyPath, SerialNumber: row.SerialNumber, FingerprintSHA256: row.FingerprintSHA256, NotBefore: row.NotBefore, NotAfter: row.NotAfter, LastIssuedAt: row.LastIssuedAt, LastRenewalAttemptAt: row.LastRenewalAttemptAt, NextRenewalAt: row.NextRenewalAt, AutoRenew: row.AutoRenew, RenewBeforeDays: row.RenewBeforeDays, LastError: row.LastError, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func certificateView(row model.ManagedCertificate) network.CertificateRecord {
	return network.CertificateRecord{ID: row.ID, NodeID: row.NodeID, ProviderAccountID: row.ProviderAccountID, Name: row.Name, Domains: row.Domains, ContactEmail: row.ContactEmail, Environment: row.Environment, ChallengeType: row.ChallengeType, WebrootPath: row.WebrootPath, Status: row.Status, CertPath: row.CertPath, KeyPath: row.KeyPath, SerialNumber: row.SerialNumber, FingerprintSHA256: row.FingerprintSHA256, NotBefore: row.NotBefore, NotAfter: row.NotAfter, LastIssuedAt: row.LastIssuedAt, LastRenewalAttemptAt: row.LastRenewalAttemptAt, NextRenewalAt: row.NextRenewalAt, AutoRenew: row.AutoRenew, RenewBeforeDays: row.RenewBeforeDays, LastError: row.LastError, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func certificateOperationView(row model.CertificateOperation) network.CertificateOperationRecord {
	return network.CertificateOperationRecord{ID: row.ID, ManagedCertificateID: row.ManagedCertificateID, NodeID: row.NodeID, OperationType: row.OperationType, Status: row.Status, Phase: row.Phase, RequestedBy: row.RequestedBy, ResultSummary: row.ResultSummary, Error: row.Error, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s CertificateLifecycle) CreateCertificate(ctx context.Context, actor uint, input network.CertificateRecord) (out network.CertificateRecord, nodeName string, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrCertificatePermission
		}
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "lifecycle_status").First(&node, input.NodeID).Error; err != nil {
			return network.ErrCertificateDependency
		}
		if node.LifecycleStatus == "deleting" {
			return network.ErrCertificateDeleting
		}
		if input.ChallengeType == "dns-01" {
			if input.ProviderAccountID == nil {
				return network.ErrCertificateDependency
			}
			var provider model.ProviderAccount
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "provider_key", "status", "capabilities").First(&provider, *input.ProviderAccountID).Error; err != nil || provider.Status != "active" || !providerSupportsCertificate(provider.Capabilities) {
				return network.ErrCertificateDependency
			}
		} else if input.ProviderAccountID != nil {
			return network.ErrCertificateDependency
		}
		row := certificateModel(input)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var domains []string
		_ = json.Unmarshal([]byte(row.Domains), &domains)
		detail := fmt.Sprintf("node=%d environment=%s challenge=%s domains=%d", row.NodeID, row.Environment, row.ChallengeType, len(domains))
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "certificate.create", Target: fmt.Sprintf("certificate:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		out, nodeName = certificateView(row), node.Name
		return nil
	})
	return
}

func (s CertificateLifecycle) UpdateCertificate(ctx context.Context, actor, id uint, change network.CertificateUpdate) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return certificateNotFound(err)
		}
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrCertificatePermission
		}
		if row.Status == "deleting" {
			return network.ErrCertificateDeleting
		}
		if row.Revision != change.ExpectedRevision {
			return network.ErrCertificateConflict
		}
		if row.Status == "issuing" || row.Status == "renewing" {
			return network.ErrCertificateOperationActive
		}
		if row.ChallengeType == "http-01-webroot" {
			if change.WebrootPath == "" {
				return network.ErrCertificateDependency
			}
		} else if change.WebrootPath != "" {
			return network.ErrCertificateDependency
		}
		updates := map[string]any{"name": change.Name, "contact_email": change.ContactEmail, "webroot_path": change.WebrootPath, "auto_renew": change.AutoRenew, "renew_before_days": change.RenewBeforeDays, "revision": row.Revision + 1}
		if row.NotAfter != nil {
			updates["next_renewal_at"] = row.NotAfter.Add(-time.Duration(change.RenewBeforeDays) * 24 * time.Hour)
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		detail := fmt.Sprintf("name=%s auto_renew=%t renew_before_days=%d", change.Name, change.AutoRenew, change.RenewBeforeDays)
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "certificate.update", Target: fmt.Sprintf("certificate:%d", row.ID), Detail: detail}).Error
	})
}

func (s CertificateLifecycle) UpdateCertificateRenewal(ctx context.Context, actor, id uint, auto bool, days int, expected uint64) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return certificateNotFound(err)
		}
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrCertificatePermission
		}
		if row.Status == "deleting" {
			return network.ErrCertificateDeleting
		}
		if row.Revision != expected {
			return network.ErrCertificateConflict
		}
		updates := map[string]any{"auto_renew": auto, "renew_before_days": days, "revision": row.Revision + 1}
		if row.NotAfter != nil {
			updates["next_renewal_at"] = row.NotAfter.Add(-time.Duration(days) * 24 * time.Hour)
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		detail := fmt.Sprintf("auto_renew=%t renew_before_days=%d", auto, days)
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "certificate.renewal_policy.update", Target: fmt.Sprintf("certificate:%d", row.ID), Detail: detail}).Error
	})
}

func (s CertificateLifecycle) StartCertificateOperation(ctx context.Context, actor, certificateID uint, operationType string) (out network.CertificateOperationRecord, err error) {
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var certificate model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, certificateID).Error; err != nil {
			return certificateNotFound(err)
		}
		var requestedBy *uint
		if actor != 0 {
			if _, err := providerAdmin(tx, actor); err != nil {
				return network.ErrCertificatePermission
			}
			requestedBy = &actor
		}
		if certificate.Status == "deleting" {
			return network.ErrCertificateDeleting
		}
		if certificate.Status == "issuing" || certificate.Status == "renewing" {
			return network.ErrCertificateOperationActive
		}
		if operationType == "renew" && certificate.NotAfter == nil {
			return network.ErrCertificateDependency
		}
		now := time.Now().UTC()
		status := "issuing"
		if operationType == "renew" {
			status = "renewing"
		}
		operation := model.CertificateOperation{ManagedCertificateID: certificate.ID, NodeID: certificate.NodeID, OperationType: operationType, Status: "running", Phase: "queued", RequestedBy: requestedBy, StartedAt: &now}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		updates := map[string]any{"status": status, "last_error": ""}
		if operationType == "renew" {
			updates["last_renewal_attempt_at"] = now
		}
		if err := tx.Model(&certificate).Updates(updates).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(struct {
			Revision    string `json:"revision"`
			OperationID uint   `json:"operation_id"`
		}{Revision: "1", OperationID: operation.ID})
		if err != nil {
			return err
		}
		if _, err := jobstore.New(tx).Submit(ctx, jobs.Submission{Timeout: 30 * time.Minute, Owner: "system", Key: fmt.Sprintf("certificate_operation:%d", operation.ID), Handler: "certificate_operation", ExecutionGroup: "external", Resource: fmt.Sprintf("certificate:%d", certificateID), Payload: string(payload)}); err != nil {
			return err
		}
		out = certificateOperationView(operation)
		return nil
	})
	return
}

func (s CertificateLifecycle) SetCertificateOperationPhase(ctx context.Context, operationID uint, phase string) error {
	result := s.DB.WithContext(ctx).Model(&model.CertificateOperation{}).Where("id = ? AND status = ?", operationID, "running").Update("phase", phase)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return network.ErrCertificateOperationActive
	}
	return nil
}

func (s CertificateLifecycle) RecordCertificateIssued(ctx context.Context, operationID, certificateID uint, issued network.CertificateIssued) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operation model.CertificateOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operation, operationID).Error; err != nil {
			return err
		}
		if operation.Status != "running" || operation.ManagedCertificateID != certificateID {
			return network.ErrCertificateOperationActive
		}
		return tx.Model(&model.ManagedCertificate{}).Where("id = ?", certificateID).Updates(map[string]any{"status": "active", "cert_path": issued.CertPath, "key_path": issued.KeyPath, "serial_number": issued.SerialNumber, "fingerprint_sha256": issued.FingerprintSHA256, "not_before": issued.NotBefore, "not_after": issued.NotAfter, "last_issued_at": issued.IssuedAt, "next_renewal_at": issued.NextRenewalAt, "last_error": ""}).Error
	})
}

func (s CertificateLifecycle) CompleteCertificateOperation(ctx context.Context, operationID uint, summary string, at time.Time) error {
	result := s.DB.WithContext(ctx).Model(&model.CertificateOperation{}).Where("id = ? AND status = ?", operationID, "running").Updates(map[string]any{"status": "succeeded", "phase": "completed", "result_summary": summary, "error": "", "finished_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return network.ErrCertificateOperationActive
	}
	return nil
}

func (s CertificateLifecycle) FailCertificateOperation(ctx context.Context, operationID, certificateID uint, failure network.CertificateFailure) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if certificateID != 0 {
			var certificate model.ManagedCertificate
			if err := tx.First(&certificate, certificateID).Error; err != nil {
				return err
			}
			status := "failed"
			if failure.Usable {
				status = "active"
			} else if certificate.NotAfter != nil && !certificate.NotAfter.After(failure.At) {
				status = "expired"
			}
			if err := tx.Model(&certificate).Updates(map[string]any{"status": status, "last_error": failure.Message, "next_renewal_at": failure.NextRetry}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.CertificateOperation{}).Where("id = ? AND status = ?", operationID, "running").Updates(map[string]any{"status": "failed", "phase": failure.Phase, "error": failure.Message, "finished_at": failure.At}).Error
	})
}

func (s CertificateLifecycle) DueCertificateRenewals(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if err := s.DB.WithContext(ctx).Model(&model.ManagedCertificate{}).Where("not_after IS NOT NULL AND not_after <= ? AND status NOT IN ?", now, []string{"issuing", "renewing", "expired", "deleting"}).Updates(map[string]any{"status": "expired", "last_error": "certificate has expired"}).Error; err != nil {
		return nil, err
	}
	var ids []uint
	err := s.DB.WithContext(ctx).Model(&model.ManagedCertificate{}).Where("auto_renew = ? AND not_after IS NOT NULL AND not_after > ? AND next_renewal_at IS NOT NULL AND next_renewal_at <= ? AND status IN ?", true, now, now, []string{"active", "failed"}).Order("next_renewal_at asc, id asc").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (s CertificateLifecycle) PrepareCertificateOperation(ctx context.Context, operationID uint) (network.CertificateExecution, error) {
	var operation model.CertificateOperation
	if err := s.DB.WithContext(ctx).First(&operation, operationID).Error; err != nil {
		return network.CertificateExecution{}, certificateNotFound(err)
	}
	result := network.CertificateExecution{Operation: certificateOperationView(operation)}
	var certificate model.ManagedCertificate
	if err := s.DB.WithContext(ctx).First(&certificate, operation.ManagedCertificateID).Error; err != nil {
		return result, certificateNotFound(err)
	}
	result.Certificate = certificateView(certificate)
	var node model.Node
	if err := s.DB.WithContext(ctx).First(&node, certificate.NodeID).Error; err != nil {
		return result, network.ErrCertificateDependency
	}
	result.Node = network.CertificateExecutionNode{
		ID: node.ID, SSHHost: node.SSHHost, SSHPort: node.SSHPort, SSHUser: node.SSHUser,
		SSHAuthMethod: node.SSHAuthMethod, SSHPwdCiphertext: node.SSHPwd,
		SSHPrivateKeyPassphraseCiphertext: node.SSHPrivateKeyPassphrase,
		SSHPrivilegeMode:                  node.SSHPrivilegeMode, SSHPrivilegePasswordCiphertext: node.SSHPrivilegePassword,
		SSHHostKeyFingerprint: node.SSHHostKeyFingerprint,
	}
	if certificate.ProviderAccountID != nil {
		var provider model.ProviderAccount
		if err := s.DB.WithContext(ctx).First(&provider, *certificate.ProviderAccountID).Error; err == nil {
			var capabilities []string
			_ = json.Unmarshal([]byte(provider.Capabilities), &capabilities)
			result.Provider = &network.CertificateExecutionProvider{ProviderKey: provider.ProviderKey, Status: provider.Status, Capabilities: capabilities, CredentialCiphertext: provider.CredentialCiphertext}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
	}
	var binding model.CertificateProtocolEndpoint
	err := s.DB.WithContext(ctx).Where("managed_certificate_id = ?", certificate.ID).Order("id asc").First(&binding).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	if err == nil {
		result.BindingProtocolEndpointID = binding.ProtocolEndpointID
	}
	return result, nil
}

func providerSupportsCertificate(raw string) bool {
	var capabilities []string
	if json.Unmarshal([]byte(raw), &capabilities) != nil {
		return false
	}
	for _, capability := range capabilities {
		if capability == "certificate.issue" || capability == "certificate.origin" {
			return true
		}
	}
	return false
}

func certificateNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.ErrCertificateNotFound
	}
	return err
}
