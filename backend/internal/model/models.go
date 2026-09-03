package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID              uint           `json:"id" gorm:"primaryKey"`
	AccountName     string         `json:"account_name" gorm:"size:80"`
	Email           string         `json:"email" gorm:"size:128;uniqueIndex;not null"`
	Password        string         `json:"-" gorm:"size:255;not null"`
	EmailVerifiedAt *time.Time     `json:"email_verified_at"`
	LastLoginAt     *time.Time     `json:"last_login_at"`
	IsAdmin         bool           `json:"is_admin" gorm:"default:false"`
	Status          string         `json:"status" gorm:"size:20;default:active"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

// Installation is the one-time, database-backed installation marker and the
// public site configuration collected by the setup wizard. ID is always 1.
type Installation struct {
	ID                uint      `json:"-" gorm:"primaryKey"`
	SiteName          string    `json:"site_name" gorm:"size:80;not null"`
	SiteURL           string    `json:"site_url" gorm:"size:255;not null"`
	AllowRegistration bool      `json:"allow_registration" gorm:"not null;default:true"`
	InstalledAt       time.Time `json:"installed_at" gorm:"not null"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Plan struct {
	ID                     uint       `json:"id" gorm:"primaryKey"`
	Name                   string     `json:"name" gorm:"size:80;not null"`
	Slug                   string     `json:"slug" gorm:"size:80;uniqueIndex;not null"`
	Summary                string     `json:"summary" gorm:"size:255"`
	Description            string     `json:"description" gorm:"type:text"`
	NodeGroupID            uint       `json:"node_group_id" gorm:"index;not null"`
	TrafficBytes           int64      `json:"traffic_bytes" gorm:"not null;default:0"`
	SpeedLimitMbps         int        `json:"speed_limit_mbps" gorm:"not null;default:0"`
	MaxActiveSubscriptions int        `json:"max_active_subscriptions" gorm:"not null;default:0"`
	IsRenewable            bool       `json:"is_renewable" gorm:"not null;default:true"`
	DeviceLimit            int        `json:"device_limit" gorm:"not null;default:1"`
	FamilyLimit            int        `json:"family_limit" gorm:"not null;default:0"`
	ResetPolicy            int16      `json:"reset_policy" gorm:"not null;default:0"`
	TrafficCalcMode        int16      `json:"traffic_calc_mode" gorm:"not null;default:0"`
	IsActive               bool       `json:"is_active" gorm:"default:true"`
	SortOrder              int        `json:"sort_order" gorm:"default:0"`
	Revision               uint64     `json:"revision" gorm:"not null;default:1"`
	SKUs                   []PlanSKU  `json:"skus,omitempty" gorm:"foreignKey:PlanID"`
	NodeGroup              *NodeGroup `json:"node_group,omitempty" gorm:"foreignKey:NodeGroupID"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// PlanSKU defines how a plan is sold. Billing cadence, allowed operations and
// entitlement fulfillment are independent concerns. Subscription entitlements
// belong to Plan and are snapshotted into Order. TrafficBytes remains as
// storage for an explicit traffic-addon grant only.
type PlanSKU struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	PlanID          uint      `json:"plan_id" gorm:"index;not null"`
	Code            string    `json:"code" gorm:"size:80;uniqueIndex;not null"`
	Name            string    `json:"name" gorm:"size:80;not null"`
	SKUType         string    `json:"sku_type" gorm:"size:20;not null;default:new"`
	BillingMode     string    `json:"billing_mode" gorm:"size:20;not null;default:periodic"`
	EntitlementMode string    `json:"entitlement_mode" gorm:"size:24;not null;default:plan"`
	RenewalEffect   string    `json:"renewal_effect" gorm:"size:32;not null;default:extend_only"`
	BillingUnit     string    `json:"billing_unit" gorm:"size:16;not null"`
	BillingValue    int       `json:"billing_value" gorm:"not null"`
	PriceCents      int64     `json:"price_cents" gorm:"not null"`
	Currency        string    `json:"currency" gorm:"size:8;not null"`
	TrafficBytes    int64     `json:"-" gorm:"not null"`
	DeviceLimit     int       `json:"-" gorm:"not null"`
	SpeedLimitMbps  int       `json:"-" gorm:"not null;default:0"`
	IsActive        bool      `json:"is_active" gorm:"default:true"`
	SortOrder       int       `json:"sort_order" gorm:"default:0"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Subscription struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	UserID            uint       `json:"user_id" gorm:"index"`
	PlanID            uint       `json:"plan_id" gorm:"index"`
	PlanSKUID         uint       `json:"plan_sku_id" gorm:"column:plan_sku_id;index"`
	NodeGroupID       uint       `json:"node_group_id" gorm:"index;not null"`
	SubscriptionType  int16      `json:"subscription_type" gorm:"not null;default:1"`
	StartAt           time.Time  `json:"start_at"`
	EndAt             time.Time  `json:"end_at"`
	Status            string     `json:"status" gorm:"size:20;default:active"`
	FlowTotal         int64      `json:"flow_total" gorm:"default:0"`
	FlowUsed          int64      `json:"flow_used" gorm:"default:0"`
	SpeedLimitMbps    int        `json:"speed_limit_mbps" gorm:"not null;default:0"`
	DeviceLimit       int        `json:"device_limit" gorm:"not null;default:1"`
	FamilyLimit       int        `json:"family_limit" gorm:"not null;default:0"`
	RenewalPriceMinor int64      `json:"renewal_price_minor" gorm:"not null;default:0"`
	ResetPolicy       int16      `json:"reset_policy" gorm:"not null;default:0"`
	NextResetAt       *time.Time `json:"next_reset_at" gorm:"index"`
	TrafficCalcMode   int16      `json:"traffic_calc_mode" gorm:"not null;default:0"`
	Config            string     `json:"config" gorm:"type:json"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Order struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	UserID               uint       `json:"user_id" gorm:"index"`
	SubscriptionID       uint       `json:"subscription_id" gorm:"index"`
	PlanID               uint       `json:"plan_id" gorm:"index"`
	PlanSKUID            uint       `json:"plan_sku_id" gorm:"column:plan_sku_id;index"`
	TradeNo              string     `json:"trade_no" gorm:"size:64;uniqueIndex"`
	OrderType            string     `json:"order_type" gorm:"size:20;not null;default:new"`
	TargetSubscriptionID *uint      `json:"target_subscription_id" gorm:"index"`
	AmountCents          int64      `json:"amount_cents"`
	PayableAmount        int64      `json:"payable_amount"`
	PaidAmount           int64      `json:"paid_amount"`
	RefundAmount         int64      `json:"refund_amount"`
	DiscountAmount       int64      `json:"discount_amount"`
	Currency             string     `json:"currency" gorm:"size:16;default:USD"`
	Channel              string     `json:"channel" gorm:"size:32"`
	ProviderTradeNo      *string    `json:"provider_trade_no" gorm:"size:128;uniqueIndex"`
	Status               string     `json:"status" gorm:"size:32;default:pending"`
	PlanName             string     `json:"plan_name" gorm:"size:80;not null"`
	SKUName              string     `json:"sku_name" gorm:"size:80;not null"`
	BillingUnit          string     `json:"billing_unit" gorm:"size:16;not null"`
	BillingValue         int        `json:"billing_value" gorm:"not null"`
	RenewalEffect        string     `json:"renewal_effect" gorm:"size:32;not null;default:none"`
	TrafficBytes         int64      `json:"traffic_bytes" gorm:"not null"`
	DeviceLimit          int        `json:"device_limit" gorm:"not null"`
	SpeedLimitMbps       int        `json:"speed_limit_mbps" gorm:"not null;default:0"`
	RawCallback          string     `json:"raw_callback" gorm:"type:text"`
	PaidAt               *time.Time `json:"paid_at"`
	CanceledAt           *time.Time `json:"canceled_at"`
	FulfilledAt          *time.Time `json:"fulfilled_at"`
	RefundedAt           *time.Time `json:"refunded_at"`
	FailureReason        string     `json:"failure_reason" gorm:"size:255"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type Node struct {
	ID                      uint             `json:"id" gorm:"primaryKey"`
	Name                    string           `json:"name" gorm:"size:64;uniqueIndex"`
	Region                  string           `json:"region" gorm:"size:32"`
	Address                 string           `json:"address" gorm:"size:128"`
	NodeCredential          string           `json:"-" gorm:"type:text"`
	NodeCredentialPrefix    string           `json:"node_credential_prefix,omitempty" gorm:"size:12"`
	NodeCredentialRevokedAt *time.Time       `json:"node_credential_revoked_at,omitempty"`
	CommunicationProtocol   int16            `json:"communication_protocol" gorm:"not null;default:1"`
	Status                  int16            `json:"status" gorm:"not null;default:0"`
	LifecycleStatus         string           `json:"lifecycle_status" gorm:"size:20;not null;default:active"`
	Config                  string           `json:"config" gorm:"type:json"`
	IsEnabled               bool             `json:"is_enabled" gorm:"not null;default:true"`
	Remark                  string           `json:"remark" gorm:"size:255"`
	IsOnline                bool             `json:"is_online" gorm:"default:false"`
	LastSeenAt              *time.Time       `json:"last_seen_at"`
	LastSyncAt              *time.Time       `json:"last_sync_at"`
	Version                 string           `json:"version" gorm:"size:64"`
	SSHHost                 string           `json:"ssh_host" gorm:"size:64"`
	SSHPort                 int              `json:"ssh_port" gorm:"default:22"`
	SSHUser                 string           `json:"ssh_user" gorm:"size:64"`
	SSHAuthMethod           string           `json:"ssh_auth_method" gorm:"size:20;not null;default:password"`
	SSHPwd                  string           `json:"-" gorm:"column:ssh_pwd;type:text"`
	SSHPrivateKeyPassphrase string           `json:"-" gorm:"type:text"`
	SSHPrivilegeMode        string           `json:"ssh_privilege_mode" gorm:"size:16;not null;default:none"`
	SSHPrivilegePassword    string           `json:"-" gorm:"type:text"`
	SSHPrivilegeConfigured  bool             `json:"ssh_privilege_password_configured" gorm:"-"`
	SSHHostKeyFingerprint   string           `json:"ssh_host_key_fingerprint" gorm:"size:128"`
	SSHVerifiedAt           *time.Time       `json:"ssh_verified_at,omitempty"`
	ConnectorLastSeenAt     *time.Time       `json:"connector_last_seen_at,omitempty"`
	ConnectorOnline         bool             `json:"connector_online" gorm:"-"`
	UptimeSeconds           uint64           `json:"uptime_seconds"`
	ActiveFlows             uint64           `json:"active_flows"`
	BytesUp                 uint64           `json:"bytes_up"`
	BytesDown               uint64           `json:"bytes_down"`
	TrafficSecret           string           `json:"-" gorm:"column:traffic_secret;type:text"`
	TrafficSecretPrefix     string           `json:"traffic_secret_prefix,omitempty" gorm:"size:12"`
	TrafficSecretRevokedAt  *time.Time       `json:"traffic_secret_revoked_at,omitempty"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
	KernelState             *NodeKernelState `json:"kernel_state,omitempty" gorm:"foreignKey:NodeID"`
}

type NodeKernelState struct {
	NodeID              uint       `json:"node_id" gorm:"primaryKey"`
	Status              string     `json:"status" gorm:"size:24;not null;default:unknown"`
	Phase               string     `json:"phase" gorm:"size:32;not null;default:idle"`
	RecommendedAction   string     `json:"recommended_action" gorm:"size:24;not null;default:detect"`
	PlatformOS          string     `json:"platform_os" gorm:"size:64"`
	Architecture        string     `json:"architecture" gorm:"size:32"`
	Libc                string     `json:"libc" gorm:"size:64"`
	DesiredVersion      string     `json:"desired_version" gorm:"size:64"`
	InstalledVersion    string     `json:"installed_version" gorm:"size:64"`
	DesiredSHA256       string     `json:"desired_sha256" gorm:"size:64"`
	InstalledSHA256     string     `json:"installed_sha256" gorm:"size:64"`
	DesiredConfigSHA256 string     `json:"desired_config_sha256" gorm:"size:64"`
	AppliedConfigSHA256 string     `json:"applied_config_sha256" gorm:"size:64"`
	ServiceStatus       string     `json:"service_status" gorm:"size:24;not null;default:unknown"`
	ControlStatus       string     `json:"control_status" gorm:"size:24;not null;default:unknown"`
	LastError           string     `json:"last_error" gorm:"type:text"`
	ActiveOperationID   *uint      `json:"active_operation_id,omitempty"`
	LastDetectedAt      *time.Time `json:"last_detected_at,omitempty"`
	LastHealthyAt       *time.Time `json:"last_healthy_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type NodeOperation struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	NodeID         uint       `json:"node_id" gorm:"index;not null"`
	OperationType  string     `json:"operation_type" gorm:"size:24;not null"`
	Status         string     `json:"status" gorm:"size:24;index;not null"`
	Phase          string     `json:"phase" gorm:"size:32;not null"`
	RequestedBy    uint       `json:"requested_by" gorm:"not null"`
	DesiredVersion string     `json:"desired_version" gorm:"size:64"`
	DesiredSHA256  string     `json:"desired_sha256" gorm:"size:64"`
	ArtifactURL    string     `json:"artifact_url" gorm:"size:512"`
	ResultSummary  string     `json:"result_summary" gorm:"type:text"`
	Error          string     `json:"error" gorm:"type:text"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ManagedCertificate is a node-scoped ACME certificate asset. Certificate
// material stays on the target node; zboard stores only public metadata and
// the stable paths injected into bound protocol endpoints.
type ManagedCertificate struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	NodeID               uint       `json:"node_id" gorm:"index;not null"`
	ProviderAccountID    *uint      `json:"provider_account_id,omitempty" gorm:"index"`
	Name                 string     `json:"name" gorm:"size:80;not null"`
	Domains              string     `json:"domains" gorm:"type:json;not null"`
	ContactEmail         string     `json:"contact_email" gorm:"size:254;not null"`
	Environment          string     `json:"environment" gorm:"size:16;not null;default:production"`
	ChallengeType        string     `json:"challenge_type" gorm:"size:16;not null;default:http-01"`
	WebrootPath          string     `json:"webroot_path" gorm:"size:255;not null;default:''"`
	Status               string     `json:"status" gorm:"size:24;index;not null;default:pending"`
	CertPath             string     `json:"cert_path" gorm:"size:255"`
	KeyPath              string     `json:"key_path" gorm:"size:255"`
	SerialNumber         string     `json:"serial_number" gorm:"size:128"`
	FingerprintSHA256    string     `json:"fingerprint_sha256" gorm:"size:64"`
	NotBefore            *time.Time `json:"not_before,omitempty"`
	NotAfter             *time.Time `json:"not_after,omitempty" gorm:"index"`
	LastIssuedAt         *time.Time `json:"last_issued_at,omitempty"`
	LastRenewalAttemptAt *time.Time `json:"last_renewal_attempt_at,omitempty"`
	NextRenewalAt        *time.Time `json:"next_renewal_at,omitempty" gorm:"index"`
	AutoRenew            bool       `json:"auto_renew" gorm:"not null;default:true"`
	RenewBeforeDays      int        `json:"renew_before_days" gorm:"not null;default:30"`
	LastError            string     `json:"last_error" gorm:"type:text"`
	Revision             uint64     `json:"revision" gorm:"not null;default:1"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// ProviderAccount is a reusable external-service account. Domain resources
// reference it while credentials remain encrypted and redacted from APIs.
type ProviderAccount struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	ProviderKey          string     `json:"provider_key" gorm:"size:48;index;not null"`
	Name                 string     `json:"name" gorm:"size:80;uniqueIndex;not null"`
	Capabilities         string     `json:"capabilities" gorm:"type:json;not null"`
	CredentialCiphertext string     `json:"-" gorm:"type:text;not null"`
	CredentialPrefix     string     `json:"credential_prefix" gorm:"size:16;not null"`
	Status               string     `json:"status" gorm:"size:24;index;not null;default:pending"`
	LastVerifiedAt       *time.Time `json:"last_verified_at,omitempty"`
	LastError            string     `json:"last_error" gorm:"type:text"`
	Revision             uint64     `json:"revision" gorm:"not null;default:1"`
	CreatedBy            uint       `json:"created_by" gorm:"index;not null"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// ManagedDNSRecord is the desired and observed state of one provider-backed
// DNS record. The handwritten FQDN remains the operator input and NodeID is the
// infrastructure target, not an ownership shortcut.
type ManagedDNSRecord struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	ProviderAccountID uint       `json:"provider_account_id" gorm:"index;not null"`
	NodeID            uint       `json:"node_id" gorm:"index;not null"`
	DomainName        string     `json:"domain_name" gorm:"size:253;not null"`
	RecordType        string     `json:"record_type" gorm:"size:8;not null"`
	RecordValue       string     `json:"record_value" gorm:"size:255;not null"`
	ProviderZoneID    string     `json:"provider_zone_id" gorm:"size:64"`
	ProviderRecordID  string     `json:"provider_record_id" gorm:"size:64"`
	TTL               int        `json:"ttl" gorm:"not null;default:1"`
	Proxied           bool       `json:"proxied" gorm:"not null;default:false"`
	Status            string     `json:"status" gorm:"size:24;index;not null;default:pending"`
	DesiredHash       string     `json:"desired_hash" gorm:"size:64;not null"`
	ObservedHash      string     `json:"observed_hash" gorm:"size:64"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	LastPublicCheckAt *time.Time `json:"last_public_check_at,omitempty"`
	PublicResolved    bool       `json:"public_resolved" gorm:"not null;default:false"`
	LastError         string     `json:"last_error" gorm:"type:text"`
	Revision          uint64     `json:"revision" gorm:"not null;default:1"`
	CreatedBy         uint       `json:"created_by" gorm:"index;not null"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ProviderOperation struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	ProviderAccountID uint       `json:"provider_account_id" gorm:"index;not null"`
	ResourceType      string     `json:"resource_type" gorm:"size:32;not null"`
	ResourceID        uint       `json:"resource_id" gorm:"index;not null"`
	OperationType     string     `json:"operation_type" gorm:"size:24;not null"`
	Status            string     `json:"status" gorm:"size:24;index;not null"`
	Phase             string     `json:"phase" gorm:"size:32;not null"`
	RequestedBy       *uint      `json:"requested_by,omitempty"`
	ResultSummary     string     `json:"result_summary" gorm:"type:text"`
	Error             string     `json:"error" gorm:"type:text"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CertificateProtocolEndpoint struct {
	ID                   uint      `json:"id" gorm:"primaryKey"`
	ManagedCertificateID uint      `json:"managed_certificate_id" gorm:"uniqueIndex:ux_certificate_endpoint,priority:1;index;not null"`
	ProtocolEndpointID   uint      `json:"protocol_endpoint_id" gorm:"uniqueIndex:ux_certificate_endpoint,priority:2;uniqueIndex;not null"`
	CreatedAt            time.Time `json:"created_at"`
}

type CertificateOperation struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	ManagedCertificateID uint       `json:"managed_certificate_id" gorm:"index;not null"`
	NodeID               uint       `json:"node_id" gorm:"index;not null"`
	OperationType        string     `json:"operation_type" gorm:"size:16;not null"`
	Status               string     `json:"status" gorm:"size:24;index;not null"`
	Phase                string     `json:"phase" gorm:"size:32;not null"`
	RequestedBy          *uint      `json:"requested_by,omitempty"`
	ResultSummary        string     `json:"result_summary" gorm:"type:text"`
	Error                string     `json:"error" gorm:"type:text"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// ProtocolEndpoint is the sellable network resource. A Node is only its
// operational host; each host may publish multiple independently configured
// protocols with different addresses, ports and billing multipliers.
type ProtocolEndpoint struct {
	ID                    uint      `json:"id" gorm:"primaryKey"`
	NodeID                uint      `json:"node_id" gorm:"index;not null"`
	Name                  string    `json:"name" gorm:"size:80;not null"`
	RuntimeKey            string    `json:"-" gorm:"size:36;uniqueIndex;not null"`
	Protocol              string    `json:"protocol" gorm:"size:32;not null"`
	Address               string    `json:"address" gorm:"size:255;not null"`
	Port                  int       `json:"port" gorm:"not null"`
	PublicPort            int       `json:"public_port" gorm:"not null"`
	Cipher                int16     `json:"cipher" gorm:"not null;default:0"`
	ParentProtocolID      *uint     `json:"parent_protocol_id" gorm:"index"`
	MultiplierMilli       int64     `json:"multiplier_milli" gorm:"not null;default:1000"`
	ManagedPrincipalReady bool      `json:"managed_principal_ready" gorm:"not null;default:false"`
	MieruPrincipalReady   bool      `json:"mieru_principal_ready" gorm:"not null;default:false"`
	ServerConfig          string    `json:"-" gorm:"type:text"`
	ClientConfig          string    `json:"client_config" gorm:"type:text"`
	OptionalConfig        string    `json:"optional_config" gorm:"type:json"`
	Tags                  string    `json:"tags" gorm:"type:json"`
	IsActive              bool      `json:"is_active" gorm:"default:true"`
	SortOrder             int       `json:"sort_order" gorm:"default:0"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type NodeGroup struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	Name                string    `json:"name" gorm:"size:80;not null"`
	Code                string    `json:"code" gorm:"size:80;uniqueIndex;not null"`
	Description         string    `json:"description" gorm:"size:255"`
	IsEnabled           bool      `json:"is_enabled" gorm:"not null;default:true"`
	Revision            uint64    `json:"revision" gorm:"not null;default:1"`
	ProtocolEndpointIDs []uint    `json:"protocol_endpoint_ids" gorm:"-"`
	PlanCount           int64     `json:"plan_count" gorm:"-"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type NodeGroupEndpoint struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	NodeGroupID        uint      `json:"node_group_id" gorm:"uniqueIndex:ux_node_group_endpoint,priority:1;index;not null"`
	ProtocolEndpointID uint      `json:"protocol_endpoint_id" gorm:"uniqueIndex:ux_node_group_endpoint,priority:2;index;not null"`
	SortOrder          int       `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt          time.Time `json:"created_at"`
}

type SubscriptionToken struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	UserID          uint       `json:"user_id" gorm:"uniqueIndex;not null"`
	SubscriptionID  *uint      `json:"subscription_id,omitempty" gorm:"index"`
	TokenHash       string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	TokenCiphertext string     `json:"-" gorm:"column:token_ciphertext;type:text"`
	TokenPrefix     string     `json:"token_prefix" gorm:"size:12;not null"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// SubscriptionTemplate binds operator-managed metadata and declarative
// customization to one system-owned renderer. Renderer code, credential
// injection and response types remain backend-owned.
type SubscriptionTemplate struct {
	ID            uint            `json:"id" gorm:"primaryKey"`
	Name          string          `json:"name" gorm:"size:80;not null"`
	Slug          string          `json:"slug" gorm:"size:80;uniqueIndex;not null"`
	Description   string          `json:"description" gorm:"size:255"`
	Renderer      string          `json:"renderer" gorm:"size:32;index;not null"`
	Customization json.RawMessage `json:"customization,omitempty" gorm:"type:json;not null"`
	ContentType   string          `json:"content_type,omitempty" gorm:"-"`
	IsActive      bool            `json:"is_active" gorm:"index;not null;default:true"`
	SortOrder     int             `json:"sort_order" gorm:"not null;default:0"`
	Revision      uint64          `json:"revision" gorm:"not null;default:1"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// SubscriptionRuleSet is a reusable, renderer-specific remote rule source.
// Templates reference it by ID and own only the match action and ordering.
type SubscriptionRuleSet struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:80;not null"`
	Description string    `json:"description" gorm:"size:255"`
	Renderer    string    `json:"renderer" gorm:"size:32;uniqueIndex:ux_subscription_rule_set_renderer_tag,priority:1;index;not null"`
	Tag         string    `json:"tag" gorm:"size:64;uniqueIndex:ux_subscription_rule_set_renderer_tag,priority:2;not null"`
	URL         string    `json:"url" gorm:"size:2048;not null"`
	Behavior    string    `json:"behavior,omitempty" gorm:"size:16;not null;default:''"`
	Format      string    `json:"format" gorm:"size:32;not null"`
	Interval    int       `json:"interval" gorm:"not null;default:86400"`
	IsActive    bool      `json:"is_active" gorm:"index;not null;default:true"`
	Revision    uint64    `json:"revision" gorm:"not null;default:1"`
	UsageCount  int64     `json:"usage_count" gorm:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SubscriptionTemplateRuleSetBinding indexes reusable rule-set references
// stored in a template customization. The foreign keys provide deletion
// integrity while the customization retains the mixed library/quick order.
type SubscriptionTemplateRuleSetBinding struct {
	SubscriptionTemplateID uint   `json:"subscription_template_id" gorm:"primaryKey;autoIncrement:false"`
	SubscriptionRuleSetID  uint   `json:"subscription_rule_set_id" gorm:"primaryKey;autoIncrement:false;index"`
	Action                 string `json:"action" gorm:"size:96;not null"`
	Position               int    `json:"position" gorm:"not null"`
}

// ProtocolCredential binds one sellable endpoint credential to one subscription.
// Secrets are encrypted at rest; observable events carry only CredentialID and
// PrincipalKey so a kernel event can be attributed without exposing the secret.
type ProtocolCredential struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID     uint       `json:"subscription_id" gorm:"uniqueIndex:ux_protocol_credential_subscription_endpoint,priority:1;index;not null"`
	UserID             uint       `json:"user_id" gorm:"index;not null"`
	ProtocolEndpointID uint       `json:"protocol_endpoint_id" gorm:"uniqueIndex:ux_protocol_credential_subscription_endpoint,priority:2;index;not null"`
	NodeID             uint       `json:"node_id" gorm:"index;not null"`
	CredentialID       string     `json:"credential_id" gorm:"size:96;uniqueIndex;not null"`
	PrincipalKey       string     `json:"principal_key" gorm:"size:128;uniqueIndex;not null"`
	Secret             string     `json:"-" gorm:"type:text;not null"`
	ListenPort         int        `json:"listen_port" gorm:"not null"`
	PublicPort         int        `json:"public_port" gorm:"not null"`
	Status             string     `json:"status" gorm:"size:20;index;not null;default:active"`
	ExpiresAt          time.Time  `json:"expires_at" gorm:"index;not null"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// FlowUsage is the idempotent cursor for Zero flow.updated/flow.completed
// events. It lets the panel charge only the new cumulative byte delta while a
// flow is active and settle any missing delta from the final completed event.
type FlowUsage struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	NodeID               uint       `json:"node_id" gorm:"uniqueIndex:ux_flow_usage_node_flow,priority:1;index;not null"`
	FlowID               string     `json:"flow_id" gorm:"size:128;uniqueIndex:ux_flow_usage_node_flow,priority:2;not null"`
	ProtocolCredentialID uint       `json:"protocol_credential_id" gorm:"index;not null"`
	SubscriptionID       uint       `json:"subscription_id" gorm:"index;not null"`
	ProtocolEndpointID   uint       `json:"protocol_endpoint_id" gorm:"index;not null"`
	PrincipalKey         string     `json:"principal_key" gorm:"size:128;index;not null"`
	Revision             uint64     `json:"revision" gorm:"not null;default:0"`
	RawBytes             int64      `json:"raw_bytes" gorm:"not null;default:0"`
	UploadBytes          int64      `json:"upload_bytes" gorm:"not null;default:0"`
	DownloadBytes        int64      `json:"download_bytes" gorm:"not null;default:0"`
	UsedBytes            int64      `json:"used_bytes" gorm:"not null;default:0"`
	Status               string     `json:"status" gorm:"size:20;index;not null;default:active"`
	LastEventID          string     `json:"last_event_id" gorm:"size:191;not null"`
	LastSeenAt           time.Time  `json:"last_seen_at" gorm:"index;not null"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type TrafficRecord struct {
	ID                      uint      `json:"id" gorm:"primaryKey"`
	UserID                  uint      `json:"user_id" gorm:"index"`
	SubscriptionID          uint      `json:"subscription_id,omitempty" gorm:"index"`
	NodeID                  uint      `json:"node_id" gorm:"index;uniqueIndex:ux_traffic_node_report,priority:1;uniqueIndex:ux_traffic_node_nonce,priority:1"`
	ProtocolEndpointID      uint      `json:"protocol_endpoint_id" gorm:"index"`
	ReportID                string    `json:"report_id,omitempty" gorm:"size:191;uniqueIndex:ux_traffic_node_report,priority:2"`
	FlowID                  string    `json:"flow_id,omitempty" gorm:"size:128;index"`
	EventType               string    `json:"event_type,omitempty" gorm:"size:32;index"`
	EventRevision           uint64    `json:"event_revision,omitempty"`
	Nonce                   string    `json:"-" gorm:"size:64;uniqueIndex:ux_traffic_node_nonce,priority:2"`
	RawBytes                int64     `json:"raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes"`
	TrafficCalcMode         int16     `json:"traffic_calc_mode"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes"`
	At                      time.Time `json:"record_at" gorm:"index;column:record_at"`
	Meta                    string    `json:"meta" gorm:"type:text"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    *uint     `json:"user_id,omitempty" gorm:"index"`
	Actor     string    `json:"actor" gorm:"size:128"`
	Action    string    `json:"action" gorm:"size:128"`
	Target    string    `json:"target" gorm:"size:128"`
	Detail    string    `json:"detail" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}

// EmailTemplate is operator-authored presentation content. Transactional
// triggers and operational campaigns both copy a template into a durable Task
// before delivery so later edits never rewrite an in-flight or historical
// message.
type EmailTemplate struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name" gorm:"size:80;not null"`
	Slug            string    `json:"slug" gorm:"size:80;uniqueIndex;not null"`
	Category        string    `json:"category" gorm:"size:24;index;not null"`
	TriggerKey      *string   `json:"trigger_key,omitempty" gorm:"size:64;uniqueIndex"`
	SubjectTemplate string    `json:"subject_template" gorm:"size:200;not null"`
	BodyTemplate    string    `json:"body_template" gorm:"type:text;not null"`
	IsActive        bool      `json:"is_active" gorm:"index;not null;default:true"`
	SortOrder       int       `json:"sort_order" gorm:"not null;default:0"`
	Revision        uint64    `json:"revision" gorm:"not null;default:1"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// RegistrationEmailChallenge is a short-lived identity proof. It stores only
// a keyed digest of the one-time code; account creation consumes the challenge
// in the same transaction that creates the user.
type RegistrationEmailChallenge struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	Email           string     `json:"-" gorm:"size:128;uniqueIndex:ux_registration_email_challenge,priority:1;not null"`
	Purpose         string     `json:"-" gorm:"size:32;uniqueIndex:ux_registration_email_challenge,priority:2;not null;default:register"`
	CodeHash        string     `json:"-" gorm:"size:64;not null"`
	RequestedIPHash string     `json:"-" gorm:"size:64;index"`
	Attempts        int        `json:"attempts" gorm:"not null;default:0"`
	LastSentAt      time.Time  `json:"last_sent_at" gorm:"not null"`
	ExpiresAt       time.Time  `json:"expires_at" gorm:"index;not null"`
	ConsumedAt      *time.Time `json:"consumed_at" gorm:"index"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Ticket is the durable support conversation owned by one user. The current
// status is denormalized here for queue queries; every transition is also
// recorded in TicketMessage so the full history remains traceable.
type Ticket struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	TicketNo      string     `json:"ticket_no" gorm:"size:32;uniqueIndex;not null"`
	UserID        uint       `json:"user_id" gorm:"index;not null"`
	Subject       string     `json:"subject" gorm:"size:160;not null"`
	Category      string     `json:"category" gorm:"size:32;index;not null"`
	Priority      int16      `json:"priority" gorm:"index;not null;default:1"`
	Status        string     `json:"status" gorm:"size:24;index;not null;default:open"`
	LastMessageAt time.Time  `json:"last_message_at" gorm:"index;not null"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	ClosedAt      *time.Time `json:"closed_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TicketMessage contains both human replies and system status events.
type TicketMessage struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	TicketID   uint      `json:"ticket_id" gorm:"index;not null"`
	AuthorID   *uint     `json:"author_id" gorm:"index"`
	AuthorRole string    `json:"author_role" gorm:"size:16;not null"`
	Type       string    `json:"type" gorm:"column:message_type;size:16;not null"`
	Body       string    `json:"body" gorm:"type:text;not null"`
	FromStatus string    `json:"from_status" gorm:"size:24;not null"`
	ToStatus   string    `json:"to_status" gorm:"size:24;not null"`
	CreatedAt  time.Time `json:"created_at"`
}

type PaymentEvent struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	OrderID         uint       `json:"order_id" gorm:"index;not null"`
	Provider        string     `json:"provider" gorm:"size:32;uniqueIndex:ux_payment_provider_event,priority:1;not null"`
	ProviderEventID string     `json:"provider_event_id" gorm:"size:128;uniqueIndex:ux_payment_provider_event,priority:2;not null"`
	EventType       string     `json:"event_type" gorm:"size:32;not null"`
	AmountMinor     int64      `json:"amount_minor"`
	SignatureValid  bool       `json:"signature_valid"`
	Payload         string     `json:"payload" gorm:"type:json"`
	ProcessedAt     *time.Time `json:"processed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type SubscriptionMember struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID uint       `json:"subscription_id" gorm:"uniqueIndex:ux_subscription_member,priority:1;index;not null"`
	UserID         uint       `json:"user_id" gorm:"uniqueIndex:ux_subscription_member,priority:2;index;not null"`
	MemberRole     string     `json:"member_role" gorm:"size:16;not null;default:member"`
	Status         string     `json:"status" gorm:"size:20;not null;default:active"`
	JoinedAt       time.Time  `json:"joined_at"`
	RemovedAt      *time.Time `json:"removed_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type UserAPIToken struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	UserID      uint       `json:"user_id" gorm:"index;not null"`
	TokenHash   string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"size:12;not null"`
	Scopes      string     `json:"scopes" gorm:"type:json"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Task struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	Type           string     `json:"type" gorm:"size:32;index;not null"`
	Scope          string     `json:"scope" gorm:"type:json"`
	Content        string     `json:"content" gorm:"type:json"`
	Status         int16      `json:"status" gorm:"index;not null;default:0"`
	Errors         string     `json:"errors" gorm:"type:text"`
	Total          int64      `json:"total"`
	Current        int64      `json:"current"`
	IdempotencyKey string     `json:"idempotency_key" gorm:"size:128;uniqueIndex"`
	Priority       int        `json:"priority" gorm:"index;not null;default:0"`
	ScheduledAt    *time.Time `json:"scheduled_at" gorm:"index"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts" gorm:"not null;default:3"`
	LockedBy       string     `json:"locked_by" gorm:"size:128"`
	LockedUntil    *time.Time `json:"locked_until" gorm:"index"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type TaskItem struct {
	ID         uint       `json:"id" gorm:"primaryKey"`
	TaskID     uint       `json:"task_id" gorm:"uniqueIndex:ux_task_target,priority:1;index;not null"`
	TargetType string     `json:"target_type" gorm:"size:32;uniqueIndex:ux_task_target,priority:2;not null"`
	TargetID   string     `json:"target_id" gorm:"size:128;uniqueIndex:ux_task_target,priority:3;not null"`
	Payload    string     `json:"payload" gorm:"type:json"`
	Status     int16      `json:"status" gorm:"index;not null;default:0"`
	Attempts   int        `json:"attempts"`
	Error      string     `json:"error" gorm:"type:text"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type SystemConfig struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ConfigKey   string    `json:"config_key" gorm:"column:config_key;size:80;uniqueIndex;not null"`
	Name        string    `json:"name" gorm:"size:120;not null"`
	Value       string    `json:"value" gorm:"type:text"`
	ValueType   string    `json:"value_type" gorm:"size:16;not null"`
	Description string    `json:"description" gorm:"size:255"`
	IsPublic    bool      `json:"is_public"`
	IsSecret    bool      `json:"is_secret"`
	Revision    uint64    `json:"revision" gorm:"not null;default:1"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Announcement is a durable site-wide notice. Publishing and visibility are
// evaluated by the server so clients never need to download and filter an
// unbounded announcement history.
type Announcement struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Title        string     `json:"title" gorm:"size:160;not null"`
	Content      string     `json:"content" gorm:"type:text;not null"`
	Severity     string     `json:"severity" gorm:"size:16;index;not null;default:info"`
	Audience     string     `json:"audience" gorm:"size:16;index;index:idx_announcements_feed,priority:2;not null;default:all"`
	Status       string     `json:"status" gorm:"size:16;index;index:idx_announcements_feed,priority:1;not null;default:draft"`
	PopupEnabled bool       `json:"popup_enabled" gorm:"index:idx_announcements_feed,priority:3;not null;default:false"`
	Dismissible  bool       `json:"dismissible" gorm:"not null;default:true"`
	StartsAt     *time.Time `json:"starts_at" gorm:"index;index:idx_announcements_feed,priority:4"`
	EndsAt       *time.Time `json:"ends_at" gorm:"index"`
	CreatedBy    uint       `json:"created_by" gorm:"index"`
	Revision     uint64     `json:"revision" gorm:"not null;default:1"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// AnnouncementRead records the latest announcement revision acknowledged by a
// user. Comparing revisions makes an edited announcement unread again without
// deleting receipt history.
type AnnouncementRead struct {
	AnnouncementID uint      `json:"announcement_id" gorm:"primaryKey;autoIncrement:false"`
	UserID         uint      `json:"user_id" gorm:"primaryKey;autoIncrement:false;index:idx_announcement_reads_user_time,priority:1"`
	Revision       uint64    `json:"revision" gorm:"not null"`
	ReadAt         time.Time `json:"read_at" gorm:"index:idx_announcement_reads_user_time,priority:2"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ProtocolDeployment struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	ProtocolEndpointID  uint       `json:"protocol_endpoint_id" gorm:"index;not null"`
	NodeID              uint       `json:"node_id" gorm:"index;not null"`
	ConfigRevision      uint64     `json:"config_revision" gorm:"not null"`
	DesiredConfigSHA256 string     `json:"desired_config_sha256" gorm:"size:64"`
	AppliedConfigSHA256 string     `json:"applied_config_sha256" gorm:"size:64"`
	Status              string     `json:"status" gorm:"size:20;index;not null"`
	RequestedBy         uint       `json:"requested_by" gorm:"index"`
	Output              string     `json:"output" gorm:"type:text"`
	Error               string     `json:"error" gorm:"type:text"`
	StartedAt           *time.Time `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type QuotaEvent struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"index;uniqueIndex:ux_quota_event_reference,priority:1;not null"`
	EventType      string    `json:"event_type" gorm:"size:24;uniqueIndex:ux_quota_event_reference,priority:2;not null"`
	DeltaBytes     int64     `json:"delta_bytes"`
	BalanceBefore  int64     `json:"balance_before"`
	BalanceAfter   int64     `json:"balance_after"`
	ReferenceType  string    `json:"reference_type" gorm:"size:32;uniqueIndex:ux_quota_event_reference,priority:3;not null"`
	ReferenceID    string    `json:"reference_id" gorm:"size:128;uniqueIndex:ux_quota_event_reference,priority:4;not null"`
	Detail         string    `json:"detail" gorm:"type:json"`
	CreatedAt      time.Time `json:"created_at"`
}
