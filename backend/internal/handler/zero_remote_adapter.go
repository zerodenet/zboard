package handler

import (
	"context"

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type zeroKernelRemoteDialer struct {
	h    *handlers
	node model.Node
}

func (d zeroKernelRemoteDialer) Dial(ctx context.Context) (zeroadapter.KernelRemoteSession, error) {
	client, _, err := d.h.dialNodeSSHContext(ctx, d.node)
	if err != nil {
		return nil, err
	}
	return d.h.newSSHRemoteSession(client, d.node), nil
}
