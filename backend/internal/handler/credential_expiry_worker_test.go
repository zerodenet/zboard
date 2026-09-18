package handler

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func TestCredentialExpiryRequiresRepository(t *testing.T) {
	service := entitlements.CredentialExpiry{}
	if _, err := service.ExpireDue(context.Background(), time.Now().UTC(), 1); err == nil {
		t.Fatal("missing repository must be rejected")
	}
}
