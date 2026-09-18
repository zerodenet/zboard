package handler

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Deletion intent survives network errors and restarts. Only DELETE may resume
// it; ordinary edits and background workers must never recreate removed assets.
const resourceStatusDeleting = "deleting"

var errResourceDeleting = errors.New("资源已进入删除流程，请等待删除完成或重试删除")

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
	prepared, err := h.services.DNSDeletion.Prepare(ctx, network.DNSDeletionInput{RecordID: record.ID, ProviderAccountID: record.ProviderAccountID,
		ProviderZoneID: record.ProviderZoneID, ProviderRecordID: record.ProviderRecordID, ObservedHash: record.ObservedHash, LastSyncedAt: record.LastSyncedAt})
	if errors.Is(err, network.ErrDNSDeletionRemoteIdentity) {
		return false, errors.New("DNS 远端标识缺失、不完整或可能已写入供应商，请先核对并恢复准确标识")
	}
	if err != nil || !prepared.DeleteRemote {
		return false, err
	}
	token, err := h.credentialCipher.Decrypt(prepared.CredentialCiphertext)
	if err != nil {
		return false, err
	}
	if err := deleteCloudflareDNSRecord(ctx, token, record.ProviderZoneID, record.ProviderRecordID); err != nil && !cloudflareRecordAlreadyAbsent(err) {
		return false, err
	}
	return true, nil
}
