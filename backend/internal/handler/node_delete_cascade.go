package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type nodeDeleteCleanup struct {
	ProtocolEndpoints    int64 `json:"protocol_endpoints"`
	ProtocolDeployments  int64 `json:"protocol_deployments"`
	ProtocolCredentials  int64 `json:"protocol_credentials"`
	FlowUsage            int64 `json:"flow_usage"`
	NodeGroupLinks       int64 `json:"node_group_links"`
	CertificateLinks     int64 `json:"certificate_links"`
	ManagedCertificates  int64 `json:"managed_certificates"`
	CertificateOperations int64 `json:"certificate_operations"`
	ManagedDNSRecords    int64 `json:"managed_dns_records"`
	KernelOperations     int64 `json:"kernel_operations"`
	KernelState          int64 `json:"kernel_state"`
}

type nodeCascadeDeleteResponse struct {
	ID                             uint              `json:"id"`
	Deleted                        bool              `json:"deleted"`
	Cleanup                        nodeDeleteCleanup `json:"cleanup"`
	TrafficRecordsRetained         int64             `json:"traffic_records_retained"`
	RemoteZeroRetained             bool              `json:"remote_zero_retained"`
	RemoteCertificateFilesRetained bool              `json:"remote_certificate_files_retained"`
	ProviderDNSRecordsRetained     bool              `json:"provider_dns_records_retained"`
}

// NodeCascadeDeleteHandler treats a registered VPS as the lifecycle root for
// zboard-owned runtime/configuration resources. Historical billing, task and
// audit facts deliberately survive the asset deletion, while external state on
// the VPS/provider is never silently destroyed by deleting a panel record.
func (h *handlers) NodeCascadeDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	endpointIDs := make([]uint, 0)
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", node.ID).Pluck("id", &endpointIDs).Error; err != nil {
		ServerError(w, err)
		return
	}
	if blockers, err := h.nodeDeletionBlockers(node.ID, endpointIDs); err != nil {
		ServerError(w, err)
		return
	} else if len(blockers) > 0 {
		writeJSON(w, http.StatusConflict, "节点仍有正在执行的运维任务，请等待任务结束后再删除。", map[string]interface{}{"blockers": blockers})
		return
	}

	var trafficRecords int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("node_id = ?", node.ID).Count(&trafficRecords).Error; err != nil {
		ServerError(w, err)
		return
	}

	cleanup := nodeDeleteCleanup{}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var certificateIDs []uint
		if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", node.ID).Pluck("id", &certificateIDs).Error; err != nil {
			return err
		}

		var err error
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

		if err := tx.Delete(&node).Error; err != nil {
			return err
		}
		encodedCleanup, _ := json.Marshal(cleanup)
		return createAuditLog(tx, claims, "node.delete", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("name=%s cleanup=%s traffic_records_retained=%d external_state_retained=true", node.Name, encodedCleanup, trafficRecords))
	})
	if err != nil {
		ServerError(w, err)
		return
	}

	OK(w, nodeCascadeDeleteResponse{
		ID:                             node.ID,
		Deleted:                        true,
		Cleanup:                        cleanup,
		TrafficRecordsRetained:         trafficRecords,
		RemoteZeroRetained:             true,
		RemoteCertificateFilesRetained: true,
		ProviderDNSRecordsRetained:     true,
	})
}

func (h *handlers) nodeDeletionBlockers(nodeID uint, endpointIDs []uint) (map[string]int64, error) {
	blockers := map[string]int64{}
	checks := map[string]*gorm.DB{
		"kernel_operations":      h.db.Model(&model.NodeOperation{}).Where("node_id = ? AND status = ?", nodeID, "running"),
		"protocol_deployments":   h.db.Model(&model.ProtocolDeployment{}).Where("node_id = ? AND status = ?", nodeID, "running"),
		"certificate_operations": h.db.Model(&model.CertificateOperation{}).Where("node_id = ? AND status = ?", nodeID, "running"),
	}
	for name, query := range checks {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			blockers[name] = count
		}
	}

	targetNodeID := strconv.FormatUint(uint64(nodeID), 10)
	activeTasks := h.db.Table("tasks").Joins("JOIN task_items ON task_items.task_id = tasks.id").
		Where("tasks.status IN ?", []int16{taskStatusPending, taskStatusRunning}).
		Where("task_items.target_type = ? AND task_items.target_id = ?", "node", targetNodeID)
	if len(endpointIDs) > 0 {
		endpointTargets := make([]string, 0, len(endpointIDs))
		for _, endpointID := range endpointIDs {
			endpointTargets = append(endpointTargets, strconv.FormatUint(uint64(endpointID), 10))
		}
		activeTasks = h.db.Table("tasks").Joins("JOIN task_items ON task_items.task_id = tasks.id").
			Where("tasks.status IN ?", []int16{taskStatusPending, taskStatusRunning}).
			Where("(task_items.target_type = ? AND task_items.target_id = ?) OR (task_items.target_type = ? AND task_items.target_id IN ?)", "node", targetNodeID, "protocol_endpoint", endpointTargets)
	}
	var activeTaskCount int64
	if err := activeTasks.Distinct("tasks.id").Count(&activeTaskCount).Error; err != nil {
		return nil, err
	}
	if activeTaskCount > 0 {
		blockers["admin_tasks"] = activeTaskCount
	}
	return blockers, nil
}

func deleteNodeScopedRows(tx *gorm.DB, value interface{}, query string, args ...interface{}) (int64, error) {
	result := tx.Where(query, args...).Delete(value)
	return result.RowsAffected, result.Error
}
