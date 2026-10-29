package datastore

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type principalRuntimeDigestHead struct {
	ID             uint64
	BodyBytes      int64
	DigestBytes    int64
	DigestMismatch bool
}

const principalRuntimeDigestHeadSQL = "id,OCTET_LENGTH(core_instance_id) AS body_bytes,OCTET_LENGTH(core_instance_sha256) AS digest_bytes"

// Keep both the row count and the source bytes bounded. MySQL computes SHA256
// over the original bytes while their exact rows are locked; opaque identities
// never need to cross the connection. Existing conflicting digests still fail
// the whole batch instead of being overwritten.
func backfillMySQLPrincipalRuntimeDigests(db *gorm.DB) error {
	var cursor uint64
	for {
		var heads []principalRuntimeDigestHead
		if err := db.Table(principalRuntimeTable).Select(principalRuntimeDigestHeadSQL).
			Where("id > ?", cursor).Order("id").Limit(512).Find(&heads).Error; err != nil {
			return err
		}
		if len(heads) == 0 {
			return nil
		}
		var sourceBytes int64
		ids := make([]uint64, 0, len(heads))
		for _, head := range heads {
			if err := validatePrincipalRuntimeDigestHead(head); err != nil {
				return err
			}
			if sourceBytes+head.BodyBytes > auxiliaryRuntimeCopyBatchBytes {
				break
			}
			ids = append(ids, head.ID)
			sourceBytes += head.BodyBytes
		}
		heads = heads[:len(ids)]
		if err := db.Transaction(func(tx *gorm.DB) error {
			var locked []principalRuntimeDigestHead
			mismatchSQL := ",CASE WHEN core_instance_sha256 = '' THEN 0 WHEN BINARY core_instance_sha256 = BINARY SHA2(core_instance_id,256) THEN 0 ELSE 1 END AS digest_mismatch"
			if err := tx.Table(principalRuntimeTable).Clauses(clause.Locking{Strength: "UPDATE"}).
				Select(principalRuntimeDigestHeadSQL+mismatchSQL).Where("id IN ?", ids).Order("id").Find(&locked).Error; err != nil {
				return err
			}
			if len(locked) != len(heads) {
				return errors.New("principal runtime source changed during backfill; retain original evidence")
			}
			var missing int64
			for i, row := range locked {
				if row.ID != heads[i].ID || row.BodyBytes != heads[i].BodyBytes || row.DigestBytes != heads[i].DigestBytes {
					return errors.New("principal runtime source changed during backfill; retain original evidence")
				}
				if err := validatePrincipalRuntimeDigestHead(row); err != nil {
					return err
				}
				if row.DigestMismatch {
					return errors.New("principal runtime raw/digest mismatch; retain original evidence")
				}
				if row.DigestBytes == 0 {
					missing++
				}
			}
			if missing == 0 {
				return nil
			}
			r := tx.Table(principalRuntimeTable).Where("id IN ? AND core_instance_sha256 = ''", ids).
				UpdateColumn("core_instance_sha256", gorm.Expr("SHA2(core_instance_id,256)"))
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != missing {
				return errors.New("principal runtime digest backfill count differs; retain original evidence")
			}
			return nil
		}); err != nil {
			return err
		}
		cursor = ids[len(ids)-1]
	}
}

func validatePrincipalRuntimeDigestHead(head principalRuntimeDigestHead) error {
	if head.DigestBytes != 0 && head.DigestBytes != 64 {
		return errors.New("principal runtime digest has invalid byte length; retain original evidence")
	}
	if head.BodyBytes < 0 || head.BodyBytes > metering.MaxRuntimeIdentityBytes {
		return errors.New("principal runtime backfill source exceeds auxiliary identity budget")
	}
	return nil
}
