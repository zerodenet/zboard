package network

import (
	"context"
	"errors"
)

var ErrConnectionRevocationPending = errors.New("configuration applied; connection revocation is pending verification")

// ConnectionRevocationAuthorization fences entitlement changes while a
// currently forbidden, owned principal's native close command is submitted.
// An unknown or currently authorized principal never invokes close.
type ConnectionRevocationAuthorization interface {
	WithRevokedPrincipal(context.Context, uint, string, func() error) (bool, error)
}
