package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sshadapter "github.com/zerodenet/zboard/backend/internal/adapters/ssh"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/ssh"
)

func (h *handlers) execSSHCommand(node model.Node, command string) (string, time.Duration, error) {
	return h.execSSHCommandWithPrivilege(node, command, false)
}

func (h *handlers) execSSHCommandWithPrivilege(node model.Node, command string, privileged bool) (string, time.Duration, error) {
	return h.execSSHCommandWithPrivilegeContext(context.Background(), node, command, privileged)
}

func (h *handlers) execSSHCommandWithPrivilegeContext(ctx context.Context, node model.Node, command string, privileged bool) (string, time.Duration, error) {
	start := time.Now()
	conn, _, err := h.dialNodeSSHContext(ctx, node)
	if err != nil {
		return "", time.Since(start), err
	}
	remote := h.newSSHRemoteSession(conn, node)
	defer remote.Close()
	stop := context.AfterFunc(ctx, func() { _ = remote.Close() })
	defer stop()
	out, err := remote.Run(command, privileged)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out, time.Since(start), err
}

func (h *handlers) newSSHRemoteSession(client *ssh.Client, node model.Node) *sshadapter.RemoteSession {
	return sshadapter.NewRemoteSession(client, func(command string, privileged bool) (string, string, bool, error) {
		return h.prepareSSHCommand(node, command, privileged)
	})
}

func (h *handlers) dialNodeSSH(node model.Node) (*ssh.Client, time.Duration, error) {
	return h.dialNodeSSHContext(context.Background(), node)
}

func (h *handlers) dialNodeSSHContext(ctx context.Context, node model.Node) (*ssh.Client, time.Duration, error) {
	pendingCleanup, err := h.services.JobResourceFence.Pending(ctx, network.NodeCleanupResource(node.SSHHost, node.SSHPort))
	if err != nil {
		return nil, 0, fmt.Errorf("check node cleanup fence: %w", err)
	}
	if pendingCleanup {
		return nil, 0, errors.New("该 SSH 主机仍有未完成的节点清理任务；请先完成、复核或取消清理，再执行节点运维")
	}
	credential, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return nil, 0, fmt.Errorf("decrypt node ssh credential: %w", err)
	}
	passphrase := ""
	if normalizeSSHAuthMethod(node.SSHAuthMethod) == sshAuthPrivateKey {
		passphrase, err = h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
		if err != nil {
			return nil, 0, fmt.Errorf("decrypt node ssh private key passphrase: %w", err)
		}
	}
	result, err := sshadapter.Dial(ctx, sshadapter.DialRequest{
		Host: node.SSHHost, Port: node.SSHPort, User: node.SSHUser,
		AuthMethod: normalizeSSHAuthMethod(node.SSHAuthMethod), Credential: credential,
		PrivateKeyPassphrase: passphrase, ExpectedHostFingerprint: node.SSHHostKeyFingerprint,
	})
	if err != nil {
		return nil, result.Elapsed, err
	}
	if err := h.pinSSHHostKeyContext(ctx, node.ID, node.SSHHostKeyFingerprint, result.ObservedHostFingerprint); err != nil {
		_ = result.Client.Close()
		return nil, result.Elapsed, err
	}
	return result.Client, result.Elapsed, nil
}

func (h *handlers) pinSSHHostKey(nodeID uint, expectedFingerprint string, observedFingerprint string) error {
	return h.pinSSHHostKeyContext(context.Background(), nodeID, expectedFingerprint, observedFingerprint)
}

func (h *handlers) pinSSHHostKeyContext(ctx context.Context, nodeID uint, expectedFingerprint string, observedFingerprint string) error {
	expected := strings.TrimSpace(expectedFingerprint)
	observed := strings.TrimSpace(observedFingerprint)
	if err := sshadapter.ValidateHostKeyFingerprint(observed); err != nil {
		return fmt.Errorf("record SSH host key: %w", err)
	}
	return h.services.SSHHostTrust.Pin(ctx, nodeID, expected, observed)
}
