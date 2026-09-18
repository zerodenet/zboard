package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNodeAdministrationUnavailable = errors.New("node administration unavailable")
	ErrNodeAdministrationPermission  = errors.New("node administration requires current administrator")
	ErrNodeAdministrationNotFound    = errors.New("node not found")
	ErrNodeAdministrationDeleting    = errors.New("node is deleting")
	ErrNodeCredentialNotFound        = errors.New("node credential not found")
)

type NodeAdministrationValidation struct {
	Message string
	Fields  map[string]string
}

func (e *NodeAdministrationValidation) Error() string { return e.Message }

type NodeAdministrationRecord struct {
	ID                      uint       `json:"id"`
	Name                    string     `json:"name"`
	Region                  string     `json:"region"`
	Address                 string     `json:"address"`
	NodeCredentialPrefix    string     `json:"node_credential_prefix,omitempty"`
	NodeCredentialRevokedAt *time.Time `json:"node_credential_revoked_at,omitempty"`
	CommunicationProtocol   int16      `json:"communication_protocol"`
	Status                  int16      `json:"status"`
	LifecycleStatus         string     `json:"lifecycle_status"`
	Config                  string     `json:"config"`
	IsEnabled               bool       `json:"is_enabled"`
	Remark                  string     `json:"remark"`
	IsOnline                bool       `json:"is_online"`
	LastSeenAt              *time.Time `json:"last_seen_at"`
	LastSyncAt              *time.Time `json:"last_sync_at"`
	Version                 string     `json:"version"`
	SSHHost                 string     `json:"ssh_host"`
	SSHPort                 int        `json:"ssh_port"`
	SSHUser                 string     `json:"ssh_user"`
	SSHAuthMethod           string     `json:"ssh_auth_method"`
	SSHPrivilegeMode        string     `json:"ssh_privilege_mode"`
	SSHPrivilegeConfigured  bool       `json:"ssh_privilege_password_configured"`
	SSHHostKeyFingerprint   string     `json:"ssh_host_key_fingerprint"`
	SSHVerifiedAt           *time.Time `json:"ssh_verified_at,omitempty"`
	ConnectorLastSeenAt     *time.Time `json:"connector_last_seen_at,omitempty"`
	ConnectorOnline         bool       `json:"connector_online"`
	UptimeSeconds           uint64     `json:"uptime_seconds"`
	ActiveFlows             uint64     `json:"active_flows"`
	BytesUp                 uint64     `json:"bytes_up"`
	BytesDown               uint64     `json:"bytes_down"`
	TrafficSecretPrefix     string     `json:"traffic_secret_prefix,omitempty"`
	TrafficSecretRevokedAt  *time.Time `json:"traffic_secret_revoked_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type NodeAdministrationSnapshot struct {
	Node                     NodeAdministrationRecord
	NodeCredentialCiphertext string
	SSHPwdCiphertext         string
	SSHPassphraseCiphertext  string
	SSHPrivilegeCiphertext   string
	TrafficSecretCiphertext  string
}

type NodeCreateChange struct {
	Name                     string
	Region                   string
	Address                  string
	NodeCredentialCiphertext string
	NodeCredentialPrefix     string
	CommunicationProtocol    int16
	Config                   string
	IsEnabled                bool
	Remark                   string
	SSHHost                  string
	SSHPort                  int
	SSHUser                  string
	SSHAuthMethod            string
	SSHPwdCiphertext         string
	SSHPassphraseCiphertext  string
	SSHPrivilegeMode         string
	SSHPrivilegeCiphertext   string
}

type NodeUpdateRequest struct {
	ID              uint
	Name            *string
	Region          *string
	Address         *string
	Remark          *string
	LifecycleStatus *string
	IsEnabled       *bool
}

type NodeSSHConfigurationChange struct {
	SSHHost                 string
	SSHPort                 int
	SSHUser                 string
	SSHAuthMethod           string
	SSHPwdCiphertext        string
	SSHPassphraseCiphertext string
	SSHPrivilegeMode        string
	SSHPrivilegeCiphertext  string
	ResetHostKey            bool
}

type NodeCredentialKind string

const (
	NodeCredentialConnector NodeCredentialKind = "connector"
	NodeCredentialTraffic   NodeCredentialKind = "traffic"
)

type NodeCredentialChange struct {
	Ciphertext string
	Prefix     string
}

type NodeAdministrationRepository interface {
	LoadNodeAdministration(context.Context, uint, uint) (NodeAdministrationSnapshot, error)
	CreateNodeAdministration(context.Context, uint, NodeCreateChange) (NodeAdministrationRecord, error)
	UpdateNodeAdministration(context.Context, uint, NodeUpdateRequest) (NodeAdministrationRecord, error)
	UpdateNodeSSHConfiguration(context.Context, uint, uint, NodeSSHConfigurationChange) (NodeAdministrationRecord, error)
	ResetNodeSSHHostKey(context.Context, uint, uint) error
	RotateNodeCredential(context.Context, uint, uint, NodeCredentialKind, NodeCredentialChange) error
	RevokeNodeCredential(context.Context, uint, uint, NodeCredentialKind, time.Time) error
	RecordNodeSSHVerification(context.Context, uint, uint, time.Time, bool) (NodeAdministrationRecord, error)
}

type NodeAdministration struct{ Repository NodeAdministrationRepository }

func (s NodeAdministration) Load(ctx context.Context, actor, id uint) (NodeAdministrationSnapshot, error) {
	if s.Repository == nil || actor == 0 || id == 0 {
		return NodeAdministrationSnapshot{}, ErrNodeAdministrationUnavailable
	}
	return s.Repository.LoadNodeAdministration(ctx, actor, id)
}

func (s NodeAdministration) Create(ctx context.Context, actor uint, change NodeCreateChange) (NodeAdministrationRecord, error) {
	if s.Repository == nil || actor == 0 {
		return NodeAdministrationRecord{}, ErrNodeAdministrationUnavailable
	}
	change.Name = strings.TrimSpace(change.Name)
	change.Region = strings.TrimSpace(change.Region)
	change.Address = strings.TrimSpace(change.Address)
	change.Remark = strings.TrimSpace(change.Remark)
	if change.Name == "" {
		return NodeAdministrationRecord{}, nodeAdministrationValidation("name", "请输入主机名称。")
	}
	if change.CommunicationProtocol == 0 {
		change.CommunicationProtocol = 1
	}
	if change.SSHPort == 0 {
		change.SSHPort = 22
	}
	return s.Repository.CreateNodeAdministration(ctx, actor, change)
}

func (s NodeAdministration) Update(ctx context.Context, actor uint, request NodeUpdateRequest) (NodeAdministrationRecord, error) {
	if s.Repository == nil || actor == 0 || request.ID == 0 {
		return NodeAdministrationRecord{}, ErrNodeAdministrationUnavailable
	}
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		if value == "" {
			return NodeAdministrationRecord{}, nodeAdministrationValidation("name", "请输入主机名称。")
		}
		request.Name = &value
	}
	request.Region = trimmedNodeAdministrationString(request.Region)
	request.Address = trimmedNodeAdministrationString(request.Address)
	request.Remark = trimmedNodeAdministrationString(request.Remark)
	if request.LifecycleStatus != nil {
		value := strings.ToLower(strings.TrimSpace(*request.LifecycleStatus))
		if value != "active" && value != "maintenance" && value != "retired" {
			return NodeAdministrationRecord{}, nodeAdministrationValidation("lifecycle_status", "请选择有效的生命周期。")
		}
		request.LifecycleStatus = &value
		if value != "active" && request.IsEnabled != nil && *request.IsEnabled {
			return NodeAdministrationRecord{}, nodeAdministrationValidation("is_enabled", "维护或退役节点不能承载对外服务。")
		}
	}
	if request.Name == nil && request.Region == nil && request.Address == nil && request.Remark == nil && request.LifecycleStatus == nil && request.IsEnabled == nil {
		return NodeAdministrationRecord{}, &NodeAdministrationValidation{Message: "no valid update fields"}
	}
	return s.Repository.UpdateNodeAdministration(ctx, actor, request)
}

func (s NodeAdministration) SaveSSH(ctx context.Context, actor, id uint, change NodeSSHConfigurationChange) (NodeAdministrationRecord, error) {
	if s.Repository == nil || actor == 0 || id == 0 {
		return NodeAdministrationRecord{}, ErrNodeAdministrationUnavailable
	}
	return s.Repository.UpdateNodeSSHConfiguration(ctx, actor, id, change)
}

func (s NodeAdministration) ResetSSHHostKey(ctx context.Context, actor, id uint) error {
	if s.Repository == nil || actor == 0 || id == 0 {
		return ErrNodeAdministrationUnavailable
	}
	return s.Repository.ResetNodeSSHHostKey(ctx, actor, id)
}

func (s NodeAdministration) RotateCredential(ctx context.Context, actor, id uint, kind NodeCredentialKind, change NodeCredentialChange) error {
	if s.Repository == nil || actor == 0 || id == 0 || change.Ciphertext == "" || change.Prefix == "" || !validNodeCredentialKind(kind) {
		return ErrNodeAdministrationUnavailable
	}
	return s.Repository.RotateNodeCredential(ctx, actor, id, kind, change)
}

func (s NodeAdministration) RevokeCredential(ctx context.Context, actor, id uint, kind NodeCredentialKind) error {
	if s.Repository == nil || actor == 0 || id == 0 || !validNodeCredentialKind(kind) {
		return ErrNodeAdministrationUnavailable
	}
	return s.Repository.RevokeNodeCredential(ctx, actor, id, kind, time.Now().UTC())
}

func (s NodeAdministration) RecordSSHVerification(ctx context.Context, actor, id uint, at time.Time, verified bool) (NodeAdministrationRecord, error) {
	if s.Repository == nil || actor == 0 || id == 0 || at.IsZero() {
		return NodeAdministrationRecord{}, ErrNodeAdministrationUnavailable
	}
	return s.Repository.RecordNodeSSHVerification(ctx, actor, id, at, verified)
}

func validNodeCredentialKind(kind NodeCredentialKind) bool {
	return kind == NodeCredentialConnector || kind == NodeCredentialTraffic
}

func nodeAdministrationValidation(field, message string) error {
	return &NodeAdministrationValidation{Message: "节点信息校验失败。", Fields: map[string]string{field: message}}
}

func trimmedNodeAdministrationString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
