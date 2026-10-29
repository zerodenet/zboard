package handler

import (
	"context"
	"fmt"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net"
	"time"
)

func (h *handlers) revokeNodeConnections(parent context.Context, nodeID uint, expectedSHA string) error {
	// Connection verification is independent of publication success. Retrying
	// this phase cannot compile, apply or create another configuration deployment.
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	pending := func(reason string) error { return fmt.Errorf("%w: %s", network.ErrConnectionRevocationPending, reason) }
	if len(expectedSHA) != 64 {
		return pending("applied configuration identity missing")
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		return pending("node inventory unavailable")
	}
	session, err := (zeroKernelRemoteDialer{h: h, node: node}).Dial(ctx)
	if err != nil {
		return pending("native control connection unavailable")
	}
	defer session.Close()
	socketSession, ok := session.(interface {
		OpenControlSocket(context.Context, string) (net.Conn, error)
	})
	if !ok {
		return pending("native stream-local transport unsupported")
	}
	conn, err := socketSession.OpenControlSocket(ctx, "/run/zerodenet/control.sock")
	if err != nil {
		return pending("native socket access unavailable; configuration remains applied")
	}
	authority := h.services.ConnectionRevocation(h.credentialCipher)
	_, err = zeroadapter.DrainRevokedFlows(ctx, conn, func(ctx context.Context) error {
		remote, err := (zeroadapter.ConfigurationPublisher{Dialer: zeroKernelRemoteDialer{h: h, node: node}}).Reconcile(ctx, expectedSHA)
		if err != nil || !remote.Applied {
			return zeroadapter.ErrFlowControlEvidence
		}
		return nil
	}, func(ctx context.Context, principal string, close func() error) (bool, error) {
		commandCtx, release := context.WithTimeout(ctx, 3*time.Second)
		defer release()
		stop := context.AfterFunc(commandCtx, func() { conn.Close() })
		defer stop()
		return authority.WithRevokedPrincipal(commandCtx, nodeID, principal, close)
	})
	if err != nil {
		return pending(err.Error())
	}
	return nil
}
