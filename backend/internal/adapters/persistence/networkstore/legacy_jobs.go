package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecoverLegacyNative imports only unfinished domain operations missing their
// common Run. It does not call a provider, change domain state, or replay work.
// Keyset pages bound reads; each operation and its import audit commit together.
type Recovery struct{ DB *gorm.DB }

func (s Recovery) RecoverLegacyNative(ctx context.Context) (int, error) {
	imported := 0
	for _, kind := range []string{"certificate_operation", "dns_operation"} {
		var cursor uint
		for {
			var ids []uint
			q := s.DB.WithContext(ctx)
			if kind == "certificate_operation" {
				q = q.Model(&model.CertificateOperation{})
			} else {
				q = q.Model(&model.ProviderOperation{}).Where("resource_type = ?", "dns_record")
			}
			if err := q.Where("id > ? AND status = ?", cursor, "running").Order("id").Limit(100).Pluck("id", &ids).Error; err != nil {
				return imported, err
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				didImport := false
				err := jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
					var resource uint
					var created time.Time
					var status string
					if kind == "certificate_operation" {
						var op model.CertificateOperation
						if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&op, id).Error; err != nil {
							if errors.Is(err, gorm.ErrRecordNotFound) {
								return nil
							}
							return err
						}
						resource, created, status = op.ManagedCertificateID, op.CreatedAt, op.Status
					} else {
						var op model.ProviderOperation
						if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&op, id).Error; err != nil {
							if errors.Is(err, gorm.ErrRecordNotFound) {
								return nil
							}
							return err
						}
						resource, created, status = op.ResourceID, op.CreatedAt, op.Status
					}
					if status != "running" {
						return nil
					}
					payload, _ := json.Marshal(struct {
						Revision    string `json:"revision"`
						OperationID uint   `json:"operation_id"`
					}{"1", id})
					prefix, timeout := "certificate", 30*time.Minute
					if kind == "dns_operation" {
						prefix, timeout = "dns", time.Minute
					}
					runID, inserted, err := jobstore.New(tx).ImportUnknown(ctx, jobs.Submission{Owner: "system", Key: fmt.Sprintf("%s:%d", kind, id), Handler: kind, ExecutionGroup: "external", Resource: fmt.Sprintf("%s:%d", prefix, resource), Payload: string(payload), Timeout: timeout}, created)
					if err != nil {
						return err
					}
					if !inserted {
						return nil
					}
					if err := tx.Create(&model.AuditLog{Actor: "system", Action: "job.legacy.import", Target: "job:" + runID, Detail: fmt.Sprintf("%s:%d imported as unknown; external outcome has not been verified", kind, id)}).Error; err != nil {
						return err
					}
					didImport = true
					return nil
				})
				if err != nil {
					return imported, err
				}
				if didImport {
					imported++
				}
				cursor = id
			}
		}
	}
	return imported, nil
}
