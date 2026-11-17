package networkstore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProtocolEndpointRemoval struct{ DB *gorm.DB }

func (s ProtocolEndpointRemoval) RemoveProtocolEndpoint(ctx context.Context, actor, id uint, now time.Time) (out network.ProtocolEndpointRemovalFacts, err error) {
	if s.DB == nil {
		return out, network.ErrProtocolEndpointRemovalUnavailable
	}
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var endpoint model.ProtocolEndpoint
		admin, err := deletionAdministrator(tx, actor)
		if err != nil {
			return err
		}
		if err := tx.Unscoped().First(&endpoint, id).Error; err != nil {
			return removalError(err)
		}
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, endpoint.NodeID).Error; err != nil {
			return removalError(err)
		}
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, id).Error; err != nil {
			return removalError(err)
		}
		check, err := protocolDeletionCheck(tx, endpoint, node, true)
		if err != nil {
			return err
		}
		if check.AlreadyDeleted {
			out = network.ProtocolEndpointRemovalFacts{ID: endpoint.ID, AlreadyDeleted: true}
			return nil
		}
		if len(check.Blockers) > 0 {
			cause := network.ErrProtocolEndpointRemovalConflict
			if network.NodeManagementClosed(node.LifecycleStatus) {
				cause = network.ErrProtocolEndpointResourceDeleting
			}
			return &network.DeletionBlocked{Check: check, Cause: cause}
		}
		if err := TouchEndpointGroups(tx, []uint{endpoint.ID}); err != nil {
			return err
		}
		if _, err := RemoveTopologyEntries(tx, admin.ID, TopologySelection{EndpointID: endpoint.ID}); err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{
			UserID: &admin.ID, Actor: admin.Email, Action: "protocol_endpoint.delete",
			Target: fmt.Sprintf("protocol_endpoint:%d", endpoint.ID),
			Detail: fmt.Sprintf("node=%d protocol=%s was_active=%t", endpoint.NodeID, endpoint.Protocol, endpoint.IsActive),
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("protocol_endpoint_id = ?", endpoint.ID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
			return err
		}
		if err := tx.Where("protocol_endpoint_id = ?", endpoint.ID).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("protocol_endpoint_id = ? AND revoked_at IS NULL", endpoint.ID).
			Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
			return err
		}
		// Other independently configured protocols survive; remove only their
		// parent association, matching the former FK SET NULL without deleting us.
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("parent_protocol_id = ?", endpoint.ID).Update("parent_protocol_id", nil).Error; err != nil {
			return err
		}
		// Keep the identity and all financial/deployment foreign keys intact.
		if err := tx.Model(&endpoint).Updates(map[string]any{"is_active": false, "deleted_at": now}).Error; err != nil {
			return err
		}
		if err := EnqueuePublication(tx, endpoint.NodeID, 0, admin.ID); err != nil {
			return err
		}
		out = network.ProtocolEndpointRemovalFacts{ID: endpoint.ID, NodeID: endpoint.NodeID, Protocol: endpoint.Protocol, WasActive: endpoint.IsActive}
		return nil
	})
	if err != nil {
		return network.ProtocolEndpointRemovalFacts{}, err
	}
	return out, nil
}
