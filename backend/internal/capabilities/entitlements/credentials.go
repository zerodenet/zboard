package entitlements

// CredentialIssuer supplies carrier-specific material. Entitlements owns
// subscription identity, credential persistence, expiry and revocation.
type CredentialIssuer interface {
	Supports(protocol string) bool
	Status(protocol string, principalReady bool) string
	Secret(protocol, protectedServerConfig string) (string, error)
	Encrypt(secret string) (string, error)
}
