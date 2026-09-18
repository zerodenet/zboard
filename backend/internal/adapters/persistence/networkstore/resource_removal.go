package networkstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourceRemoval struct{ DB *gorm.DB }

func (s ResourceRemoval) RemoveDNS(ctx context.Context, actor, id uint) error {
	return s.remove(ctx, actor, id, network.DNSOperation)
}
func (s ResourceRemoval) RemoveCertificate(ctx context.Context, actor, id uint) error {
	return s.remove(ctx, actor, id, network.CertificateOperation)
}
func (s ResourceRemoval) remove(ctx context.Context, actor, id uint, kind network.OperationKind) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var row any
		var status, action, target, detail string
		if kind == network.DNSOperation {
			var record model.ManagedDNSRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
				return removalError(err)
			}
			row, status = &record, record.Status
			action, target = "dns_record.delete", fmt.Sprintf("managed_dns_record:%d", id)
			detail = fmt.Sprintf("domain=%s provider_account=%d remote_record_deleted=false external_cleanup=not_attempted", record.DomainName, record.ProviderAccountID)
		} else {
			var record model.ManagedCertificate
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
				return removalError(err)
			}
			row, status = &record, record.Status
			action, target = "certificate.delete", fmt.Sprintf("certificate:%d", id)
			detail = fmt.Sprintf("node=%d external_cleanup=not_attempted", record.NodeID)
		}
		user, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrResourcePermission
			}
			return err
		}
		blockers, err := resourceRemovalBlockers(tx, kind, id, status)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return &network.ResourceRemovalBlocked{Blockers: blockers}
		}
		if kind == network.CertificateOperation {
			if err := tx.Where("managed_certificate_id = ?", id).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: action, Target: target, Detail: detail}).Error; err != nil {
			return err
		}
		return tx.Delete(row).Error
	})
}
func removalError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.ErrResourceNotFound
	}
	return err
}
func resourceRemovalBlockers(tx *gorm.DB, kind network.OperationKind, id uint, status string) (map[string]int64, error) {
	blockers := map[string]int64{}
	var operations *gorm.DB
	var resources []string
	if kind == network.DNSOperation {
		operations = tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ?", "dns_record", id)
		resources = []string{fmt.Sprintf("dns:%d", id), fmt.Sprintf("dns-inspection:%d", id)}
		if status == "syncing" {
			blockers["resource_active"] = 1
		}
	} else {
		operations = tx.Model(&model.CertificateOperation{}).Where("managed_certificate_id = ?", id)
		resources = []string{fmt.Sprintf("certificate:%d", id)}
		if status == "issuing" || status == "renewing" {
			blockers["resource_active"] = 1
		}
	}
	var operationsCount int64
	if err := operations.Where("status = ? OR EXISTS (?)", "running", unconfirmedOperationEvidence(tx, kind)).Count(&operationsCount).Error; err != nil {
		return nil, err
	}
	if operationsCount > 0 {
		blockers["unconfirmed_operations"] = operationsCount
	}
	var runs int64
	if err := tx.Model(&jobstore.Record{}).Where("owner = ? AND resource IN ? AND state NOT IN ?", "system", resources, []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Canceled}).Count(&runs).Error; err != nil {
		return nil, err
	}
	if runs > 0 {
		blockers["unverified_jobs"] = runs
	}
	return blockers, nil
}
