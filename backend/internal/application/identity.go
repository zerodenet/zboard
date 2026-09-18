package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/pluginstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"gorm.io/gorm"
)

type Identity struct {
	db               *gorm.DB
	secret           string
	Accounts         identity.Accounts
	Sessions         identity.Sessions
	Tokens           identity.SessionTokens
	Passwords        identity.PasswordService
	Administration   identity.Administration
	Directory        identity.AccountDirectory
	Relationships    identity.RelationshipDirectory
	Registration     identity.Registration
	Bindings         identity.BindingRemoval
	Confirmation     identity.PasswordConfirmation
	EmailCodes       identity.EmailCodes
	EmailAttempts    identity.RegistrationAttempts
	External         identity.ExternalIdentities
	InitialPasswords identity.InitialPasswords
}

func NewIdentity(db *gorm.DB, secret string) Identity {
	tokens := identity.SessionTokens{Key: []byte(secret)}
	codes := identity.EmailCodes{Key: []byte(secret)}
	confirmation := identity.PasswordConfirmation{Key: []byte(secret)}
	accounts := identitystore.Accounts{DB: db}
	return Identity{
		db: db, secret: secret, Tokens: tokens, EmailCodes: codes, Confirmation: confirmation,
		Accounts: identity.Accounts{Repository: accounts}, Sessions: identity.Sessions{Tokens: tokens, Accounts: accounts},
		Passwords:        identity.PasswordService{Repository: identitystore.Passwords{DB: db}},
		Administration:   identity.Administration{Repository: identitystore.Administration{DB: db}},
		Directory:        identity.AccountDirectory{Repository: identitystore.AccountDirectory{DB: db}},
		Relationships:    identity.RelationshipDirectory{Repository: identitystore.RelationshipDirectory{DB: db}},
		Registration:     identity.Registration{Repository: identitystore.Registration{DB: db}, Tokens: tokens, EmailCodes: codes},
		Bindings:         identity.BindingRemoval{Repository: identitystore.BindingRemoval{DB: db}, Confirmation: confirmation},
		External:         identity.ExternalIdentities{Repository: identitystore.ExternalIdentities{DB: db}, Tokens: tokens, EmailCodes: codes},
		InitialPasswords: identity.InitialPasswords{Repository: identitystore.InitialPasswords{DB: db}},
		EmailAttempts:    identitystore.EmailAttempts{DB: db},
	}
}

// InTransaction preserves the existing provider lifecycle transaction and its
// cancellation context without opening another runtime or worker pool.
func (s Identity) InTransaction(tx *gorm.DB) Identity { return NewIdentity(tx, s.secret) }
func (s Identity) PluginTransactions() plugins.IdentityTransactions {
	return pluginstore.IdentityTransactions{DB: s.db, Tokens: s.Tokens, EmailCodes: s.EmailCodes}
}
func (s Identity) CodeIssuance(delivery identity.CodeDelivery) identity.CodeIssuance {
	return identity.CodeIssuance{Repository: identitystore.CodeIssuance{DB: s.db}, Codes: s.EmailCodes, Delivery: delivery}
}
