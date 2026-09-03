package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Deletion intent survives network errors and restarts. Only DELETE may resume
// it; ordinary edits and background workers must never recreate removed assets.
const resourceStatusDeleting = "deleting"

var errResourceDeleting = errors.New("资源已进入删除流程，请修复外部连接后重试删除")

func requireAvailableNode(tx *gorm.DB, id uint) error {
	var node model.Node
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, id).Error; err != nil {
		return err
	}
	if node.LifecycleStatus == resourceStatusDeleting {
		return errResourceDeleting
	}
	return nil
}

func requireAvailableProvider(tx *gorm.DB, id uint) error {
	var account model.ProviderAccount
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, id).Error
}

func (h *handlers) deleteManagedDNSRemote(ctx context.Context, record model.ManagedDNSRecord) (bool, error) {
	if record.ProviderZoneID == "" && record.ProviderRecordID == "" {
		// Only an untouched desired record is known not to own anything remotely.
		var attempts int64
		if err := h.db.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND phase IN ?", "dns_record", record.ID, []string{"applying_record", "persisting", "completed"}).Count(&attempts).Error; err != nil {
			return false, err
		}
		if attempts > 0 || record.LastSyncedAt != nil || record.ObservedHash != "" {
			return false, errors.New("DNS 远端标识缺失且可能已写入供应商，请先核对并恢复准确标识")
		}
		return false, nil
	}
	if record.ProviderZoneID == "" || record.ProviderRecordID == "" {
		return false, errors.New("DNS 远端标识不完整，无法确认删除目标")
	}
	var account model.ProviderAccount
	if err := h.db.First(&account, record.ProviderAccountID).Error; err != nil {
		return false, err
	}
	if account.ProviderKey != providerCloudflare {
		return false, errors.New("unsupported DNS provider")
	}
	token, err := h.credentialCipher.Decrypt(account.CredentialCiphertext)
	if err != nil {
		return false, err
	}
	if err := deleteCloudflareDNSRecord(ctx, token, record.ProviderZoneID, record.ProviderRecordID); err != nil && !cloudflareRecordAlreadyAbsent(err) {
		return false, err
	}
	return true, nil
}

func (h *handlers) prepareNodeExternalDeletion(nodeID uint) error {
	return h.db.Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, nodeID).Error; err != nil {
			return err
		}
		for _, query := range []*gorm.DB{
			tx.Model(&model.NodeOperation{}).Where("node_id = ? AND status = ?", nodeID, "running"),
			tx.Model(&model.ProtocolDeployment{}).Where("node_id = ? AND status = ?", nodeID, "running"),
			tx.Model(&model.CertificateOperation{}).Where("node_id = ? AND status = ?", nodeID, "running"),
		} {
			var running int64
			if err := query.Count(&running).Error; err != nil {
				return err
			}
			if running > 0 {
				return errors.New("节点仍有正在运行的运维任务")
			}
		}
		var records []model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).Find(&records).Error; err != nil {
			return err
		}
		for _, record := range records {
			var running int64
			if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", record.ID, "running").Count(&running).Error; err != nil {
				return err
			}
			if running > 0 || record.Status == dnsStatusSyncing {
				return errManagedDNSOperationRunning
			}
		}
		var certificates []model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).Find(&certificates).Error; err != nil {
			return err
		}
		for _, certificate := range certificates {
			if certificate.Status == certificateStatusIssuing || certificate.Status == certificateStatusRenewing {
				return errCertificateOperationRunning
			}
		}
		if err := tx.Model(&node).Updates(map[string]interface{}{"lifecycle_status": resourceStatusDeleting, "is_enabled": false}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", nodeID).Update("status", resourceStatusDeleting).Error; err != nil {
			return err
		}
		return tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{"status": resourceStatusDeleting, "auto_renew": false}).Error
	})
}

func (h *handlers) cleanupNodeExternalResources(ctx context.Context, node model.Node) error {
	var records []model.ManagedDNSRecord
	if err := h.db.Where("node_id = ?", node.ID).Order("id").Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		if _, err := h.deleteManagedDNSRemote(ctx, record); err != nil {
			h.failManagedDNSDeletion(record.ID, err)
			return fmt.Errorf("DNS %d: %w", record.ID, err)
		}
	}
	var certificates []model.ManagedCertificate
	if err := h.db.Where("node_id = ?", node.ID).Order("id").Find(&certificates).Error; err != nil {
		return err
	}
	for _, certificate := range certificates {
		if err := h.removeManagedCertificateRemote(ctx, node, certificate); err != nil {
			_ = h.db.Model(&certificate).Update("last_error", truncateCertificateError(err.Error())).Error
			return fmt.Errorf("certificate %d: %w", certificate.ID, err)
		}
	}
	return nil
}
