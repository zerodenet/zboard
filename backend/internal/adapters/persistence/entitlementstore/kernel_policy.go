package entitlementstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Prepared before rendering, under the same subscription lock as entitlement
// mutations. Traffic observations never advance this monotonic policy identity.
func reconcileKernelPolicy(ctx context.Context, db *gorm.DB, subscriptionID uint) error {
	return RunCredentialTransaction(ctx, db, func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, subscriptionID).Error; err != nil {
			return err
		}
		var credentials []struct {
			ID        uint
			Endpoint  uint
			Status    string
			ExpiresAt time.Time
		}
		if err := tx.Table("protocol_credentials").Select("protocol_credentials.id, protocol_endpoint_id AS endpoint, status, expires_at").Where("subscription_id = ? AND status IN ? AND revoked_at IS NULL", sub.ID, []string{"active", "prepared"}).Order("id asc").Scan(&credentials).Error; err != nil {
			return err
		}
		data, err := json.Marshal(struct {
			Group          uint
			Status         string
			End            time.Time
			Total          int64
			Speed, Devices int
			Credentials    any
		}{sub.NodeGroupID, sub.Status, sub.EndAt, sub.FlowTotal, sub.SpeedLimitMbps, sub.DeviceLimit, credentials})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		fingerprint := hex.EncodeToString(digest[:])
		if fingerprint == sub.KernelPolicyFingerprint && sub.KernelPolicyRevision > 0 {
			return nil
		}
		revision := sub.KernelPolicyRevision
		if revision == 0 && sub.UpdatedAt.UnixMilli() > 0 {
			revision = uint64(sub.UpdatedAt.UnixMilli())
		}
		if revision == math.MaxUint64 {
			return fmt.Errorf("subscription kernel policy revision exhausted")
		}
		// UpdateColumns deliberately does not touch the business UpdatedAt.
		return tx.Model(&sub).UpdateColumns(map[string]any{"kernel_policy_revision": revision + 1, "kernel_policy_fingerprint": fingerprint}).Error
	})
}
