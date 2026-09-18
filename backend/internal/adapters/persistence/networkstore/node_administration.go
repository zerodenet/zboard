package networkstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeAdministration struct{ DB *gorm.DB }

func (s NodeAdministration) LoadNodeAdministration(ctx context.Context, actor, id uint) (out network.NodeAdministrationSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := nodeAdministrator(tx, actor); err != nil {
			return err
		}
		var row model.Node
		if err := tx.First(&row, id).Error; err != nil {
			return nodeAdministrationError(err)
		}
		out = nodeAdministrationSnapshot(row)
		return nil
	})
	return
}

func (s NodeAdministration) CreateNodeAdministration(ctx context.Context, actor uint, change network.NodeCreateChange) (out network.NodeAdministrationRecord, err error) {
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row := model.Node{
			Name: change.Name, Region: change.Region, Address: change.Address,
			NodeCredential: change.NodeCredentialCiphertext, NodeCredentialPrefix: change.NodeCredentialPrefix,
			CommunicationProtocol: change.CommunicationProtocol, Status: 0, LifecycleStatus: "active",
			Config: change.Config, IsEnabled: change.IsEnabled, Remark: change.Remark, IsOnline: false,
			SSHHost: change.SSHHost, SSHPort: change.SSHPort, SSHUser: change.SSHUser,
			SSHAuthMethod: change.SSHAuthMethod, SSHPwd: change.SSHPwdCiphertext,
			SSHPrivateKeyPassphrase: change.SSHPassphraseCiphertext, SSHPrivilegeMode: change.SSHPrivilegeMode,
			SSHPrivilegePassword: change.SSHPrivilegeCiphertext,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.NodeKernelState{NodeID: row.ID, Status: "unknown", Phase: "idle", RecommendedAction: "detect"}).Error; err != nil {
			return err
		}
		if err := createNodeAdministrationAudit(tx, admin, "node.create", row.ID, fmt.Sprintf("region=%s", row.Region)); err != nil {
			return err
		}
		out = nodeAdministrationRecord(row)
		return nil
	})
	return
}

func (s NodeAdministration) UpdateNodeAdministration(ctx context.Context, actor uint, request network.NodeUpdateRequest) (out network.NodeAdministrationRecord, err error) {
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, request.ID)
		if err != nil {
			return err
		}
		updates := map[string]any{}
		if request.Name != nil {
			updates["name"] = *request.Name
		}
		if request.Region != nil {
			updates["region"] = *request.Region
		}
		if request.Address != nil {
			updates["address"] = *request.Address
		}
		if request.Remark != nil {
			updates["remark"] = *request.Remark
		}
		if request.LifecycleStatus != nil {
			updates["lifecycle_status"] = *request.LifecycleStatus
			if *request.LifecycleStatus != "active" {
				updates["is_enabled"] = false
			}
		}
		if request.IsEnabled != nil {
			lifecycle := row.LifecycleStatus
			if request.LifecycleStatus != nil {
				lifecycle = *request.LifecycleStatus
			}
			if lifecycle != "" && lifecycle != "active" && *request.IsEnabled {
				return &network.NodeAdministrationValidation{Message: "节点信息校验失败。", Fields: map[string]string{"lifecycle_status": "请先将生命周期恢复为正常，再启用对外服务。"}}
			}
			updates["is_enabled"] = *request.IsEnabled
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := createNodeAdministrationAudit(tx, admin, "node.update", row.ID, "metadata or lifecycle updated"); err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		out = nodeAdministrationRecord(row)
		return nil
	})
	return
}

func (s NodeAdministration) UpdateNodeSSHConfiguration(ctx context.Context, actor, id uint, change network.NodeSSHConfigurationChange) (out network.NodeAdministrationRecord, err error) {
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, id)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"ssh_host": change.SSHHost, "ssh_port": change.SSHPort, "ssh_user": change.SSHUser,
			"ssh_auth_method": change.SSHAuthMethod, "ssh_pwd": change.SSHPwdCiphertext,
			"ssh_private_key_passphrase": change.SSHPassphraseCiphertext,
			"ssh_privilege_mode":         change.SSHPrivilegeMode, "ssh_privilege_password": change.SSHPrivilegeCiphertext,
			"ssh_verified_at": nil,
		}
		if change.ResetHostKey {
			updates["ssh_host_key_fingerprint"] = ""
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := createNodeAdministrationAudit(tx, admin, "node.ssh_config.update", row.ID, fmt.Sprintf("auth_method=%s privilege_mode=%s target_changed=%t", change.SSHAuthMethod, change.SSHPrivilegeMode, change.ResetHostKey)); err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		out = nodeAdministrationRecord(row)
		return nil
	})
	return
}

func (s NodeAdministration) ResetNodeSSHHostKey(ctx context.Context, actor, id uint) error {
	if s.DB == nil {
		return network.ErrNodeAdministrationUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, id)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", row.ID).Updates(map[string]any{"ssh_host_key_fingerprint": "", "ssh_verified_at": nil}).Error; err != nil {
			return err
		}
		return createNodeAdministrationAudit(tx, admin, "node.ssh_host_key.reset", row.ID, "next successful SSH connection will enroll the host key")
	})
}

func (s NodeAdministration) RotateNodeCredential(ctx context.Context, actor, id uint, kind network.NodeCredentialKind, change network.NodeCredentialChange) error {
	if s.DB == nil {
		return network.ErrNodeAdministrationUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, id)
		if err != nil {
			return err
		}
		updates := map[string]any{}
		action := ""
		switch kind {
		case network.NodeCredentialConnector:
			updates = map[string]any{"node_credential": change.Ciphertext, "node_credential_prefix": change.Prefix, "node_credential_revoked_at": nil, "connector_last_seen_at": nil}
			action = "node.connector_credential.rotate"
		case network.NodeCredentialTraffic:
			updates = map[string]any{"traffic_secret": change.Ciphertext, "traffic_secret_prefix": change.Prefix, "traffic_secret_revoked_at": nil}
			action = "node.traffic_credential.rotate"
		default:
			return network.ErrNodeAdministrationUnavailable
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		return createNodeAdministrationAudit(tx, admin, action, row.ID, change.Prefix)
	})
}

func (s NodeAdministration) RevokeNodeCredential(ctx context.Context, actor, id uint, kind network.NodeCredentialKind, now time.Time) error {
	if s.DB == nil {
		return network.ErrNodeAdministrationUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, id)
		if err != nil {
			return err
		}
		condition, updates, action := "", map[string]any{}, ""
		switch kind {
		case network.NodeCredentialConnector:
			condition = "node_credential <> '' AND node_credential_revoked_at IS NULL"
			updates = map[string]any{"node_credential_revoked_at": now, "connector_last_seen_at": nil}
			action = "node.connector_credential.revoke"
		case network.NodeCredentialTraffic:
			condition = "traffic_secret <> '' AND traffic_secret_revoked_at IS NULL"
			updates = map[string]any{"traffic_secret_revoked_at": now}
			action = "node.traffic_credential.revoke"
		default:
			return network.ErrNodeAdministrationUnavailable
		}
		result := tx.Model(&model.Node{}).Where("id = ? AND "+condition, row.ID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return network.ErrNodeCredentialNotFound
		}
		return createNodeAdministrationAudit(tx, admin, action, row.ID, "credential revoked")
	})
}

func (s NodeAdministration) RecordNodeSSHVerification(ctx context.Context, actor, id uint, at time.Time, verified bool) (out network.NodeAdministrationRecord, err error) {
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := nodeAdministrator(tx, actor); err != nil {
			return err
		}
		row, err := lockNodeAdministration(tx, id)
		if err != nil {
			return err
		}
		updates := map[string]any{"last_sync_at": at, "ssh_verified_at": nil}
		if verified {
			updates["ssh_verified_at"] = at
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		out = nodeAdministrationRecord(row)
		return nil
	})
	return
}

func nodeAdministrator(tx *gorm.DB, actor uint) (model.User, error) {
	admin, err := providerAdmin(tx, actor)
	if errors.Is(err, network.ErrProviderPermission) {
		return model.User{}, network.ErrNodeAdministrationPermission
	}
	return admin, err
}

func lockNodeAdministration(tx *gorm.DB, id uint) (model.Node, error) {
	var row model.Node
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
		return row, nodeAdministrationError(err)
	}
	if row.LifecycleStatus == "deleting" {
		return row, network.ErrNodeAdministrationDeleting
	}
	return row, nil
}

func nodeAdministrationError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.ErrNodeAdministrationNotFound
	}
	return err
}

func createNodeAdministrationAudit(tx *gorm.DB, admin model.User, action string, nodeID uint, detail string) error {
	return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("node:%d", nodeID), Detail: detail}).Error
}

func nodeAdministrationSnapshot(row model.Node) network.NodeAdministrationSnapshot {
	return network.NodeAdministrationSnapshot{
		Node: nodeAdministrationRecord(row), NodeCredentialCiphertext: row.NodeCredential,
		SSHPwdCiphertext: row.SSHPwd, SSHPassphraseCiphertext: row.SSHPrivateKeyPassphrase,
		SSHPrivilegeCiphertext: row.SSHPrivilegePassword, TrafficSecretCiphertext: row.TrafficSecret,
	}
}

func nodeAdministrationRecord(row model.Node) network.NodeAdministrationRecord {
	return network.NodeAdministrationRecord{
		ID: row.ID, Name: row.Name, Region: row.Region, Address: row.Address,
		NodeCredentialPrefix: row.NodeCredentialPrefix, NodeCredentialRevokedAt: row.NodeCredentialRevokedAt,
		CommunicationProtocol: row.CommunicationProtocol, Status: row.Status, LifecycleStatus: row.LifecycleStatus,
		Config: row.Config, IsEnabled: row.IsEnabled, Remark: row.Remark, IsOnline: row.IsOnline,
		LastSeenAt: row.LastSeenAt, LastSyncAt: row.LastSyncAt, Version: row.Version,
		SSHHost: row.SSHHost, SSHPort: row.SSHPort, SSHUser: row.SSHUser, SSHAuthMethod: row.SSHAuthMethod,
		SSHPrivilegeMode: row.SSHPrivilegeMode, SSHPrivilegeConfigured: row.SSHPrivilegePassword != "",
		SSHHostKeyFingerprint: row.SSHHostKeyFingerprint, SSHVerifiedAt: row.SSHVerifiedAt,
		ConnectorLastSeenAt: row.ConnectorLastSeenAt, ConnectorOnline: row.ConnectorOnline,
		UptimeSeconds: row.UptimeSeconds, ActiveFlows: row.ActiveFlows,
		BytesUp: row.BytesUp, BytesDown: row.BytesDown, TrafficSecretPrefix: row.TrafficSecretPrefix,
		TrafficSecretRevokedAt: row.TrafficSecretRevokedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
