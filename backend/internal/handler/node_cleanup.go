package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sshadapter "github.com/zerodenet/zboard/backend/internal/adapters/ssh"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/nodecleanup"
)

func nodeCleanupCommand() string {
	// The remote deadline survives an SSH disconnect. Use the embedded script,
	// not an unverified copy already present on the VPS. No secrets enter argv.
	return "timeout -k 5 150 sh -c " + shellQuote(string(nodecleanup.Script)) + " zboard-cleanup uninstall --yes"
}

func (h *handlers) executeNodeCleanupRun(ctx context.Context, run jobs.Run) error {
	if run.Owner != "system" || run.Handler != network.NodeCleanupHandler {
		return jobs.ErrInvalid
	}
	var input network.NodeCleanupRunInput
	if json.Unmarshal([]byte(run.Payload), &input) != nil || input.Revision != "1" || input.NodeID == 0 || strings.TrimSpace(input.SnapshotCiphertext) == "" {
		return jobs.ErrInvalid
	}
	encoded, err := h.credentialCipher.Decrypt(input.SnapshotCiphertext)
	if err != nil {
		return errors.New("node cleanup snapshot is unavailable")
	}
	var snapshot network.NodeCleanupSnapshot
	if json.Unmarshal([]byte(encoded), &snapshot) != nil || snapshot.NodeID != input.NodeID {
		return errors.New("node cleanup snapshot is invalid")
	}
	node := model.Node{
		ID: snapshot.NodeID, SSHHost: snapshot.Host, SSHPort: snapshot.Port, SSHUser: snapshot.User,
		SSHAuthMethod: snapshot.AuthMethod, SSHPwd: snapshot.CredentialCiphertext, SSHPrivateKeyPassphrase: snapshot.PassphraseCiphertext,
		SSHPrivilegeMode: snapshot.PrivilegeMode, SSHPrivilegePassword: snapshot.PrivilegePasswordCiphertext, SSHHostKeyFingerprint: snapshot.HostKeyFingerprint,
	}
	if strings.TrimSpace(node.SSHHostKeyFingerprint) == "" {
		return errors.New("node cleanup requires the trusted SSH host fingerprint captured before deletion")
	}
	if err := sshadapter.ValidateHostKeyFingerprint(node.SSHHostKeyFingerprint); err != nil {
		return errors.New("node cleanup SSH host fingerprint is invalid")
	}
	if err := h.validateNodeSSH(node); err != nil {
		return errors.New("node cleanup SSH configuration is unavailable")
	}
	inUse, err := h.services.NodeCleanupGuard.TargetInUse(ctx, node.SSHHost, node.SSHPort, node.SSHHostKeyFingerprint)
	if err != nil {
		return errors.New("cannot verify node cleanup target ownership")
	}
	if inUse {
		return errors.New("node cleanup target is used by another node")
	}
	credential, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return errors.New("node cleanup SSH credential is unavailable")
	}
	passphrase := ""
	if normalizeSSHAuthMethod(node.SSHAuthMethod) == sshAuthPrivateKey {
		passphrase, err = h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
		if err != nil {
			return errors.New("node cleanup SSH passphrase is unavailable")
		}
	}
	result, err := sshadapter.Dial(ctx, sshadapter.DialRequest{
		Host: node.SSHHost, Port: node.SSHPort, User: node.SSHUser, AuthMethod: normalizeSSHAuthMethod(node.SSHAuthMethod),
		Credential: credential, PrivateKeyPassphrase: passphrase, ExpectedHostFingerprint: node.SSHHostKeyFingerprint,
	})
	if err != nil {
		if observed := strings.TrimSpace(result.ObservedHostFingerprint); observed != "" && observed != strings.TrimSpace(node.SSHHostKeyFingerprint) {
			return errors.New("node cleanup SSH host fingerprint no longer matches the deletion snapshot")
		}
		return jobs.Retry(errors.New("node cleanup SSH connection failed"))
	}
	defer result.Client.Close()
	remote := h.newSSHRemoteSession(result.Client, node)
	defer remote.Close()
	stop := context.AfterFunc(ctx, func() {
		_ = remote.Close()
		_ = result.Client.Close()
	})
	defer stop()
	if _, err := remote.Run(nodeCleanupCommand(), true); err != nil {
		// The command may have reached the host. Preserve uncertainty rather than
		// replaying a destructive uninstall after a disconnect or timeout.
		return fmt.Errorf("node cleanup completion is unknown: %w", jobs.ErrUncertain)
	}
	return nil
}
