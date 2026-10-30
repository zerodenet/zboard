package networkstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Material is adapter-private: encrypted credentials never enter progress APIs.
type retirementMaterial struct {
	Node                  model.Node
	DNS                   model.ManagedDNSRecord
	Certificate           model.ManagedCertificate
	Shared                bool
	Mode                  string
	RemoteCleanupPossible bool
	RemoteCleanupReason   string
}

func retirementHostExclusive(tx *gorm.DB, node model.Node) error {
	var count int64
	q := tx.Model(&model.Node{}).Where("id <> ? AND lifecycle_status <> ?", node.ID, "archived").Where("(LOWER(TRIM(ssh_host)) = ? AND ssh_port = ?) OR (ssh_host_key_fingerprint <> '' AND ssh_host_key_fingerprint = ?)", strings.ToLower(strings.TrimSpace(node.SSHHost)), node.SSHPort, node.SSHHostKeyFingerprint)
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return retirementBlocked("SSH 主机仍被其他节点使用，拒绝卸载共享主机；可选择仅移除面板管理")
	}
	return nil
}
func (s NodeRetirement) material(ctx context.Context, id uint, runID string, step network.RetirementStep) (out retirementMaterial, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := retirementLease(tx, id, runID)
		if err != nil {
			return err
		}
		found := false
		var persisted network.RetirementStep
		for _, expected := range retirementView(row).Steps {
			if expected.ID == step.ID && expected.Kind == step.Kind && expected.State == "running" {
				found = true
				persisted = expected
			}
		}
		if !found || (row.Mode != "retire" && row.Mode != "delete") {
			return jobs.ErrInvalid
		}
		if err := tx.First(&out.Node, id).Error; err != nil {
			return err
		}
		out.Mode = row.Mode
		if out.Node.LifecycleStatus != "deleting" && !(row.Mode == "delete" && out.Node.LifecycleStatus == "archived") {
			return jobs.ErrConflict
		}
		out.RemoteCleanupPossible = true
		if row.Mode == "delete" && step.Kind != "dns" {
			n := out.Node
			if n.SSHHost == "" || n.SSHUser == "" || n.SSHPwd == "" || n.SSHHostKeyFingerprint == "" || n.ConnectorLastSeenAt == nil || n.ConnectorLastSeenAt.Before(time.Now().Add(-2*time.Minute)) {
				out.RemoteCleanupPossible = false
				out.RemoteCleanupReason = "节点离线或没有可用的已验证 SSH，远端程序和文件未能清理。"
			}
			if e := retirementHostExclusive(tx, n); n.SSHHost != "" && e != nil {
				var blocked *network.RetirementBlocked
				if !errors.As(e, &blocked) {
					return e
				}
				out.RemoteCleanupPossible = false
				out.RemoteCleanupReason = "SSH 主机仍由其他节点使用，未清理共享主机上的程序和文件。"
			}
		}
		switch step.Kind {
		case "dns":
			if err := tx.First(&out.DNS, "id = ? AND node_id = ?", step.ID, id).Error; err != nil {
				return err
			}
			if row.Mode == "delete" && (persisted.ProviderAccountID != out.DNS.ProviderAccountID || persisted.ProviderZoneID != out.DNS.ProviderZoneID || persisted.ProviderRecordID != out.DNS.ProviderRecordID) {
				return jobs.ErrConflict
			}
			var count int64
			q := tx.Model(&model.ManagedDNSRecord{}).Where("node_id <> ?", id).
				Where("(domain_name = ? AND record_type = ?) OR (provider_zone_id <> '' AND provider_record_id <> '' AND provider_zone_id = ? AND provider_record_id = ?)", out.DNS.DomainName, out.DNS.RecordType, out.DNS.ProviderZoneID, out.DNS.ProviderRecordID)
			if err := q.Count(&count).Error; err != nil {
				return err
			}
			out.Shared = count > 0
			if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id <> ? AND LOWER(address) = ?", id, strings.ToLower(out.DNS.DomainName)).Count(&count).Error; err != nil {
				return err
			}
			out.Shared = out.Shared || count > 0
		case "certificate":
			if err := retirementHostExclusive(tx, out.Node); err != nil && row.Mode != "delete" {
				return err
			}
			if err := tx.First(&out.Certificate, "id = ? AND node_id = ?", step.ID, id).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Table("certificate_protocol_endpoints AS c").Joins("JOIN protocol_endpoints AS p ON p.id = c.protocol_endpoint_id").Where("c.managed_certificate_id = ? AND p.node_id <> ?", step.ID, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return retirementBlocked("证书被其他节点引用，拒绝清理")
			}
		case "zero":
			if row.Mode != "delete" {
				return retirementHostExclusive(tx, out.Node)
			}
			return nil
		default:
			return jobs.ErrInvalid
		}
		return nil
	})
	return
}

func retirementBlocked(reason string) error { return &network.RetirementBlocked{Reason: reason} }

func (s NodeRetirement) Material(ctx context.Context, id uint, runID string, step network.RetirementStep) (network.RetirementMaterial, error) {
	m, err := s.material(ctx, id, runID, step)
	if err != nil {
		return network.RetirementMaterial{}, err
	}
	n, d, c := m.Node, m.DNS, m.Certificate
	return network.RetirementMaterial{SSH: network.NodeCleanupSnapshot{NodeID: n.ID, Host: n.SSHHost, Port: n.SSHPort, User: n.SSHUser, AuthMethod: n.SSHAuthMethod, CredentialCiphertext: n.SSHPwd, PassphraseCiphertext: n.SSHPrivateKeyPassphrase, PrivilegeMode: n.SSHPrivilegeMode, PrivilegePasswordCiphertext: n.SSHPrivilegePassword, HostKeyFingerprint: n.SSHHostKeyFingerprint}, Mode: m.Mode, RemoteCleanupPossible: m.RemoteCleanupPossible, RemoteCleanupReason: m.RemoteCleanupReason, DNS: network.ManagedDNSRecord{ID: d.ID, NodeID: d.NodeID, ProviderAccountID: d.ProviderAccountID, ProviderZoneID: d.ProviderZoneID, ProviderRecordID: d.ProviderRecordID, DomainName: d.DomainName, RecordType: d.RecordType, RecordValue: d.RecordValue, TTL: d.TTL, Proxied: d.Proxied, ObservedHash: d.ObservedHash, LastSyncedAt: d.LastSyncedAt}, CertificateID: c.ID, CertPath: c.CertPath, KeyPath: c.KeyPath, Shared: m.Shared}, nil
}
