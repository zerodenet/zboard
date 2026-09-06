package handler

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

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
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	session, err := conn.NewSession()
	if err != nil {
		return "", time.Since(start), err
	}
	defer session.Close()

	command, stdin, requestPTY, err := h.prepareSSHCommand(node, command, privileged)
	if err != nil {
		return "", time.Since(start), err
	}
	if requestPTY {
		modes := ssh.TerminalModes{ssh.ECHO: 0, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
		if err := session.RequestPty("xterm", 24, 80, modes); err != nil {
			return "", time.Since(start), fmt.Errorf("request privilege terminal: %w", err)
		}
	}
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	out, err := session.CombinedOutput(command)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return string(bytes.TrimSpace(out)), time.Since(start), err
}

func (h *handlers) dialNodeSSH(node model.Node) (*ssh.Client, time.Duration, error) {
	return h.dialNodeSSHContext(context.Background(), node)
}

func (h *handlers) dialNodeSSHContext(ctx context.Context, node model.Node) (*ssh.Client, time.Duration, error) {
	start := time.Now()
	credential, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return nil, time.Since(start), fmt.Errorf("decrypt node ssh credential: %w", err)
	}
	var authMethod ssh.AuthMethod
	switch normalizeSSHAuthMethod(node.SSHAuthMethod) {
	case sshAuthPassword:
		authMethod = ssh.Password(credential)
	case sshAuthPrivateKey:
		passphrase, err := h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
		if err != nil {
			return nil, time.Since(start), fmt.Errorf("decrypt node ssh private key passphrase: %w", err)
		}
		signer, err := parseSSHPrivateKey(credential, passphrase)
		if err != nil {
			return nil, time.Since(start), err
		}
		authMethod = ssh.PublicKeys(signer)
	default:
		return nil, time.Since(start), errors.New("unsupported ssh_auth_method")
	}
	observedFingerprint := ""
	addr := net.JoinHostPort(strings.TrimSpace(node.SSHHost), strconv.Itoa(node.SSHPort))
	conf := &ssh.ClientConfig{
		User:            strings.TrimSpace(node.SSHUser),
		Auth:            []ssh.AuthMethod{authMethod},
		Timeout:         12 * time.Second,
		HostKeyCallback: verifiedHostKeyCallback(node.SSHHostKeyFingerprint, &observedFingerprint),
	}
	// ssh.ClientConfig.Timeout bounds TCP dialing only. Bound the entire SSH
	// handshake and close the underlying socket on cancellation as well.
	handshakeCtx, cancel := context.WithTimeout(ctx, conf.Timeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(handshakeCtx, "tcp", addr)
	if err != nil {
		return nil, time.Since(start), err
	}
	deadline, _ := handshakeCtx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		_ = raw.Close()
		return nil, time.Since(start), err
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(handshakeCtx, func() { _ = raw.Close(); close(closed) })
	clientConn, channels, requests, err := ssh.NewClientConn(raw, addr, conf)
	if !stop() {
		<-closed
	}
	if handshakeCtx.Err() != nil {
		err = handshakeCtx.Err()
	}
	if err == nil {
		err = raw.SetDeadline(time.Time{})
	}
	if err != nil {
		_ = raw.Close()
		return nil, time.Since(start), err
	}
	conn := ssh.NewClient(clientConn, channels, requests)
	if err := h.pinSSHHostKeyContext(ctx, node.ID, node.SSHHostKeyFingerprint, observedFingerprint); err != nil {
		_ = conn.Close()
		return nil, time.Since(start), err
	}
	return conn, time.Since(start), nil
}

func (h *handlers) pinSSHHostKey(nodeID uint, expectedFingerprint string, observedFingerprint string) error {
	return h.pinSSHHostKeyContext(context.Background(), nodeID, expectedFingerprint, observedFingerprint)
}

func (h *handlers) pinSSHHostKeyContext(ctx context.Context, nodeID uint, expectedFingerprint string, observedFingerprint string) error {
	expected := strings.TrimSpace(expectedFingerprint)
	observed := strings.TrimSpace(observedFingerprint)
	if expected != "" {
		var stored string
		if err := h.db.WithContext(ctx).Model(&model.Node{}).Select("ssh_host_key_fingerprint").Where("id = ?", nodeID).Scan(&stored).Error; err != nil {
			return fmt.Errorf("read recorded SSH host key: %w", err)
		}
		stored = strings.TrimSpace(stored)
		if stored == "" {
			return errors.New("SSH host trust was reset while connecting; retry the connection to enroll the current host key")
		}
		if subtle.ConstantTimeCompare([]byte(stored), []byte(expected)) != 1 || subtle.ConstantTimeCompare([]byte(observed), []byte(expected)) != 1 {
			return fmt.Errorf("SSH host key changed while connecting: expected %s, received %s; verify the VPS identity before resetting trust", stored, observed)
		}
		return nil
	}
	if err := validateSSHHostKeyFingerprint(observed); err != nil {
		return fmt.Errorf("record SSH host key: %w", err)
	}
	result := h.db.WithContext(ctx).Model(&model.Node{}).
		Where("id = ? AND (ssh_host_key_fingerprint IS NULL OR ssh_host_key_fingerprint = '')", nodeID).
		Update("ssh_host_key_fingerprint", observed)
	if result.Error != nil {
		return fmt.Errorf("record SSH host key: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var stored string
	if err := h.db.WithContext(ctx).Model(&model.Node{}).Select("ssh_host_key_fingerprint").Where("id = ?", nodeID).Scan(&stored).Error; err != nil {
		return fmt.Errorf("read recorded SSH host key: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(stored)), []byte(observed)) == 1 {
		return nil
	}
	return fmt.Errorf("SSH host key changed while it was being recorded: expected %s, received %s; verify the VPS identity before resetting trust", stored, observed)
}
