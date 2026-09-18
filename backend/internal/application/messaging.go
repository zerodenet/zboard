package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	smtpadapter "github.com/zerodenet/zboard/backend/internal/adapters/smtp"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
	"time"
)

func (s *Services) DeliveryReview() messaging.DeliveryReview {
	return messaging.DeliveryReview{Repository: messagingstore.DeliveryReview{DB: s.Identity.db}}
}
func RequireReviewedMailRetry(tx *gorm.DB, taskID uint) error {
	return messagingstore.RequireReviewedMailRetry(tx, taskID)
}

func SMTPDelivery(settings platform.SMTPSettings) messaging.Delivery {
	return messaging.Delivery{Channel: smtpadapter.Channel{Settings: settings}}
}

func (s *Services) RegistrationWelcome(cipher platform.SettingsCipher) messaging.RegistrationWelcome {
	return messaging.RegistrationWelcome{Repository: messagingstore.RegistrationWelcome{DB: s.Identity.db, Cipher: cipher}}
}

func (s *Services) RegistrationMessages(cipher platform.SettingsCipher) messaging.RegistrationEvents {
	return messaging.RegistrationEvents{Repository: messagingstore.RegistrationWelcome{DB: s.Identity.db, Cipher: cipher}}
}

func (s *Services) RegistrationEventStatus() messaging.RegistrationStatus {
	return messaging.RegistrationStatus{Repository: messagingstore.RegistrationStatus{DB: s.Identity.db}}
}

func StartMailAttempt(tx *gorm.DB, taskID, itemID uint, token string, now time.Time) (int, error) {
	return messagingstore.StartMailAttempt(tx, taskID, itemID, token, now)
}
func FinishMailAttempt(tx *gorm.DB, taskID, itemID uint, attempt int, token string, acceptance messaging.Acceptance, now time.Time) error {
	return messagingstore.FinishMailAttempt(tx, taskID, itemID, attempt, token, acceptance, now)
}
func (s *Services) DeliveryHistory() messaging.DeliveryHistory {
	return messaging.DeliveryHistory{Repository: messagingstore.DeliveryHistory{DB: s.Identity.db}}
}

func (s *Services) MessageRequests() messaging.Requests {
	return messaging.Requests{Repository: messagingstore.Requests{DB: s.Identity.db}}
}
