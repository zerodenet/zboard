package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sshadapter "github.com/zerodenet/zboard/backend/internal/adapters/ssh"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func (h *handlers) RetireResource(ctx context.Context, nodeID uint, runID string, step network.RetirementStep) (string, error) {
	material, err := h.services.NodeRetirement(h).Repository.Material(ctx, nodeID, runID, step)
	if err != nil {
		return "", err
	}
	s := material.SSH
	node := model.Node{ID: s.NodeID, SSHHost: s.Host, SSHPort: s.Port, SSHUser: s.User, SSHAuthMethod: s.AuthMethod, SSHPwd: s.CredentialCiphertext, SSHPrivateKeyPassphrase: s.PassphraseCiphertext, SSHPrivilegeMode: s.PrivilegeMode, SSHPrivilegePassword: s.PrivilegePasswordCiphertext, SSHHostKeyFingerprint: s.HostKeyFingerprint}
	switch step.Kind {
	case "dns":
		if material.Shared {
			return "其他节点仍在使用此 DNS，供应商记录已保留。", nil
		}
		d := material.DNS
		return h.retireDNS(ctx, model.ManagedDNSRecord{ID: d.ID, NodeID: d.NodeID, ProviderAccountID: d.ProviderAccountID, ProviderZoneID: d.ProviderZoneID, ProviderRecordID: d.ProviderRecordID, DomainName: d.DomainName, RecordType: d.RecordType, RecordValue: d.RecordValue, TTL: d.TTL, Proxied: d.Proxied, ObservedHash: d.ObservedHash, LastSyncedAt: d.LastSyncedAt})
	case "certificate":
		command, managed := retirementCertificateCommand(model.ManagedCertificate{ID: material.CertificateID, CertPath: material.CertPath, KeyPath: material.KeyPath})
		if !managed {
			return "证书使用非面板专用路径，文件已保留；不会撤销 CA 证书。", nil
		}
		return "", h.retirementSSH(ctx, node, command)
	case "zero":
		return "", h.retirementSSH(ctx, node, nodeCleanupCommand())
	default:
		return "", errors.New("unknown retirement resource")
	}
}
func (h *handlers) retireDNS(ctx context.Context, record model.ManagedDNSRecord) (string, error) {
	prepared, err := h.services.DNSDeletion.Prepare(ctx, network.DNSDeletionInput{RecordID: record.ID, ProviderAccountID: record.ProviderAccountID, ProviderZoneID: record.ProviderZoneID, ProviderRecordID: record.ProviderRecordID, ObservedHash: record.ObservedHash, LastSyncedAt: record.LastSyncedAt})
	if err != nil {
		return "", err
	}
	if !prepared.DeleteRemote {
		return "尚未同步到供应商，无远端记录需要清理。", nil
	}
	token, err := h.credentialCipher.Decrypt(prepared.CredentialCiphertext)
	if err != nil {
		return "", err
	}
	path := "/zones/" + url.PathEscape(record.ProviderZoneID) + "/dns_records/" + url.PathEscape(record.ProviderRecordID)
	remote, err := cloudflareRequest[cloudflareRecord](ctx, http.MethodGet, path, token, nil)
	if cloudflareRecordAlreadyAbsent(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	marker := fmt.Sprintf("[ZBoard dns=%d node=%d]", record.ID, record.NodeID)
	if remote.ID != record.ProviderRecordID || !strings.EqualFold(strings.TrimSuffix(remote.Name, "."), strings.TrimSuffix(record.DomainName, ".")) || remote.Type != record.RecordType || remote.Content != record.RecordValue || remote.Proxied != record.Proxied || remote.TTL != record.TTL || !strings.HasSuffix(remote.Comment, marker) {
		return "供应商记录已变更或来源不匹配，已保留以避免误删。", nil
	}
	err = deleteCloudflareDNSRecord(ctx, token, record.ProviderZoneID, record.ProviderRecordID)
	if cloudflareRecordAlreadyAbsent(err) {
		err = nil
	}
	return "", err
}

// Delete only this certificate's generated paths, never a whole certificate
// tree or arbitrary paths received from the browser/database.
func retirementCertificateCommand(cert model.ManagedCertificate) (string, bool) {
	base := fmt.Sprintf("/etc/zboard/certificates/%d", cert.ID)
	if (cert.CertPath != "" && !strings.HasPrefix(cert.CertPath, base+"/current/")) || (cert.KeyPath != "" && !strings.HasPrefix(cert.KeyPath, base+"/current/")) {
		return "", false
	}
	paths := []string{base, fmt.Sprintf("/etc/letsencrypt/live/zboard-%d", cert.ID), fmt.Sprintf("/etc/letsencrypt/archive/zboard-%d", cert.ID), fmt.Sprintf("/etc/letsencrypt/renewal/zboard-%d.conf", cert.ID)}
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		quoted = append(quoted, shellQuote(p))
	}
	script := "set -eu; set -- " + strings.Join(quoted, " ") + `; for target in "$@"; do cursor=$target; while [ "$cursor" != / ]; do [ ! -L "$cursor" ] || exit 73; cursor=$(dirname "$cursor"); done; done; rm -rf -- "$@"`
	return "timeout -k 5 150 sh -c " + shellQuote(script), true
}
func (h *handlers) retirementSSH(ctx context.Context, node model.Node, command string) error {
	if err := h.validateNodeSSH(node); err != nil {
		return err
	}
	if err := sshadapter.ValidateHostKeyFingerprint(node.SSHHostKeyFingerprint); err != nil {
		return err
	}
	credential, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return err
	}
	passphrase := ""
	if normalizeSSHAuthMethod(node.SSHAuthMethod) == sshAuthPrivateKey {
		passphrase, err = h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
		if err != nil {
			return err
		}
	}
	result, err := sshadapter.Dial(ctx, sshadapter.DialRequest{Host: node.SSHHost, Port: node.SSHPort, User: node.SSHUser, AuthMethod: normalizeSSHAuthMethod(node.SSHAuthMethod), Credential: credential, PrivateKeyPassphrase: passphrase, ExpectedHostFingerprint: node.SSHHostKeyFingerprint})
	if err != nil {
		return err
	}
	defer result.Client.Close()
	remote := h.newSSHRemoteSession(result.Client, node)
	defer remote.Close()
	stop := context.AfterFunc(ctx, func() { _ = remote.Close(); _ = result.Client.Close() })
	defer stop()
	_, err = remote.Run(command, true)
	return err
}
