package handler

import (
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
)

func newTestHandlers(db *gorm.DB, secret string, cipher *security.CredentialCipher, dir, contract, version string) (*handlers, error) {
	services := application.New(db, secret)
	h, err := NewHandlers(services, db, secret, cipher, dir, contract, version)
	if err != nil {
		services.Close()
		return nil, err
	}
	services.StartWork()
	return h, nil
}
