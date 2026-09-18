package networkstore

import (
	"fmt"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func nodeRemovalBlockers(tx *gorm.DB, nodeID uint, endpointIDs []uint) (map[string]int64, error) {
	blockers := map[string]int64{}
	dnsIDs := tx.Model(&model.ManagedDNSRecord{}).Select("id").Where("node_id = ?", nodeID)
	certIDs := tx.Model(&model.ManagedCertificate{}).Select("id").Where("node_id = ?", nodeID)
	queries := map[string]*gorm.DB{
		"kernel_operations":      tx.Model(&model.NodeOperation{}).Where("node_id = ? AND status = ?", nodeID, "running"),
		"protocol_deployments":   tx.Model(&model.ProtocolDeployment{}).Where("node_id = ? AND status = ?", nodeID, "running"),
		"certificate_operations": tx.Model(&model.CertificateOperation{}).Where("node_id = ? OR managed_certificate_id IN (?)", nodeID, certIDs).Where("status = ? OR EXISTS (?)", "running", unconfirmedOperationEvidence(tx, network.CertificateOperation)),
		"dns_operations":         tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id IN (?)", "dns_record", dnsIDs).Where("status = ? OR EXISTS (?)", "running", unconfirmedOperationEvidence(tx, network.DNSOperation)),
		"active_dns":             tx.Model(&model.ManagedDNSRecord{}).Where("node_id = ? AND status = ?", nodeID, "syncing"),
		"active_certificates":    tx.Model(&model.ManagedCertificate{}).Where("node_id = ? AND status IN ?", nodeID, []string{"issuing", "renewing"}),
	}
	targets := make([]string, 0, len(endpointIDs))
	for _, id := range endpointIDs {
		targets = append(targets, strconv.FormatUint(uint64(id), 10))
	}
	queries["admin_tasks"] = tx.Table("tasks").Joins("JOIN task_items ON task_items.task_id = tasks.id").Where("tasks.status IN ? OR task_items.status = ?", []int16{0, 1}, 1).Where("(task_items.target_type = ? AND task_items.target_id = ?) OR (task_items.target_type = ? AND task_items.target_id IN ?)", "node", strconv.FormatUint(uint64(nodeID), 10), "protocol_endpoint", targets).Distinct("tasks.id")
	concat := func(prefix string, model any) *gorm.DB {
		expression := "(? || id)"
		if tx.Dialector.Name() == "mysql" {
			expression = "CONCAT(?, id)"
		}
		return tx.Model(model).Select(expression, prefix).Where("node_id = ?", nodeID)
	}
	queries["unverified_jobs"] = tx.Model(&jobstore.Record{}).Where("owner = ? AND state NOT IN ?", "system", []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Canceled}).Where("resource IN ? OR resource IN (?) OR resource IN (?) OR resource IN (?)", []string{fmt.Sprintf("node:%d", nodeID), fmt.Sprintf("node-publish:%d", nodeID)}, concat("dns:", &model.ManagedDNSRecord{}), concat("dns-inspection:", &model.ManagedDNSRecord{}), concat("certificate:", &model.ManagedCertificate{}))
	for name, q := range queries {
		var count int64
		if err := q.Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			blockers[name] = count
		}
	}
	// Freeze the durable publication lease until deletion commits. A nonempty
	// token stays a blocker even after its deadline; expiry alone proves no result.
	var publications []model.NodeConfigPublish
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).Find(&publications).Error; err != nil {
		return nil, err
	}
	for _, publication := range publications {
		if publication.LeaseToken != "" {
			blockers["node_publication"]++
		}
	}
	return blockers, nil
}
