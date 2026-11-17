package entitlementstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProcessCompatibility handles only a bounded page of due legacy credentials.
// Auth removal and key erasure are separate phases. A failed/offline node keeps
// the old key recoverable until its exact applied manifest excludes that key.
func (s CredentialMigration) ProcessCompatibility(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("credential compatibility accepts 1..200 rows")
	}
	var candidates []model.ProtocolCredential
	if e := s.DB.WithContext(ctx).Where("material_version=0 AND compatibility_until<=? AND secret<>'' AND status IN ?", now, []string{"active", "prepared", "revoked"}).Where("status<>'revoked' OR updated_at<=?", now.Add(-time.Minute)).Order("id").Limit(limit).Find(&candidates).Error; e != nil {
		return 0, e
	}
	cipher, ok := s.Issuer.(subscriptionMaterialIssuer)
	if !ok {
		return 0, entitlements.ErrCredentialMaterial
	}
	processed := 0
	for _, candidate := range candidates {
		e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var sub model.Subscription
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", candidate.SubscriptionID).Limit(1).Find(&sub).Error; e != nil {
				return e
			}
			var row model.ProtocolCredential
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, candidate.ID).Error; e != nil {
				return e
			}
			if row.Secret == "" || row.CompatibilityUntil == nil || row.CompatibilityUntil.After(now) {
				return nil
			}
			if row.Status == "active" || row.Status == "prepared" {
				if e := networkstore.EnqueuePublication(tx, row.NodeID, row.ProtocolEndpointID, 0); e != nil {
					return e
				}
				return tx.Model(&row).Updates(map[string]any{"status": "revoked", "revoked_at": now, "updated_at": now.UTC()}).Error
			}
			var state model.NodeCredentialDeliveryState
			if e := tx.Where("node_id=?", row.NodeID).Limit(1).Find(&state).Error; e != nil {
				return e
			}
			if state.DeploymentID == 0 || row.RevokedAt == nil || !state.ConfirmedAt.After(*row.RevokedAt) {
				return tx.Model(&row).UpdateColumn("updated_at", now).Error
			}
			var applied model.ProtocolDeployment
			if e := tx.Where("id=? AND node_id=? AND status='succeeded' AND applied_config_sha256=?", state.DeploymentID, row.NodeID, state.ConfigSHA256).Limit(1).Find(&applied).Error; e != nil {
				return e
			}
			manifest, e := network.DecodeConfigurationDeliveryManifest(applied.CredentialTargets)
			if e != nil {
				return tx.Model(&row).UpdateColumn("updated_at", now).Error
			}
			for _, target := range manifest.Targets {
				if target.CredentialID == row.ID {
					return tx.Model(&row).UpdateColumn("updated_at", now).Error
				}
			}
			plain, e := cipher.Decrypt(row.Secret)
			if e != nil {
				return e
			}
			// The retained legacy ID/principal/hash is a finite migration alias,
			// not a renewable service or an independently usable credential.
			return tx.Model(&row).Updates(map[string]any{"secret": "", "retired_principal_hash": entitlements.RetiredPrincipalHash(row.NodeID, plain), "current_scope": nil, "listen_port": 0, "public_port": 0, "compatibility_until": nil, "updated_at": now.UTC()}).Error
		})
		if e != nil {
			return processed, e
		}
		processed++
	}
	return processed, nil
}
