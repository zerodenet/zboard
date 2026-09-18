package network

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrSSHHostTrustUnavailable = errors.New("SSH host trust unavailable")
	ErrSSHHostTrustInvalid     = errors.New("invalid SSH host fingerprint")
	ErrSSHHostTrustNodeMissing = errors.New("SSH host trust node not found")
)

type SSHHostTrustRepository interface {
	PinSSHHostKey(context.Context, uint, string, string) error
}

type SSHHostTrust struct{ Repository SSHHostTrustRepository }

func (s SSHHostTrust) Pin(ctx context.Context, nodeID uint, expected, observed string) error {
	if s.Repository == nil {
		return ErrSSHHostTrustUnavailable
	}
	expected = strings.TrimSpace(expected)
	observed = strings.TrimSpace(observed)
	if nodeID == 0 || observed == "" {
		return ErrSSHHostTrustInvalid
	}
	return s.Repository.PinSSHHostKey(ctx, nodeID, expected, observed)
}
