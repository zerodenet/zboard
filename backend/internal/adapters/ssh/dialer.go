package ssh

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

const (
	AuthPassword   = "password"
	AuthPrivateKey = "private_key"
)

type DialRequest struct {
	Host                    string
	Port                    int
	User                    string
	AuthMethod              string
	Credential              string
	PrivateKeyPassphrase    string
	ExpectedHostFingerprint string
	Timeout                 time.Duration
}

type DialResult struct {
	Client                  *cryptossh.Client
	ObservedHostFingerprint string
	Elapsed                 time.Duration
}

func Dial(ctx context.Context, request DialRequest) (DialResult, error) {
	started := time.Now()
	auth, err := authMethod(request.AuthMethod, request.Credential, request.PrivateKeyPassphrase)
	if err != nil {
		return DialResult{Elapsed: time.Since(started)}, err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	observed := ""
	address := net.JoinHostPort(strings.TrimSpace(request.Host), strconv.Itoa(request.Port))
	config := &cryptossh.ClientConfig{
		User:            strings.TrimSpace(request.User),
		Auth:            []cryptossh.AuthMethod{auth},
		Timeout:         timeout,
		HostKeyCallback: VerifiedHostKeyCallback(request.ExpectedHostFingerprint, &observed),
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(handshakeCtx, "tcp", address)
	if err != nil {
		return DialResult{Elapsed: time.Since(started)}, err
	}
	deadline, _ := handshakeCtx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		_ = raw.Close()
		return DialResult{Elapsed: time.Since(started)}, err
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(handshakeCtx, func() {
		_ = raw.Close()
		close(closed)
	})
	clientConn, channels, requests, err := cryptossh.NewClientConn(raw, address, config)
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
		return DialResult{ObservedHostFingerprint: observed, Elapsed: time.Since(started)}, err
	}
	return DialResult{
		Client:                  cryptossh.NewClient(clientConn, channels, requests),
		ObservedHostFingerprint: observed,
		Elapsed:                 time.Since(started),
	}, nil
}

func ParsePrivateKey(privateKey, passphrase string) (cryptossh.Signer, error) {
	if strings.TrimSpace(privateKey) == "" {
		return nil, errors.New("empty SSH private key")
	}
	if passphrase == "" {
		return cryptossh.ParsePrivateKey([]byte(privateKey))
	}
	return cryptossh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
}

func ValidateHostKeyFingerprint(fingerprint string) error {
	normalized := strings.TrimSpace(fingerprint)
	if !strings.HasPrefix(normalized, "SHA256:") {
		return errors.New("SSH host fingerprint must use SHA256 format")
	}
	encoded := strings.TrimPrefix(normalized, "SHA256:")
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("SSH host fingerprint is invalid")
	}
	return nil
}

func VerifiedHostKeyCallback(expectedFingerprint string, observedFingerprint *string) cryptossh.HostKeyCallback {
	expected := strings.TrimSpace(expectedFingerprint)
	return func(_ string, _ net.Addr, key cryptossh.PublicKey) error {
		actual := cryptossh.FingerprintSHA256(key)
		if observedFingerprint != nil {
			*observedFingerprint = actual
		}
		if expected == "" {
			return nil
		}
		if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
			return fmt.Errorf("SSH host key changed: expected %s, received %s; verify the VPS identity before resetting trust", expected, actual)
		}
		return nil
	}
}

func authMethod(method, credential, passphrase string) (cryptossh.AuthMethod, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", AuthPassword:
		return cryptossh.Password(credential), nil
	case AuthPrivateKey:
		signer, err := ParsePrivateKey(credential, passphrase)
		if err != nil {
			return nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		return cryptossh.PublicKeys(signer), nil
	default:
		return nil, errors.New("unsupported SSH authentication method")
	}
}
