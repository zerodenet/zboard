package handler

import (
	"errors"
	"fmt"
	"net/http"

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
		if err := createAuditLog(tx, claims, "dns_record.delete", fmt.Sprintf("managed_dns_record:%d", id), fmt.Sprintf("domain=%s provider_account=%d remote_record_deleted=false external_cleanup=not_attempted", record.DomainName, record.ProviderAccountID)); err != nil {
			return err
		}
		return tx.Delete(&record).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true, "remote_record_deleted": false})
}
