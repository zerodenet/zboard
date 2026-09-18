package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

func (s *Services) SettingUpdate(cipher platform.SettingsCipher) platform.SettingUpdate {
	return platform.SettingUpdate{Repository: platformstore.SettingUpdate{DB: s.Identity.db, Cipher: cipher}}
}
