package messagingstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RegistrationWelcome struct {
	DB     *gorm.DB
	Cipher platform.SettingsCipher
}

func (s RegistrationWelcome) EnqueueRegisteredAccount(ctx context.Context, accountID uint) (messaging.WelcomeReceipt, error) {
	var receipt messaging.WelcomeReceipt
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the account before checking replay; callbacks for the same account
		// cannot generate duplicate tasks or audit rows across host instances.
		var user model.User
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", accountID, "active").Limit(1).Find(&user)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		// Preserve optional welcome behavior: unavailable or disabled mail delivery
		// does not prevent successful core registration.
		if _, err := platformstore.LoadSMTPSettings(tx, s.Cipher, true); err != nil {
			return nil
		}
		var template model.EmailTemplate
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("category = ? AND trigger_key = ? AND is_active = ?", messaging.TemplateRegistration, "user.registered", true).First(&template).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		key := fmt.Sprintf("registration-welcome:%d:%d", accountID, template.Revision)
		var existing model.Task
		found := tx.Where("idempotency_key = ?", key).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			receipt.TaskID = existing.ID
			return nil
		}
		var site model.Installation
		if err := tx.First(&site, 1).Error; err != nil {
			return err
		}
		name := strings.TrimSpace(site.SiteName)
		if name == "" {
			name = "Zboard"
		}
		content, err := json.Marshal(messaging.EmailContent{Subject: template.SubjectTemplate, Body: template.BodyTemplate, TemplateID: template.ID, TemplateRevision: template.Revision, SiteName: name, SiteURL: strings.TrimSpace(site.SiteURL)})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		task := model.Task{Type: "email", Scope: fmt.Sprintf(`{"user_ids":[%d]}`, accountID), Content: string(content), Status: 0, Total: 1, IdempotencyKey: key, MaxAttempts: 3, ScheduledAt: &now}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: strconv.FormatUint(uint64(accountID), 10), Payload: "{}", Status: 0}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{Actor: "system", Action: "notification.enqueue", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("trigger=user.registered template_id=%d revision=%d", template.ID, template.Revision)}).Error; err != nil {
			return err
		}
		receipt = messaging.WelcomeReceipt{TaskID: task.ID, Created: true}
		return nil
	})
	if err != nil {
		return messaging.WelcomeReceipt{}, err
	}
	return receipt, nil
}
