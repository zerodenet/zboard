package network

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const NodeCleanupHandler = "node_cleanup"

// NodeCleanupSnapshot retains only the encrypted SSH material required after
// the owning node row has been deleted. The complete JSON envelope is encrypted
// again before it is persisted in the jobs ledger.
type NodeCleanupSnapshot struct {
	NodeID                      uint   `json:"node_id"`
	Host                        string `json:"host"`
	Port                        int    `json:"port"`
	User                        string `json:"user"`
	AuthMethod                  string `json:"auth_method"`
	CredentialCiphertext        string `json:"credential_ciphertext"`
	PassphraseCiphertext        string `json:"passphrase_ciphertext"`
	PrivilegeMode               string `json:"privilege_mode"`
	PrivilegePasswordCiphertext string `json:"privilege_password_ciphertext"`
	HostKeyFingerprint          string `json:"host_key_fingerprint"`
}

type NodeCleanupRunInput struct {
	Revision           string `json:"revision"`
	NodeID             uint   `json:"node_id"`
	NodeName           string `json:"node_name"`
	SnapshotCiphertext string `json:"snapshot_ciphertext"`
}

func NodeCleanupResource(host string, port int) string {
	identity := fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(host)), port)
	digest := sha256.Sum256([]byte(identity))
	return "node-cleanup:" + hex.EncodeToString(digest[:])
}

type NodeCleanupGuardRepository interface {
	TargetInUse(context.Context, string, int, string) (bool, error)
}

type NodeCleanupGuard struct{ Repository NodeCleanupGuardRepository }

func (g NodeCleanupGuard) TargetInUse(ctx context.Context, host string, port int, fingerprint string) (bool, error) {
	if g.Repository == nil {
		return false, errors.New("node cleanup guard is unavailable")
	}
	return g.Repository.TargetInUse(ctx, strings.TrimSpace(host), port, strings.TrimSpace(fingerprint))
}
