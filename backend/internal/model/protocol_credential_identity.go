package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"gorm.io/gorm"
)

const protocolPrincipalRandomBytes = 16

// BeforeCreate keeps PrincipalKey an observable, stable identity without
// exposing the subscription/endpoint database primary keys in Zero runtime
// configuration and flow events. Existing rows are never rewritten, so
// historical accounting and already-published runtime identities remain valid.
func (credential *ProtocolCredential) BeforeCreate(_ *gorm.DB) error {
	current := strings.TrimSpace(credential.PrincipalKey)
	legacy := fmt.Sprintf("subscription:%d:endpoint:%d", credential.SubscriptionID, credential.ProtocolEndpointID)

	if current == "" || current == legacy {
		key, err := newProtocolPrincipalKey()
		if err != nil {
			return err
		}
		credential.PrincipalKey = key
	}
	// Identity normalization precedes derived indexing. A digest calculated by
	// the caller before this hook could otherwise describe the legacy key.
	credential.PrincipalDigest = entitlements.RetiredPrincipalHash(credential.NodeID, credential.PrincipalKey)
	return nil
}

func newProtocolPrincipalKey() (string, error) {
	payload := make([]byte, protocolPrincipalRandomBytes)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("generate protocol principal identity: %w", err)
	}
	return "principal:" + hex.EncodeToString(payload), nil
}

// Source identity changes invalidate derived lookup coverage in the same
// write. A later lookup can recover the index without trusting stale aliases.
func (credential *ProtocolCredential) BeforeUpdate(tx *gorm.DB) error {
	if tx.Statement.Changed("NodeID", "PrincipalKey", "Secret", "RetiredPrincipalHash") {
		tx.Statement.SetColumn("PrincipalLookupVersion", uint8(0))
		tx.Statement.SetColumn("PrincipalDigest", "")
		tx.Statement.SetColumn("SecretPrincipalDigest", "")
	}
	return nil
}
