package networkstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DNSReconciliation struct{ DB *gorm.DB }

func (s DNSReconciliation) LoadDNSObservationCredential(ctx context.Context, scope network.DNSReconciliationScope) (string, error) {
	var account model.ProviderAccount
	err := s.DB.WithContext(ctx).Select("credential_ciphertext").
		Where("id = ? AND revision = ? AND status = ? AND provider_key = ?", scope.AccountID, scope.AccountRevision, "active", "cloudflare").Take(&account).Error
	return account.CredentialCiphertext, err
}

func (s DNSReconciliation) LoadDNSReconciliation(ctx context.Context, actor uint, runID string) (out network.DNSReconciliationScope, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { var err error; out, err = loadDNSReconciliation(tx, actor, runID); return err })
	return
}
func loadDNSReconciliation(tx *gorm.DB, actor uint, runID string) (network.DNSReconciliationScope, error) {
	var out network.DNSReconciliationScope
	var user model.User
	if actor == 0 {
		return out, jobs.ErrPermission
	}
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").First(&user).Error; err != nil {
		return out, jobs.ErrPermission
	}
	var run jobstore.Record
	if err := tx.Where("id = ? AND owner = ? AND handler = ? AND state = ?", runID, "system", "dns_operation", jobs.Unknown).First(&run).Error; err != nil {
		return out, network.ErrDNSChanged
	}
	var input struct {
		OperationID uint `json:"operation_id"`
	}
	if json.Unmarshal([]byte(run.Payload), &input) != nil || input.OperationID == 0 || run.Key != fmt.Sprintf("dns_operation:%d", input.OperationID) {
		return out, network.ErrDNSChanged
	}
	var op model.ProviderOperation
	if err := tx.Where("id = ? AND resource_type = ?", input.OperationID, "dns_record").First(&op).Error; err != nil {
		return out, err
	}
	var record model.ManagedDNSRecord
	if err := tx.First(&record, op.ResourceID).Error; err != nil {
		return out, err
	}
	if run.Resource != fmt.Sprintf("dns:%d", record.ID) || record.Status == "deleting" || record.ProviderAccountID != op.ProviderAccountID {
		return out, network.ErrDNSChanged
	}
	var account model.ProviderAccount
	if err := tx.First(&account, record.ProviderAccountID).Error; err != nil {
		return out, err
	}
	if account.Status != "active" || account.ProviderKey != "cloudflare" {
		return out, network.ErrDNSUnverified
	}
	out = network.DNSReconciliationScope{RunID: runID, Reviewer: actor, OperationID: op.ID, RecordID: record.ID, AccountID: account.ID, Revision: record.Revision, AccountRevision: account.Revision, ZoneID: record.ProviderZoneID, RemoteID: record.ProviderRecordID, Domain: record.DomainName, RecordType: record.RecordType, DesiredHash: record.DesiredHash}
	return out, nil
}
func (s DNSReconciliation) CompleteDNSReconciliation(ctx context.Context, before network.DNSReconciliationScope, observation network.DNSObservation) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		// Lock mutable authority before comparing the observation scope.
		for _, row := range []struct {
			value any
			id    uint
		}{{&model.ProviderAccount{}, before.AccountID}, {&model.ManagedDNSRecord{}, before.RecordID}, {&model.ProviderOperation{}, before.OperationID}} {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(row.value, row.id).Error; err != nil {
				return err
			}
		}
		current, err := loadDNSReconciliation(tx, before.Reviewer, before.RunID)
		if err != nil {
			return err
		}
		if current != before || observation.Hash != current.DesiredHash || observation.RemoteID != current.RemoteID || observation.ZoneID != current.ZoneID {
			return network.ErrDNSChanged
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.ManagedDNSRecord{}).Where("id = ?", before.RecordID).Updates(map[string]any{"status": "active", "observed_hash": observation.Hash, "last_synced_at": now, "last_error": ""}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProviderOperation{}).Where("id = ?", before.OperationID).Updates(map[string]any{"status": "succeeded", "phase": "reconciled", "finished_at": now, "error": "", "result_summary": "read-only provider inspection verified current desired DNS state"}).Error; err != nil {
			return err
		}
		return jobstore.New(tx).RecordVerifiedOutcome(ctx, jobs.Reviewer{AccountID: before.Reviewer}, jobs.Review{RunID: before.RunID, Outcome: jobs.Succeeded, Reason: "read-only provider inspection matched the stored remote identity and desired DNS state"})
	})
}
