package credentialstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

var ErrPrincipalIndexPending = errors.New("credential principal index is incomplete or ambiguous")

const principalBackfillBatch = 256
const principalBackfillBatches = 4

type PrincipalIndex struct {
	DB     *gorm.DB
	Cipher metering.CredentialDecryptor
}

// Lookup uses only node IDs and node-scoped digests in SQL. Legacy projection
// recovery commits bounded batches independently of the later authorization
// transaction, so restart/cancellation cannot lose successful backfill work.
func (s PrincipalIndex) Lookup(ctx context.Context, node uint, provided string) (model.ProtocolCredential, error) {
	if s.DB == nil || node == 0 || provided == "" {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	if err := s.Prepare(ctx, node); err != nil {
		return model.ProtocolCredential{}, err
	}
	return s.LookupPrepared(ctx, node, provided)
}

// Prepare commits bounded legacy projection work before the accounting
// transaction. An incomplete node stays pending while the next retry resumes.
func (s PrincipalIndex) Prepare(ctx context.Context, node uint) error {
	if s.DB == nil || node == 0 {
		return ErrPrincipalIndexPending
	}
	for batch := 0; batch < principalBackfillBatches; batch++ {
		count, err := s.backfill(ctx, node)
		if err != nil {
			return err
		}
		if count < principalBackfillBatch {
			break
		}
	}
	var remaining []uint
	if err := s.DB.WithContext(ctx).Model(&model.ProtocolCredential{}).Table(principalIndexTable(s.DB, "idx_credential_lookup_backfill")).Where("node_id = ? AND principal_lookup_version = ?", node, 0).Limit(1).Pluck("id", &remaining).Error; err != nil {
		return err
	}
	if len(remaining) > 0 {
		return ErrPrincipalIndexPending
	}
	return nil
}

// LookupPrepared never writes or starts a nested backfill transaction. Callers
// must prepare this node before entering their authority transaction.
func (s PrincipalIndex) LookupPrepared(ctx context.Context, node uint, provided string) (model.ProtocolCredential, error) {
	if s.DB == nil || node == 0 || provided == "" {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	var remaining []uint
	if err := s.DB.WithContext(ctx).Model(&model.ProtocolCredential{}).Table(principalIndexTable(s.DB, "idx_credential_lookup_backfill")).Where("node_id = ? AND principal_lookup_version = ?", node, 0).Limit(1).Pluck("id", &remaining).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	if len(remaining) > 0 {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}

	digest := entitlements.RetiredPrincipalHash(node, provided)
	var ids []uint
	query := "SELECT id FROM " + principalIndexTable(s.DB, "idx_credential_principal_digest") + " WHERE node_id = ? AND principal_digest = ? UNION SELECT id FROM " + principalIndexTable(s.DB, "idx_credential_secret_digest") + " WHERE node_id = ? AND secret_principal_digest = ? UNION SELECT id FROM " + principalIndexTable(s.DB, "idx_credential_retired_principal") + " WHERE node_id = ? AND retired_principal_hash = ? LIMIT 2"

	if err := s.DB.WithContext(ctx).Raw(query, node, digest, node, digest, node, digest).Scan(&ids).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	if len(ids) == 0 {
		return model.ProtocolCredential{}, gorm.ErrRecordNotFound
	}
	if len(ids) != 1 {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	remaining = nil
	// An unreadable secret on a different credential could conceal a collision.
	// Do not choose an arbitrary billable/closable identity in damaged old data.
	if err := s.DB.WithContext(ctx).Model(&model.ProtocolCredential{}).Table(principalIndexTable(s.DB, "idx_credential_lookup_backfill")).Where("node_id = ? AND principal_lookup_version = ? AND id <> ?", node, 2, ids[0]).Limit(1).Pluck("id", &remaining).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	if len(remaining) > 0 {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	var row model.ProtocolCredential
	err := s.DB.WithContext(ctx).Where("node_id = ?", node).First(&row, ids[0]).Error
	return row, err
}

func (s PrincipalIndex) backfill(ctx context.Context, node uint) (count int, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.ProtocolCredential
		if err := tx.Table(principalIndexTable(tx, "idx_credential_lookup_backfill")).Select("id", "node_id", "principal_key", "secret", "retired_principal_hash").Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND principal_lookup_version = ?", node, 0).Order("id").Limit(principalBackfillBatch).Find(&rows).Error; err != nil {
			return err
		}
		count = len(rows)
		if count == 0 {
			return nil
		}
		for i := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			row := &rows[i]
			row.PrincipalDigest = entitlements.RetiredPrincipalHash(node, row.PrincipalKey)
			row.SecretPrincipalDigest = row.RetiredPrincipalHash
			row.PrincipalLookupVersion = 1
			if row.Secret != "" {
				if s.Cipher == nil {
					row.PrincipalLookupVersion = 2
				} else {
					plain, err := s.Cipher.Decrypt(row.Secret)
					if err != nil {
						row.PrincipalLookupVersion = 2
					} else {
						row.SecretPrincipalDigest = entitlements.RetiredPrincipalHash(node, plain)
					}
				}
			}
		}

		// One bounded UPDATE of projection columns only. Canonical ciphertext never
		// becomes an INSERT/UPSERT parameter or gets rewritten by index recovery.
		var sql strings.Builder
		var args []any
		sql.WriteString("UPDATE protocol_credentials SET ")
		for field, column := range []string{"principal_digest", "secret_principal_digest", "principal_lookup_version"} {
			if field > 0 {
				sql.WriteString(",")
			}
			sql.WriteString(column + " = CASE id")
			for _, row := range rows {
				var value any = row.PrincipalDigest
				if field == 1 {
					value = row.SecretPrincipalDigest
				}
				if field == 2 {
					value = row.PrincipalLookupVersion
				}
				sql.WriteString(" WHEN ? THEN ?")
				args = append(args, row.ID, value)
			}
			sql.WriteString(" ELSE " + column + " END")
		}
		sql.WriteString(" WHERE node_id = ? AND principal_lookup_version = 0 AND id IN (")
		args = append(args, node)
		for i, row := range rows {
			if i > 0 {
				sql.WriteString(",")
			}
			sql.WriteString("?")
			args = append(args, row.ID)
		}
		sql.WriteString(")")
		return tx.Exec(sql.String(), args...).Error

	})
	return
}

// The lookup must not depend on stale optimizer cardinality estimates choosing
// the node-only index on a node with a long retained credential history.
func principalIndexTable(db *gorm.DB, index string) string {
	if db.Dialector.Name() == "sqlite" {
		return "protocol_credentials INDEXED BY " + index
	}
	return "protocol_credentials FORCE INDEX (" + index + ")"
}
