package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (s *Services) GroupCredentialReconciliation(cipher zeroadapter.Cipher, mieru bool) entitlements.GroupCredentialReconciliation {
	return entitlements.GroupCredentialReconciliation{Repository: entitlementstore.GroupCredentialReconciliation{
		DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru},
	}}
}

func (s *Services) NodeCredentialReconciliation(cipher zeroadapter.Cipher, mieru bool) entitlements.NodeCredentialReconciliation {
	return entitlements.NodeCredentialReconciliation{Repository: entitlementstore.NodeCredentialReconciliation{
		DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru},
	}}
}

func (s *Services) CredentialPreparation(cipher zeroadapter.Cipher, mieru bool) entitlements.CredentialPreparation {
	return entitlements.CredentialPreparation{Repository: entitlementstore.CredentialPreparation{
		DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru},
	}}
}
