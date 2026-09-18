package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

func (s *Services) Integrations() identity.Integrations {
	return identity.Integrations{Repository: identitystore.Integrations{DB: s.Identity.db}, AllowedScopes: []string{"commerce.orders.list", "commerce.payments.record", "metering.usage.query"}}
}
func (s *Services) IntegrationAuthority() catalog.Authority {
	return identitystore.Integrations{DB: s.Identity.db}
}

func (s *Services) IntegrationAdmission() catalog.Admission {
	return identitystore.Integrations{DB: s.Identity.db}
}

func (s *Services) AuthenticateIntegration(ctx context.Context, c catalog.Credential) error {
	return (identitystore.Integrations{DB: s.Identity.db}).Authenticate(ctx, c)
}
