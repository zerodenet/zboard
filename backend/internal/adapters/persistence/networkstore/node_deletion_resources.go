package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func finalizeManagedNodeDeletion(tx *gorm.DB, id uint) error {
	var certs []uint
	if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", id).Pluck("id", &certs).Error; err != nil {
		return err
	}
	if len(certs) > 0 {
		if err := tx.Where("managed_certificate_id IN ?", certs).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", id).Updates(map[string]any{"status": "deleted", "auto_renew": false, "provider_account_id": nil}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id = ?", id).Delete(&model.ManagedDNSRecord{}).Error; err != nil {
		return err
	}
	return nil
}
