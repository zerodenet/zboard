package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeRemoval struct{ DB *gorm.DB }

func (s NodeRemoval) RemoveNode(ctx context.Context, actor, id uint) (out network.NodeRemovalFacts, err error) {
	cleanup := network.NodeDeleteCleanup{}
	var trafficRecords int64
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, id).Error; err != nil {
			return removalError(err)
		}
		user, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrResourcePermission
			}
			return err
		}
		var endpointIDs []uint
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", id).Pluck("id", &endpointIDs).Error; err != nil {
			return err
		}
		blockers, err := nodeRemovalBlockers(tx, id, endpointIDs)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return &network.ResourceRemovalBlocked{Blockers: blockers}
		}
		if err := tx.Model(&model.TrafficRecord{}).Where("node_id = ?", id).Count(&trafficRecords).Error; err != nil {
			return err
		}

		var cleanupErr error
		if err := TouchEndpointGroups(tx, endpointIDs); err != nil {
			return err
		}
		cleanup.NetworkEntries, cleanupErr = RemoveTopologyEntries(tx, user.ID, TopologySelection{NodeID: node.ID})
		if cleanupErr != nil {
			return cleanupErr
		}

		var certificateIDs []uint
		if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", node.ID).Pluck("id", &certificateIDs).Error; err != nil {
			return err
		}

		if len(endpointIDs) > 0 {
			cleanup.NodeGroupLinks, err = deleteNodeScopedRows(tx, &model.NodeGroupEndpoint{}, "protocol_endpoint_id IN ?", endpointIDs)
			if err != nil {
				return err
			}
			cleanup.CertificateLinks, err = deleteNodeScopedRows(tx, &model.CertificateProtocolEndpoint{}, "protocol_endpoint_id IN ?", endpointIDs)
			if err != nil {
				return err
			}
		}
		if len(certificateIDs) > 0 {
			removed, err := deleteNodeScopedRows(tx, &model.CertificateProtocolEndpoint{}, "managed_certificate_id IN ?", certificateIDs)
			if err != nil {
				return err
			}
			cleanup.CertificateLinks += removed
		}

		cleanup.PrincipalFlowCurrents, cleanup.PrincipalFlowGeneration, err = meteringstore.RemoveNode(tx, node.ID)
		if err != nil {
			return err
		}

		if cleanup.ProtocolCredentials, err = deleteNodeScopedRows(tx, &model.ProtocolCredential{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.FlowUsage, err = deleteNodeScopedRows(tx, &model.FlowUsage{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.ProtocolDeployments, err = deleteNodeScopedRows(tx, &model.ProtocolDeployment{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.CertificateOperations, err = deleteNodeScopedRows(tx, &model.CertificateOperation{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.ManagedCertificates, err = deleteNodeScopedRows(tx, &model.ManagedCertificate{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.ManagedDNSRecords, err = deleteNodeScopedRows(tx, &model.ManagedDNSRecord{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.ProtocolEndpoints, err = deleteNodeScopedRows(tx, &model.ProtocolEndpoint{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.KernelOperations, err = deleteNodeScopedRows(tx, &model.NodeOperation{}, "node_id = ?", node.ID); err != nil {
			return err
		}
		if cleanup.KernelState, err = deleteNodeScopedRows(tx, &model.NodeKernelState{}, "node_id = ?", node.ID); err != nil {
			return err
		}

		if cleanup.ProxyPools, err = deleteNodeScopedRows(tx, &model.NodeProxyPool{}, "node_id = ?", node.ID); err != nil {
			return err
		}

		if err := tx.Delete(&node).Error; err != nil {
			return err
		}
		encodedCleanup, _ := json.Marshal(cleanup)
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "node.delete", Target: fmt.Sprintf("node:%d", node.ID), Detail: fmt.Sprintf("name=%s cleanup=%s traffic_records_retained=%d external_cleanup=not_attempted", node.Name, encodedCleanup, trafficRecords)}).Error
	})
	if err == nil {
		out = network.NodeRemovalFacts{Cleanup: cleanup, TrafficRecordsRetained: trafficRecords}
	}
	return
}
func deleteNodeScopedRows(tx *gorm.DB, value any, query string, args ...any) (int64, error) {
	result := tx.Where(query, args...).Delete(value)
	return result.RowsAffected, result.Error
}
