package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) ManagedDNSDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/dns-records/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.deletionMu.Lock()
	defer h.deletionMu.Unlock()
	var record model.ManagedDNSRecord
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
			return err
		}
		var running int64
		if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", id, "running").Count(&running).Error; err != nil {
			return err
		}
		if running > 0 || record.Status == dnsStatusSyncing {
			return errManagedDNSOperationRunning
		}
		return tx.Model(&record).Updates(map[string]interface{}{"status": resourceStatusDeleting, "last_error": ""}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	deleted, err := h.deleteManagedDNSRemote(ctx, record)
	if err == nil {
		err = h.db.Transaction(func(tx *gorm.DB) error {
			if err := createAuditLog(tx, claims, "dns_record.delete", fmt.Sprintf("managed_dns_record:%d", id),
				fmt.Sprintf("domain=%s type=%s provider_account=%d remote_record_deleted=%t", record.DomainName, record.RecordType, record.ProviderAccountID, deleted)); err != nil {
				return err
			}
			return tx.Delete(&record).Error
		})
	}
	if err != nil {
		h.failManagedDNSDeletion(id, err)
		writeJSON(w, http.StatusBadGateway, "Cloudflare 远端 DNS 清理未完成；面板记录已保留，请修复后重试删除。", nil)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true, "remote_record_deleted": deleted})
}

func (h *handlers) failManagedDNSDeletion(id uint, err error) {
	_ = h.db.Model(&model.ManagedDNSRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status": resourceStatusDeleting, "last_error": truncateCertificateError(err.Error()),
	}).Error
}
