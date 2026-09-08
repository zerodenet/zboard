package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) ProviderAccountDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/provider-accounts/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	blockers := map[string]int64{}
	blocked := errors.New("provider account is still referenced")
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var account model.ProviderAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, id).Error; err != nil {
			return err
		}
		for name, query := range map[string]*gorm.DB{
			"running_operations": tx.Model(&model.ProviderOperation{}).Where("provider_account_id = ? AND status = ?", id, "running"),
		} {
			var count int64
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				blockers[name] = count
			}
		}
		if len(blockers) != 0 {
			return blocked
		}
		var running int64
		if err := tx.Model(&model.ManagedCertificate{}).Where("provider_account_id = ? AND status IN ?", id, []string{certificateStatusIssuing, certificateStatusRenewing}).Count(&running).Error; err != nil {
			return err
		}
		if running > 0 {
			blockers["certificate_operations"] = running
			return blocked
		}
		if err := tx.Model(&model.ManagedCertificate{}).Where("provider_account_id = ?", id).Updates(map[string]interface{}{"provider_account_id": nil, "auto_renew": false}).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_account_id = ?", id).Delete(&model.ManagedDNSRecord{}).Error; err != nil {
			return err
		}

		if err := createAuditLog(tx, claims, "provider_account.delete", fmt.Sprintf("provider_account:%d", id), "local credential removed; external account and shared token unchanged"); err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, blocked) {
		writeJSON(w, http.StatusConflict, "请等待正在执行的供应商或证书任务结束。", map[string]interface{}{"blockers": blockers})
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true, "external_account_retained": true})
}
