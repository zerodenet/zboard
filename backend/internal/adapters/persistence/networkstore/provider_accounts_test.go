package networkstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type providerTestCipher struct{}

func (providerTestCipher) Encrypt(value string) (string, error) { return "encrypted:" + value, nil }
func (providerTestCipher) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "encrypted:"), nil
}

type providerVerifierFunc func(context.Context, string, string) error

func (f providerVerifierFunc) VerifyProviderCredential(ctx context.Context, key, token string) error {
	return f(ctx, key, token)
}

type providerRegistryFunc func(context.Context, string) (network.ProviderDefinition, error)

func (f providerRegistryFunc) ProviderDefinition(ctx context.Context, key string) (network.ProviderDefinition, error) {
	return f(ctx, key)
}

func TestProviderCapabilityRejectsStaleEvidenceAndRevokedAuthority(t *testing.T) {
	for _, mode := range []string{"update", "verify"} {
		for _, scenario := range []string{"success", "denied", "revision", "revoked", "audit", "canceled"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				db, _ := administrationFixture(t)
				seedLegacyResources(t, db)
				check := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				check(db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error)
				check(db.Model(&model.ProviderAccount{}).Where("id = ?", 1).Updates(map[string]any{"provider_key": "cloudflare", "status": "active", "credential_ciphertext": "encrypted:original-token"}).Error)
				var before model.ProviderAccount
				check(db.First(&before, 1).Error)
				if scenario == "audit" {
					check(db.Callback().Create().Before("gorm:create").Register("fail_provider_audit", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
							tx.AddError(errors.New("audit failure"))
						}
					}))
					defer db.Callback().Create().Remove("fail_provider_audit")
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				verifier := providerVerifierFunc(func(_ context.Context, key, token string) error {
					if key != "cloudflare" {
						t.Fatal("wrong provider", key)
					}
					switch scenario {
					case "denied":
						return errors.New("denied")
					case "revision":
						check(db.Model(&model.ProviderAccount{}).Where("id = ?", 1).Updates(map[string]any{"revision": before.Revision + 1, "credential_ciphertext": "newer-credential", "status": "pending"}).Error)
					case "revoked":
						check(db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error)
					case "canceled":
						cancel()
					}
					return nil
				})
				service := network.ProviderAccounts{Store: ProviderAccounts{DB: db}, Cipher: providerTestCipher{}, Verifier: verifier}
				var err error
				if mode == "update" {
					_, err = service.Update(ctx, 1, 1, network.ProviderUpdate{Name: "new-name", APIToken: "replacement-token-123456789", ExpectedRevision: before.Revision})
				} else {
					_, err = service.Verify(ctx, 1, 1)
				}
				var after model.ProviderAccount
				check(db.First(&after, 1).Error)
				switch scenario {
				case "success":
					check(err)
					if after.Status != "active" || after.LastVerifiedAt == nil || after.Revision != before.Revision+1 {
						t.Fatal("verification not committed")
					}
					if mode == "update" && (after.Name != "new-name" || after.CredentialCiphertext == before.CredentialCiphertext) {
						t.Fatal("replacement not committed")
					}
				case "revision":
					if !errors.Is(err, network.ErrProviderConflict) || after.CredentialCiphertext != "newer-credential" || after.Status != "pending" {
						t.Fatal("stale verification overwrote newer account", err)
					}
				case "denied":
					if err == nil {
						t.Fatal("invalid token accepted")
					}
					if mode == "verify" {
						if after.Status != "invalid" || after.LastError != "denied" {
							t.Fatal("failed verification missing")
						}
					} else if after.CredentialCiphertext != before.CredentialCiphertext || after.Name != before.Name || after.Revision != before.Revision {
						t.Fatal("failed replacement persisted")
					}
				default:
					if err == nil || after.CredentialCiphertext != before.CredentialCiphertext || after.Name != before.Name || after.Revision != before.Revision || after.LastVerifiedAt != nil {
						t.Fatal("partial unauthorized or canceled update", err)
					}
				}
			})
		}
	}
}

func TestProviderCapabilityChecksAdminBeforeExternalVerification(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	service := network.ProviderAccounts{Store: ProviderAccounts{DB: db}, Cipher: providerTestCipher{}, Verifier: providerVerifierFunc(func(context.Context, string, string) error { t.Fatal("unauthorized remote request"); return nil })}
	if _, err := service.Verify(context.Background(), 1, 1); !errors.Is(err, network.ErrProviderPermission) {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), 1, 1, network.ProviderUpdate{Name: "name", APIToken: "replacement-token-123456789"}); !errors.Is(err, network.ErrProviderPermission) {
		t.Fatal(err)
	}
}

func TestProviderCreationKeepsSavedInvalidAccountAndRollsBackAuditFailure(t *testing.T) {
	for _, scenario := range []string{"success", "plugin", "invalid", "audit", "unauthorized", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			seedLegacyResources(t, db)
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "unauthorized" {
				check(db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error)
			}
			if scenario == "audit" {
				check(db.Callback().Create().Before("gorm:create").Register("fail_creation_audit", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
						tx.AddError(errors.New("audit failed"))
					}
				}))
				defer db.Callback().Create().Remove("fail_creation_audit")
			}
			reads := 0
			store := ProviderAccounts{DB: db}
			service := network.ProviderCreation{Store: store, Accounts: network.ProviderAccounts{Store: store, Cipher: providerTestCipher{}, Verifier: providerVerifierFunc(func(context.Context, string, string) error {
				reads++
				if scenario == "invalid" {
					return errors.New("denied")
				}
				return nil
			})}, Registry: providerRegistryFunc(func(_ context.Context, key string) (network.ProviderDefinition, error) {
				if key == "edge-dns" {
					return network.ProviderDefinition{Key: key, Name: "Edge DNS", Capabilities: []string{"dns.records"}}, nil
				}
				if key == "cloudflare" {
					return network.ProviderDefinition{Key: key, Name: "Cloudflare", Capabilities: []string{"dns.records", "certificate.origin"}}, nil
				}
				return network.ProviderDefinition{}, errors.New("unsupported")
			})}
			input := network.ProviderCreate{ProviderKey: " CloudFlare ", Name: " New provider ", APIToken: " creation-token-123456789 "}
			if scenario == "plugin" {
				input.ProviderKey, input.APIToken = "edge-dns", "short"
			}
			if scenario == "duplicate" {
				var row model.ProviderAccount
				check(db.First(&row, 1).Error)
				input.Name = row.Name
			}
			account, verificationErr, err := service.Create(context.Background(), 1, input)
			var count int64
			check(db.Model(&model.ProviderAccount{}).Count(&count).Error)
			if scenario == "success" || scenario == "plugin" || scenario == "invalid" {
				check(err)
				if account.ID == 0 || count != 2 || reads != 1 {
					t.Fatal("creation not retained", count, reads)
				}
				if scenario == "invalid" && (verificationErr == nil || account.Status != "invalid" || account.LastError != "denied") {
					t.Fatal("invalid verification result lost")
				}
				if scenario == "success" && (verificationErr != nil || account.Status != "active" || account.Name != "New provider") {
					t.Fatal(account.Status, verificationErr)
				}
				if scenario == "plugin" && (verificationErr != nil || account.Status != "active" || account.ProviderKey != "edge-dns" || len(account.Capabilities) != 1 || account.Capabilities[0] != "dns.records" || account.CredentialPrefix != "••••") {
					t.Fatalf("plugin account=%+v verification=%v", account, verificationErr)
				}
			} else if err == nil || reads != 0 || count != 1 {
				t.Fatal("failed creation changed authority", err, count, reads)
			}
		})
	}
}
