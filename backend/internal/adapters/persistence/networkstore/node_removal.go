package networkstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeRemoval struct {
	DB                   *gorm.DB
	FinalizingRetirement bool
}

// RemoveNode archives management, never historical identity. Remote changes are
// owned exclusively by the durable retirement plan, not an after-delete job.
func (s NodeRemoval) RemoveNode(ctx context.Context, actor, id uint) (out network.NodeRemovalFacts, err error) {
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			return network.ErrResourcePermission
		}
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, id).Error; err != nil {
			return removalError(err)
		}
		if node.LifecycleStatus == "archived" {
			return nil
		}
		if !s.FinalizingRetirement {
			var active int64
			if err := tx.Model(&model.NodeRetirement{}).Where("node_id = ? AND state <> ?", id, "succeeded").Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				return &network.ResourceRemovalBlocked{Blockers: map[string]int64{"node_retirement": active}}
			}
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
		if err := TouchEndpointGroups(tx, endpoints); err != nil {
			return err
		}
		out.Cleanup.NetworkEntries, err = RemoveTopologyEntries(tx, admin.ID, TopologySelection{NodeID: id})
		if err != nil {
			return err
		}
		if len(endpoints) > 0 {
			out.Cleanup.NodeGroupLinks, err = deleteNodeScopedRows(tx, &model.NodeGroupEndpoint{}, "protocol_endpoint_id IN ?", endpoints)
			if err != nil {
				return err
			}
			if err := tx.Model(&model.ProtocolEndpoint{}).Where("id IN ?", endpoints).Update("is_active", false).Error; err != nil {
				return err
			}
		}
		out.Cleanup.PrincipalFlowCurrents, out.Cleanup.PrincipalFlowGeneration, err = meteringstore.RemoveNode(tx, id)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", id).Update("auto_renew", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.NodeProxyPool{}).Where("node_id = ?", id).Updates(map[string]any{"auto_sync": false, "subscription_url": "", "config": "{}", "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.NodeConfigPublish{}).Where("node_id = ?", id).Update("suspended", true).Error; err != nil {
			return err
		}
		if err := observationstore.Tombstone(tx, id, node.AssetTag); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&node).Updates(map[string]any{
			"lifecycle_status": "archived", "archived_at": now, "is_enabled": false, "is_online": false,
			"node_credential": "", "node_credential_prefix": "", "node_credential_revoked_at": now,
			"traffic_secret": "", "traffic_secret_prefix": "", "traffic_secret_revoked_at": now,
			"ssh_pwd": "", "ssh_private_key_passphrase": "", "ssh_privilege_password": "", "ssh_verified_at": nil,
			"config": "{}",
		}).Error; err != nil {
			return err
		}
		detail, _ := json.Marshal(out.Cleanup)
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node.archive", Target: fmt.Sprintf("node:%d", id), Detail: fmt.Sprintf("name=%s cleanup=%s history_preserved=true external_cleanup=retirement_plan", node.Name, detail)}).Error
	})
	return
}
func deleteNodeScopedRows(tx *gorm.DB, value any, query string, args ...any) (int64, error) {
	result := tx.Where(query, args...).Delete(value)
	return result.RowsAffected, result.Error
}
