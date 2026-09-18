package application

import (
	"context"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
)

type configuredTaskMail struct {
	DB     *gorm.DB
	Cipher platform.SettingsCipher
}

func (s configuredTaskMail) SendTaskMessage(ctx context.Context, message messaging.Message) error {
	settings, err := platformstore.LoadSMTPSettings(s.DB.WithContext(ctx), s.Cipher, true)
	if err != nil {
		return err
	}
	message.ID = fmt.Sprintf("<%s@%s>", message.ID, settings.Host)
	return SMTPDelivery(settings).Send(ctx, message)
}
func (s *Services) EmailExecution(cipher platform.SettingsCipher) messaging.EmailExecution {
	return messaging.EmailExecution{Repository: messagingstore.EmailExecution{DB: s.Identity.db}, Sender: configuredTaskMail{DB: s.Identity.db, Cipher: cipher}}
}

func (s *Services) SMTPSettings(ctx context.Context, cipher platform.SettingsCipher, requireEnabled bool) (platform.SMTPSettings, error) {
	return platformstore.LoadSMTPSettings(s.Identity.db.WithContext(ctx), cipher, requireEnabled)
}
