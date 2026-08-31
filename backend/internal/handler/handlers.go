package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/version"
)

const (
	adminAttentionStatus   = "attention"
	orderStatusPending     = "pending"
	orderStatusPaid        = "paid"
	orderStatusFailed      = "failed"
	orderStatusCanceled    = "canceled"
	orderStatusSuccess     = "success"
	subStatusActive        = "active"
	subStatusExpired       = "expired"
	subStatusCanceled      = "canceled"
	userStatusActive       = "active"
	userStatusSuspended    = "suspended"
	userStatusDeactivated  = "deactivated"
	nodeReportMaxBodyBytes = 1 << 20
	nodeReportTimeWindow   = 5 * time.Minute
	nodeOnlineWindow       = 2 * time.Minute
	trafficCalcBoth        = int16(0)
	trafficCalcUpload      = int16(1)
	trafficCalcDownload    = int16(2)
	sshAuthPassword        = "password"
	sshAuthPrivateKey      = "private_key"
	sshPrivilegeNone       = "none"
	sshPrivilegeSudo       = "sudo"
	sshPrivilegeSU         = "su"
	maxEndpointSelection   = 10000
)

var perpetualSubscriptionEnd = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

var (
	errAlreadyInstalled            = errors.New("zboard is already installed")
	errNodeReportCredentialChanged = errors.New("node report credential changed")
	errNodeReportNonceReplayed     = errors.New("node report nonce replayed")
	errSubscriptionNotFound        = errors.New("no active subscription")
	errSubscriptionQuotaExhausted  = errors.New("subscription quota exhausted")
	errOrderNotPayable             = errors.New("order not payable")
	errOrderNotCancelable          = errors.New("order not cancelable")
	errOrderTransitionRejected     = errors.New("order status transition rejected")
	errProtocolEndpointUnavailable = errors.New("protocol endpoint is unavailable on this node")
	errNoBillableTraffic           = errors.New("selected traffic direction contains no billable bytes")
)

var supportedProtocols = map[string]struct{}{
	"vmess":       {},
	"vless":       {},
	"trojan":      {},
	"shadowsocks": {},
	"hysteria2":   {},
	"mieru":       {},
}

const protocolKernelMieruUnavailableReason = "Mieru 托管归属需要 Zero 0.0.15-rc.4 或更高版本；请先升级所选节点内核。"
const protocolKernelManagedUsersUnavailableReason = "Trojan 和 Hysteria2 的订阅用户模式需要 Zero 0.0.15-rc.3 或更高版本；不支持退化为共享密码，请先升级所选节点内核。"

type authClaims struct {
	UserID  uint   `json:"uid"`
	Email   string `json:"e"`
	IsAdmin bool   `json:"a"`
	Expiry  int64  `json:"exp"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type userPublic struct {
	ID      uint   `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
	Status  string `json:"status"`
}

type adminUserListItem struct {
	userPublic
	ActiveSubscriptionCount int64     `json:"active_subscription_count"`
	TotalSubscriptionCount  int64     `json:"total_subscription_count"`
	PendingOrderCount       int64     `json:"pending_order_count"`
	TotalOrderCount         int64     `json:"total_order_count"`
	CreatedAt               time.Time `json:"created_at"`
}

type adminUserSubscriptionCountRow struct {
	UserID                  uint  `gorm:"column:user_id"`
	ActiveSubscriptionCount int64 `gorm:"column:active_subscription_count"`
	TotalSubscriptionCount  int64 `gorm:"column:total_subscription_count"`
}

type adminUserOrderCountRow struct {
	UserID            uint  `gorm:"column:user_id"`
	PendingOrderCount int64 `gorm:"column:pending_order_count"`
	TotalOrderCount   int64 `gorm:"column:total_order_count"`
}

type adminSubscriptionListItem struct {
	ID                uint       `json:"id"`
	UserID            uint       `json:"user_id"`
	UserEmail         string     `json:"user_email"`
	PlanID            uint       `json:"plan_id"`
	PlanName          string     `json:"plan_name"`
	PlanSKUID         uint       `json:"plan_sku_id"`
	SKUName           string     `json:"sku_name"`
	NodeGroupID       uint       `json:"node_group_id"`
	SubscriptionType  int16      `json:"subscription_type"`
	StartAt           time.Time  `json:"start_at"`
	EndAt             time.Time  `json:"end_at"`
	Status            string     `json:"status"`
	FlowTotal         int64      `json:"flow_total"`
	FlowUsed          int64      `json:"flow_used"`
	SpeedLimitMbps    int        `json:"speed_limit_mbps"`
	DeviceLimit       int        `json:"device_limit"`
	FamilyLimit       int        `json:"family_limit"`
	RenewalPriceMinor int64      `json:"renewal_price_minor"`
	ResetPolicy       int16      `json:"reset_policy"`
	NextResetAt       *time.Time `json:"next_reset_at"`
	TrafficCalcMode   int16      `json:"traffic_calc_mode"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func applyEffectiveSubscriptionStatusFilter(query *gorm.DB, status string, now time.Time) *gorm.DB {
	switch status {
	case subStatusActive:
		return query.Where(
			"subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total",
			subStatusActive, now,
		)
	case subStatusExpired:
		return query.Where(
			"(subscriptions.status = ? OR (subscriptions.status = ? AND (subscriptions.end_at <= ? OR subscriptions.flow_used >= subscriptions.flow_total)))",
			subStatusExpired, subStatusActive, now,
		)
	default:
		return query.Where("subscriptions.status = ?", status)
	}
}

func effectiveSubscriptionStatus(subscription model.Subscription, now time.Time) string {
	if subscription.Status == subStatusActive &&
		(!subscription.EndAt.After(now) || subscription.FlowUsed >= subscription.FlowTotal) {
		return subStatusExpired
	}
	return subscription.Status
}

type nodeCreateReq struct {
	Name                    string `json:"name"`
	Region                  string `json:"region"`
	Address                 string `json:"address"`
	NodeCredential          string `json:"node_credential"`
	CommunicationProtocol   int16  `json:"communication_protocol"`
	Config                  string `json:"config"`
	IsEnabled               *bool  `json:"is_enabled"`
	Remark                  string `json:"remark"`
	SSHHost                 string `json:"ssh_host"`
	SSHPort                 int    `json:"ssh_port"`
	SSHUser                 string `json:"ssh_user"`
	SSHAuthMethod           string `json:"ssh_auth_method"`
	SSHPwd                  string `json:"ssh_password"`
	SSHPrivateKey           string `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase string `json:"ssh_private_key_passphrase"`
	SSHPrivilegeMode        string `json:"ssh_privilege_mode"`
	SSHPrivilegePassword    string `json:"ssh_privilege_password"`
}

type nodeUpdateReq struct {
	Name            *string `json:"name"`
	Region          *string `json:"region"`
	Address         *string `json:"address"`
	Remark          *string `json:"remark"`
	LifecycleStatus *string `json:"lifecycle_status"`
	IsEnabled       *bool   `json:"is_enabled"`
}

type planCreateReq struct {
	Name                   string       `json:"name"`
	Slug                   string       `json:"slug"`
	Summary                string       `json:"summary"`
	Description            string       `json:"description"`
	SortOrder              int          `json:"sort_order"`
	IsActive               bool         `json:"is_active"`
	SKUs                   []planSKUReq `json:"skus"`
	NodeGroupID            uint         `json:"node_group_id"`
	TrafficBytes           int64        `json:"traffic_bytes"`
	SpeedLimitMbps         int          `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int          `json:"max_active_subscriptions"`
	IsRenewable            *bool        `json:"is_renewable"`
	DeviceLimit            int          `json:"device_limit"`
	FamilyLimit            int          `json:"family_limit"`
	ResetPolicy            int16        `json:"reset_policy"`
	TrafficCalcMode        int16        `json:"traffic_calc_mode"`
}

type planUpdateReq struct {
	Name                   *string `json:"name"`
	Slug                   *string `json:"slug"`
	Summary                *string `json:"summary"`
	Description            *string `json:"description"`
	SortOrder              *int    `json:"sort_order"`
	IsActive               *bool   `json:"is_active"`
	TrafficBytes           *int64  `json:"traffic_bytes"`
	SpeedLimitMbps         *int    `json:"speed_limit_mbps"`
	MaxActiveSubscriptions *int    `json:"max_active_subscriptions"`
	IsRenewable            *bool   `json:"is_renewable"`
	DeviceLimit            *int    `json:"device_limit"`
	FamilyLimit            *int    `json:"family_limit"`
	ResetPolicy            *int16  `json:"reset_policy"`
	TrafficCalcMode        *int16  `json:"traffic_calc_mode"`
	NodeGroupID            *uint   `json:"node_group_id"`
	ExpectedRevision       *uint64 `json:"expected_revision"`
}

type nodeGroupCreateReq struct {
	Name                string `json:"name"`
	Code                string `json:"code"`
	Description         string `json:"description"`
	IsEnabled           *bool  `json:"is_enabled"`
	ProtocolEndpointIDs []uint `json:"protocol_endpoint_ids"`
}

type nodeGroupUpdateReq struct {
	Name                *string `json:"name"`
	Code                *string `json:"code"`
	Description         *string `json:"description"`
	IsEnabled           *bool   `json:"is_enabled"`
	ProtocolEndpointIDs *[]uint `json:"protocol_endpoint_ids"`
	ExpectedRevision    *uint64 `json:"expected_revision"`
}

type planSKUReq struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	SKUType        string `json:"sku_type"`
	BillingUnit    string `json:"billing_unit"`
	BillingValue   int    `json:"billing_value"`
	PriceCents     int64  `json:"price_cents"`
	Currency       string `json:"currency"`
	TrafficBytes   int64  `json:"traffic_bytes"`
	DeviceLimit    int    `json:"device_limit"`
	SpeedLimitMbps int    `json:"speed_limit_mbps"`
	IsActive       *bool  `json:"is_active"`
	SortOrder      int    `json:"sort_order"`
}

type orderCreateReq struct {
	PlanSKUID            uint   `json:"plan_sku_id"`
	OrderType            string `json:"order_type"`
	TargetSubscriptionID uint   `json:"target_subscription_id"`
	Channel              string `json:"channel"`
}

type orderCallbackReq struct {
	Status      string `json:"status"`
	RawCallback string `json:"raw_callback"`
}

type trafficReportReq struct {
	ReportID           string `json:"report_id"`
	UserID             uint   `json:"user_id"`
	ProtocolEndpointID uint   `json:"protocol_endpoint_id"`
	RawBytes           int64  `json:"raw_bytes"`
	UploadBytes        int64  `json:"upload_bytes"`
	DownloadBytes      int64  `json:"download_bytes"`
	Meta               string `json:"meta"`
}

type authenticatedNodeReport struct {
	node      model.Node
	timestamp time.Time
	nonce     string
}

type nodeConnectorHeartbeat struct {
	NodeID        string `json:"node_id"`
	BuildID       string `json:"build_id"`
	UptimeSeconds uint64 `json:"uptime_seconds"`
	ActiveFlows   uint64 `json:"active_flows"`
	BytesUp       uint64 `json:"bytes_up"`
	BytesDown     uint64 `json:"bytes_down"`
}

type trafficReconciliationItem struct {
	SubscriptionID uint   `json:"subscription_id"`
	UserID         uint   `json:"user_id"`
	PlanID         uint   `json:"plan_id"`
	Status         string `json:"status"`
	FlowUsed       int64  `json:"flow_used"`
	RecordedBytes  int64  `json:"recorded_bytes"`
	Difference     int64  `json:"difference"`
	Result         string `json:"result"`
}

type trafficReconciliationAggregates struct {
	SubscriptionCount   int64 `json:"subscription_count" gorm:"column:subscription_count"`
	MatchedCount        int64 `json:"matched_count" gorm:"column:matched_count"`
	MissingRecordsCount int64 `json:"missing_records_count" gorm:"column:missing_records_count"`
	OverRecordedCount   int64 `json:"over_recorded_count" gorm:"column:over_recorded_count"`
	FlowUsed            int64 `json:"flow_used" gorm:"column:flow_used"`
	RecordedBytes       int64 `json:"recorded_bytes" gorm:"column:recorded_bytes"`
	MissingBytes        int64 `json:"missing_bytes" gorm:"column:missing_bytes"`
	OverRecordedBytes   int64 `json:"over_recorded_bytes" gorm:"column:over_recorded_bytes"`
}

type trafficRecordListItem struct {
	ID                      uint      `json:"id"`
	UserID                  uint      `json:"user_id"`
	SubscriptionID          uint      `json:"subscription_id,omitempty"`
	NodeID                  uint      `json:"node_id"`
	ProtocolEndpointID      uint      `json:"protocol_endpoint_id"`
	RawBytes                int64     `json:"raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes"`
	RecordAt                time.Time `json:"record_at"`
}

type trafficRecordAggregates struct {
	RawBytes              int64 `json:"raw_bytes" gorm:"column:raw_bytes"`
	UsedBytes             int64 `json:"used_bytes" gorm:"column:used_bytes"`
	UserCount             int64 `json:"user_count" gorm:"column:user_count"`
	SubscriptionCount     int64 `json:"subscription_count" gorm:"column:subscription_count"`
	NodeCount             int64 `json:"node_count" gorm:"column:node_count"`
	ProtocolEndpointCount int64 `json:"protocol_endpoint_count" gorm:"column:protocol_endpoint_count"`
}

type nodeSSHTestReq struct {
	NodeID uint `json:"node_id"`
}

type nodeSSHConfigReq struct {
	SSHHost                 string  `json:"ssh_host"`
	SSHPort                 int     `json:"ssh_port"`
	SSHUser                 string  `json:"ssh_user"`
	SSHAuthMethod           string  `json:"ssh_auth_method"`
	SSHPwd                  string  `json:"ssh_password"`
	SSHPrivateKey           string  `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase *string `json:"ssh_private_key_passphrase"`
	SSHPrivilegeMode        string  `json:"ssh_privilege_mode"`
	SSHPrivilegePassword    *string `json:"ssh_privilege_password"`
}

type protocolEndpointWriteReq struct {
	NodeID                     uint                                        `json:"node_id"`
	Name                       string                                      `json:"name"`
	Protocol                   string                                      `json:"protocol"`
	Address                    string                                      `json:"address"`
	Port                       int                                         `json:"port"`
	PublicPort                 int                                         `json:"public_port"`
	Cipher                     int16                                       `json:"cipher"`
	ParentProtocolID           *uint                                       `json:"parent_protocol_id"`
	ManagedCertificateID       *uint                                       `json:"managed_certificate_id"`
	MultiplierMilli            int64                                       `json:"multiplier_milli"`
	IsActive                   *bool                                       `json:"is_active"`
	SortOrder                  int                                         `json:"sort_order"`
	Config                     string                                      `json:"config"`
	ClientConfig               string                                      `json:"client_config"`
	OptionalConfig             string                                      `json:"optional_config"`
	Tags                       string                                      `json:"tags"`
	NodeGroupMembershipChanges []protocolEndpointNodeGroupMembershipChange `json:"node_group_membership_changes"`
}

type protocolEndpointSelectionSnapshot struct {
	IDs        []uint    `json:"ids"`
	Total      int64     `json:"total"`
	ResolvedAt time.Time `json:"resolved_at"`
}

type subscriptionManifestNode struct {
	ID              uint            `json:"id"`
	NodeID          uint            `json:"node_id"`
	SubscriptionID  uint            `json:"subscription_id,omitempty"`
	CredentialID    string          `json:"credential_id,omitempty"`
	Name            string          `json:"name"`
	Region          string          `json:"region"`
	Address         string          `json:"address"`
	Port            int             `json:"port"`
	PublicPort      int             `json:"public_port"`
	Protocol        string          `json:"protocol"`
	MultiplierMilli int64           `json:"multiplier_milli"`
	Config          json.RawMessage `json:"config"`
}

type adminUserCreateReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
	Status   string `json:"status"`
}

type adminUserUpdateReq struct {
	Status   *string `json:"status"`
	IsAdmin  *bool   `json:"is_admin"`
	Password *string `json:"password"`
}

type setupRequest struct {
	SiteName          string `json:"site_name"`
	SiteURL           string `json:"site_url"`
	AllowRegistration bool   `json:"allow_registration"`
	AdminEmail        string `json:"admin_email"`
	AdminPassword     string `json:"admin_password"`
}

type siteSettingsRequest struct {
	SiteName          string `json:"site_name"`
	SiteURL           string `json:"site_url"`
	AllowRegistration bool   `json:"allow_registration"`
}

type handlers struct {
	db                       *gorm.DB
	jwtSecret                string
	credentialCipher         *security.CredentialCipher
	zeroArtifactDir          string
	zeroNativeAccess         bool
	zeroMieruAccess          bool
	zeroLocalVersion         string
	sshTerminal              *sshTerminalRuntime
	nodePublishLocks         sync.Map
	nodePublishScheduler     *nodePublishScheduler
	nodePublishSchedulerOnce sync.Once
	zeroEventAuthCache       sync.Map
	zeroEventAuthFailures    sync.Map
	expiryReconcileMu        sync.Mutex
	lastExpiryReconcile      time.Time
}

func NewHandlers(db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion string) (*handlers, error) {
	if err := cfgpkg.ValidateJWTSecret(jwtSecret); err != nil {
		return nil, err
	}
	if credentialCipher == nil {
		return nil, errors.New("credential cipher is required")
	}
	normalizedKernelContract := strings.ToLower(strings.TrimSpace(zeroKernelContract))
	localVersion := strings.TrimSpace(zeroLocalVersion)
	nativeContract := normalizedKernelContract == cfgpkg.ZeroKernelNativeLocal || normalizedKernelContract == cfgpkg.ZeroKernelNativeMieru
	return &handlers{
		db:               db,
		jwtSecret:        jwtSecret,
		credentialCipher: credentialCipher,
		zeroArtifactDir:  strings.TrimSpace(zeroArtifactDir),
		zeroNativeAccess: nativeContract,
		zeroMieruAccess: normalizedKernelContract == cfgpkg.ZeroKernelNativeMieru ||
			(nativeContract && zeroSupportsMieruPrincipal(localVersion)),
		zeroLocalVersion:     localVersion,
		sshTerminal:          newSSHTerminalRuntime(),
		nodePublishScheduler: newNodePublishScheduler(),
	}, nil
}

func (h *handlers) HealthHandler(w http.ResponseWriter, r *http.Request) {
	OK(w, map[string]interface{}{
		"service": "zboard",
		"ready":   true,
	})
}

func (h *handlers) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	sqlDB, err := h.db.DB()
	if err != nil {
		ServiceUnavailable(w, "database not ready")
		return
	}
	if err := sqlDB.PingContext(r.Context()); err != nil {
		ServiceUnavailable(w, "database not ready")
		return
	}

	OK(w, map[string]interface{}{
		"service": "zboard",
		"ready":   true,
		"db":      true,
	})
}

func (h *handlers) VersionHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"version":               version.FullVersion(),
		"name":                  "zboard",
		"zero_kernel_contract":  h.zeroKernelContract(),
		"protocol_capabilities": h.protocolKernelCapabilities(),
	}
	if h.zeroNativeAccess {
		data["zero_local_version"] = h.zeroLocalVersion
	}
	OK(w, data)
}

func (h *handlers) zeroKernelContract() string {
	if h.zeroMieruAccess {
		return cfgpkg.ZeroKernelNativeMieru
	}
	if h.zeroNativeAccess {
		return cfgpkg.ZeroKernelNativeLocal
	}
	return cfgpkg.ZeroKernelLegacy
}

func (h *handlers) SetupStatusHandler(w http.ResponseWriter, r *http.Request) {
	var installation model.Installation
	err := h.db.First(&installation, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		OK(w, map[string]interface{}{
			"installed": false,
			"version":   version.FullVersion(),
		})
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"installed":          true,
		"site_name":          installation.SiteName,
		"site_url":           installation.SiteURL,
		"allow_registration": installation.AllowRegistration,
		"version":            version.FullVersion(),
	})
}

func (h *handlers) SetupInstallHandler(w http.ResponseWriter, r *http.Request) {
	var body setupRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := validateSetupRequest(&body); err != nil {
		BadRequestError(w, err)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}
	admin := model.User{
		AccountName: body.AdminEmail,
		Email:       body.AdminEmail, Password: string(hash),
		IsAdmin: true, Status: userStatusActive,
	}
	installation := model.Installation{
		ID:                1,
		SiteName:          body.SiteName,
		SiteURL:           body.SiteURL,
		AllowRegistration: body.AllowRegistration,
		InstalledAt:       time.Now().UTC(),
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&model.User{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount > 0 {
			return errAlreadyInstalled
		}
		// The fixed primary key is the cross-process installation lock. Only one
		// concurrent installer can commit this row.
		if err := tx.Create(&installation).Error; err != nil {
			if isDuplicateError(err) {
				return errAlreadyInstalled
			}
			return err
		}
		if err := upsertSiteConfigs(tx, installation.SiteName, installation.SiteURL, installation.AllowRegistration); err != nil {
			return err
		}
		return tx.Create(&admin).Error
	})
	if errors.Is(err, errAlreadyInstalled) || isDuplicateError(err) {
		writeJSON(w, http.StatusConflict, "zboard is already installed", nil)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}

	token, expiresAt, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"installed": true,
		"site_name": installation.SiteName,
		"user":      toPublicUser(admin),
		"auth":      tokenResponse{Token: token, ExpiresAt: expiresAt},
	})
}

func (h *handlers) AdminSettingsUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body siteSettingsRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := normalizeAndValidateSiteSettings(&body.SiteName, &body.SiteURL); err != nil {
		BadRequestError(w, err)
		return
	}

	var installation model.Installation
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&installation, 1).Error; err != nil {
			return err
		}
		installation.SiteName = body.SiteName
		installation.SiteURL = body.SiteURL
		installation.AllowRegistration = body.AllowRegistration
		if err := tx.Save(&installation).Error; err != nil {
			return err
		}
		if err := upsertSiteConfigs(tx, installation.SiteName, installation.SiteURL, installation.AllowRegistration); err != nil {
			return err
		}
		return createAuditLog(tx, claims, "system.settings.update", "installation:1", fmt.Sprintf("allow_registration=%t", body.AllowRegistration))
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, installation)
}

func (h *handlers) InstallationMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/v1/version" ||
			r.URL.Path == "/api/v1/setup/status" ||
			r.URL.Path == "/api/v1/setup/install" {
			next(w, r)
			return
		}
		var count int64
		if err := h.db.Model(&model.Installation{}).Where("id = ?", 1).Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		if count == 0 {
			writeJSON(w, http.StatusPreconditionRequired, "zboard installation is required", map[string]string{"setup_url": "/setup"})
			return
		}
		next(w, r)
	}
}

func validateSetupRequest(body *setupRequest) error {
	body.SiteName = strings.TrimSpace(body.SiteName)
	body.SiteURL = strings.TrimRight(strings.TrimSpace(body.SiteURL), "/")
	body.AdminEmail = normalizeEmail(body.AdminEmail)
	fields := siteSettingsValidationFields(body.SiteName, body.SiteURL)
	if !validEmail(body.AdminEmail) {
		fields["admin_email"] = "请输入有效的管理员邮箱。"
	}
	if len(body.AdminPassword) < 12 || len(body.AdminPassword) > 72 {
		fields["admin_password"] = "管理员密码必须为 12–72 个 UTF-8 字节。"
	}
	if len(fields) > 0 {
		return validationError("安装信息校验失败。", fields)
	}
	return nil
}

func normalizeAndValidateSiteSettings(siteName, siteURL *string) error {
	*siteName = strings.TrimSpace(*siteName)
	*siteURL = strings.TrimRight(strings.TrimSpace(*siteURL), "/")
	fields := siteSettingsValidationFields(*siteName, *siteURL)
	if len(fields) > 0 {
		return validationError("站点设置校验失败。", fields)
	}
	return nil
}

func siteSettingsValidationFields(siteName, siteURL string) map[string]string {
	fields := map[string]string{}
	if siteName == "" || len(siteName) > 80 {
		fields["site_name"] = "站点名称必须为 1–80 个 UTF-8 字节。"
	}
	parsedURL, err := url.ParseRequestURI(siteURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		fields["site_url"] = "请输入完整的 HTTP 或 HTTPS 地址。"
		return fields
	}
	if parsedURL.User != nil || parsedURL.Fragment != "" {
		fields["site_url"] = "公开访问地址不能包含账号、密码或 URL 片段。"
	}
	return fields
}

func upsertSiteConfigs(tx *gorm.DB, siteName, siteURL string, allowRegistration bool) error {
	values := map[string]string{
		"site_name":       siteName,
		"site_url":        siteURL,
		"register_switch": strconv.FormatBool(allowRegistration),
	}
	for key, value := range values {
		if err := tx.Model(&model.SystemConfig{}).
			Where("config_key = ?", key).
			Updates(map[string]interface{}{"value": value, "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
	}
	return nil
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint")
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validEmail(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func validPassword(value string) bool {
	return len(value) >= 12 && len(value) <= 72
}

func (h *handlers) SystemInfoHandler(w http.ResponseWriter, r *http.Request) {
	sqlDB, err := h.db.DB()
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := sqlDB.PingContext(r.Context()); err != nil {
		ServiceUnavailable(w, "database unavailable")
		return
	}

	OK(w, map[string]interface{}{
		"service":     "zboard",
		"version":     version.FullVersion(),
		"name":        "zboard",
		"api_version": "v1",
		"deployment": map[string]bool{
			"docker":     true,
			"kubernetes": false,
		},
		"docs": map[string]string{
			"kernel": "https://docs.zerodenet.org",
		},
	})
}

func (h *handlers) RegisterAuthRoutes(w http.ResponseWriter, r *http.Request) {
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		ServerError(w, err)
		return
	}
	if !installation.AllowRegistration {
		Forbidden(w, "public registration is disabled")
		return
	}
	type req struct {
		Email            string `json:"email"`
		Password         string `json:"password"`
		VerificationCode string `json:"verification_code"`
	}

	var body req
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	body.Email = normalizeEmail(body.Email)
	verificationEnabled, err := h.registrationEmailVerificationEnabled(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	registrationFields := map[string]string{}
	if !validEmail(body.Email) {
		registrationFields["email"] = "请输入有效邮箱。"
	}
	if !validPassword(body.Password) {
		registrationFields["password"] = "密码必须为 12–72 个 UTF-8 字节。"
	}
	if verificationEnabled && !registrationCodePattern.MatchString(strings.TrimSpace(body.VerificationCode)) {
		registrationFields["verification_code"] = "请输入 6 位邮箱验证码。"
	}
	if len(registrationFields) > 0 {
		BadRequestFields(w, "注册信息校验失败。", registrationFields)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}

	user := model.User{
		AccountName: body.Email,
		Email:       body.Email, Password: string(hash),
		IsAdmin: false, Status: userStatusActive,
	}

	if verificationEnabled {
		err = h.createVerifiedRegistrationUser(&user, strings.TrimSpace(body.VerificationCode))
	} else {
		err = h.db.Create(&user).Error
	}
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, validation)
			return
		}
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") {
			BadRequestFields(w, "注册信息校验失败。", map[string]string{"email": "该邮箱已存在。"})
			return
		}
		ServerError(w, err)
		return
	}

	auth := authClaims{
		UserID:  user.ID,
		Email:   user.Email,
		IsAdmin: user.IsAdmin,
		Expiry:  0,
	}
	token, expiresAt, err := h.issueToken(auth)
	if err != nil {
		ServerError(w, err)
		return
	}
	// Registration succeeds independently from SMTP. When the operator has
	// enabled both delivery and the registration template, enqueue a durable
	// task so delivery failures remain visible and retryable in Operations.
	_ = h.enqueueRegistrationWelcome(user)

	OK(w, map[string]interface{}{
		"user": toPublicUser(user),
		"auth": tokenResponse{Token: token, ExpiresAt: expiresAt},
	})
}

func (h *handlers) LoginHandler(w http.ResponseWriter, r *http.Request) {
	type req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	var body req
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	email := normalizeEmail(body.Email)
	loginFields := map[string]string{}
	if email == "" {
		loginFields["email"] = "请输入邮箱地址。"
	}
	if body.Password == "" {
		loginFields["password"] = "请输入密码。"
	}
	if len(loginFields) > 0 {
		BadRequestFields(w, "登录信息不完整。", loginFields)
		return
	}

	var user model.User
	query := h.db.Model(&model.User{}).Where("email = ? AND status = ?", email, userStatusActive)
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Unauthorized(w, "invalid email or password")
			return
		}
		ServerError(w, err)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)); err != nil {
		Unauthorized(w, "invalid email or password")
		return
	}
	lastLoginAt := time.Now().UTC()
	if err := h.db.Model(&user).Update("last_login_at", lastLoginAt).Error; err != nil {
		ServerError(w, err)
		return
	}
	user.LastLoginAt = &lastLoginAt

	token, expiresAt, err := h.issueToken(authClaims{
		UserID:  user.ID,
		Email:   user.Email,
		IsAdmin: user.IsAdmin,
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"user": toPublicUser(user),
		"auth": tokenResponse{Token: token, ExpiresAt: expiresAt},
	})
}

func (h *handlers) MeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var user model.User
	if err := h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Unauthorized(w, "user not found")
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, toPublicUser(user))
}

func (h *handlers) AdminUsersListHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	users := make([]model.User, 0)
	query := h.db.Model(&model.User{})

	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !h.isValidUserStatus(status) {
			BadRequest(w, "invalid user status")
			return
		}
		query = query.Where("status = ?", status)
	}

	if isAdmin := strings.TrimSpace(r.URL.Query().Get("is_admin")); isAdmin != "" {
		flag, parseErr := strconv.ParseBool(isAdmin)
		if parseErr != nil {
			BadRequest(w, "invalid is_admin")
			return
		}
		query = query.Where("is_admin = ?", flag)
	}

	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		if len(q) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		like := fmt.Sprintf("%%%s%%", strings.ToLower(q))
		query = query.Where("LOWER(email) LIKE ? OR LOWER(account_name) LIKE ?", like, like)
	}

	paged := wantsPagedList(r)
	offset, limit := 0, 50
	var total int64
	order := "id desc"
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		sortColumn := map[string]string{
			"id":         "id",
			"email":      "email",
			"created_at": "created_at",
		}[strings.TrimSpace(r.URL.Query().Get("sort"))]
		if sortColumn == "" {
			sortColumn = "created_at"
		}
		direction := "desc"
		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "asc") {
			direction = "asc"
		}
		order = sortColumn + " " + direction
		if sortColumn != "id" {
			order += ", id " + direction
		}
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Order(order).Find(&users).Error; err != nil {
		ServerError(w, err)
		return
	}

	if paged {
		items, loadErr := loadAdminUserListItems(h.db, users, time.Now().UTC())
		if loadErr != nil {
			ServerError(w, loadErr)
			return
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	publicUsers := make([]userPublic, 0, len(users))
	for _, user := range users {
		publicUsers = append(publicUsers, toPublicUser(user))
	}
	OK(w, publicUsers)
}

func loadAdminUserListItems(db *gorm.DB, users []model.User, now time.Time) ([]adminUserListItem, error) {
	items := make([]adminUserListItem, 0, len(users))
	if len(users) == 0 {
		return items, nil
	}
	userIDs := make([]uint, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}

	subscriptionRows := make([]adminUserSubscriptionCountRow, 0, len(users))
	if err := db.Model(&model.Subscription{}).
		Select(`user_id,
			COUNT(*) AS total_subscription_count,
			COALESCE(SUM(CASE WHEN status = ? AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END), 0) AS active_subscription_count`,
			subStatusActive, now).
		Where("user_id IN ?", userIDs).
		Group("user_id").
		Scan(&subscriptionRows).Error; err != nil {
		return nil, err
	}
	subscriptionCounts := make(map[uint]adminUserSubscriptionCountRow, len(subscriptionRows))
	for _, row := range subscriptionRows {
		subscriptionCounts[row.UserID] = row
	}

	orderRows := make([]adminUserOrderCountRow, 0, len(users))
	if err := db.Model(&model.Order{}).
		Select(`user_id,
			COUNT(*) AS total_order_count,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_order_count`,
			orderStatusPending).
		Where("user_id IN ?", userIDs).
		Group("user_id").
		Scan(&orderRows).Error; err != nil {
		return nil, err
	}
	orderCounts := make(map[uint]adminUserOrderCountRow, len(orderRows))
	for _, row := range orderRows {
		orderCounts[row.UserID] = row
	}

	for _, user := range users {
		subscriptions := subscriptionCounts[user.ID]
		orders := orderCounts[user.ID]
		items = append(items, adminUserListItem{
			userPublic:              toPublicUser(user),
			ActiveSubscriptionCount: subscriptions.ActiveSubscriptionCount,
			TotalSubscriptionCount:  subscriptions.TotalSubscriptionCount,
			PendingOrderCount:       orders.PendingOrderCount,
			TotalOrderCount:         orders.TotalOrderCount,
			CreatedAt:               user.CreatedAt,
		})
	}
	return items, nil
}

func (h *handlers) AdminUserCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req adminUserCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Email = normalizeEmail(req.Email)
	userFields := map[string]string{}
	if !validEmail(req.Email) {
		userFields["email"] = "请输入有效邮箱。"
	}
	if !validPassword(req.Password) {
		userFields["password"] = "密码必须为 12–72 个 UTF-8 字节。"
	}
	if len(userFields) > 0 {
		BadRequestFields(w, "用户信息校验失败。", userFields)
		return
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = userStatusActive
	}
	if !h.isValidUserStatus(status) {
		BadRequestFields(w, "用户信息校验失败。", map[string]string{"status": "账户状态无效。"})
		return
	}

	hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if hashErr != nil {
		ServerError(w, hashErr)
		return
	}
	user := model.User{
		AccountName: req.Email, Email: req.Email, Password: string(hash),
		IsAdmin: req.IsAdmin, Status: status,
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "user.create", fmt.Sprintf("user:%d", user.ID), fmt.Sprintf("status=%s admin=%t", user.Status, user.IsAdmin))
	}); err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") {
			BadRequestFields(w, "用户信息校验失败。", map[string]string{"email": "该邮箱已存在。"})
			return
		}
		ServerError(w, err)
		return
	}

	OK(w, toPublicUser(user))
}

func (h *handlers) AdminUserUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	id, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var req adminUserUpdateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}

	updates := make(map[string]interface{})
	changedFields := make([]string, 0, 3)
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !h.isValidUserStatus(status) {
			BadRequestFields(w, "账户信息校验失败。", map[string]string{"status": "账户状态无效。"})
			return
		}
		updates["status"] = status
		changedFields = append(changedFields, "status")
	}
	if req.IsAdmin != nil {
		updates["is_admin"] = *req.IsAdmin
		changedFields = append(changedFields, "is_admin")
	}
	if req.Password != nil {
		newPassword := *req.Password
		if !validPassword(newPassword) {
			BadRequestFields(w, "账户信息校验失败。", map[string]string{"password": "密码必须为 12–72 个 UTF-8 字节。"})
			return
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			ServerError(w, hashErr)
			return
		}
		updates["password"] = string(hash)
		changedFields = append(changedFields, "password")
	}

	if len(updates) == 0 {
		BadRequest(w, "no valid update fields")
		return
	}

	var target model.User
	if err := h.db.First(&target, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	nextStatus := target.Status
	nextIsAdmin := target.IsAdmin
	if req.Status != nil {
		nextStatus = strings.TrimSpace(*req.Status)
	}
	if req.IsAdmin != nil {
		nextIsAdmin = *req.IsAdmin
	}

	if err := h.isAdminActionAllowed(target, claims.UserID, nextStatus, nextIsAdmin); err != nil {
		Forbidden(w, err.Error())
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&target).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "user.update", fmt.Sprintf("user:%d", target.ID), "fields="+strings.Join(changedFields, ","))
	}); err != nil {
		ServerError(w, err)
		return
	}

	if err := h.db.First(&target, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, toPublicUser(target))
}

func (h *handlers) ensureHasAnotherActiveAdmin(targetID uint) error {
	var activeAdminCount int64
	if err := h.db.Model(&model.User{}).
		Where("id != ? AND is_admin = 1 AND status = ?", targetID, userStatusActive).
		Count(&activeAdminCount).Error; err != nil {
		return err
	}
	if activeAdminCount == 0 {
		return errors.New("cannot modify the last active admin")
	}
	return nil
}

func (h *handlers) isValidUserStatus(status string) bool {
	switch status {
	case userStatusActive, userStatusSuspended, userStatusDeactivated:
		return true
	default:
		return false
	}
}

func (h *handlers) isValidOrderStatus(status string) bool {
	switch status {
	case orderStatusPending, orderStatusPaid, orderStatusFailed, orderStatusCanceled, orderStatusSuccess:
		return true
	default:
		return false
	}
}

func (h *handlers) orderListStatusValues(status string, adminScope bool) ([]string, bool) {
	if adminScope && status == adminAttentionStatus {
		return []string{orderStatusPending, orderStatusFailed}, true
	}
	if !h.isValidOrderStatus(status) {
		return nil, false
	}
	return []string{status}, true
}

func isValidOrderType(orderType string) bool {
	switch orderType {
	case "new", "renewal", "upgrade", "traffic_pack":
		return true
	default:
		return false
	}
}

func isValidSubscriptionStatus(status string) bool {
	switch status {
	case subStatusActive, subStatusExpired, subStatusCanceled:
		return true
	default:
		return false
	}
}

func isValidSubscriptionQuotaFilter(quota string) bool {
	return quota == "available" || quota == "exhausted"
}

func orderTransitionAllowed(current, target string, force bool) bool {
	if current == target {
		return true
	}
	switch target {
	case orderStatusPaid:
		return current == orderStatusPending || current == orderStatusFailed || (force && current == orderStatusCanceled)
	case orderStatusFailed:
		return current == orderStatusPending
	case orderStatusCanceled:
		return current == orderStatusPending || (force && current == orderStatusFailed)
	default:
		return false
	}
}

func (h *handlers) isAdminActionAllowed(target model.User, actorID uint, nextStatus string, nextIsAdmin bool) error {
	if actorID != target.ID {
		return nil
	}

	if target.IsAdmin {
		if !nextIsAdmin || (nextStatus != "" && nextStatus != userStatusActive) {
			if err := h.ensureHasAnotherActiveAdmin(target.ID); err != nil {
				return err
			}
		}
	} else if nextIsAdmin {
		return nil
	}
	return nil
}

func (h *handlers) NodeListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	if r.URL.Query().Get("paged") == "true" {
		h.nodePage(w, r)
		return
	}

	nodes := make([]model.Node, 0)
	if err := h.db.Preload("KernelState").Order("id desc").Find(&nodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	cutoff := time.Now().UTC().Add(-nodeOnlineWindow)
	for index := range nodes {
		nodes[index].IsOnline = nodes[index].LastSeenAt != nil && nodes[index].LastSeenAt.After(cutoff)
		nodes[index].ConnectorOnline = nodes[index].ConnectorLastSeenAt != nil && nodes[index].ConnectorLastSeenAt.After(cutoff)
		nodes[index].SSHPrivilegeConfigured = nodes[index].SSHPrivilegePassword != ""
	}
	OK(w, nodes)
}

type nodeListItem struct {
	ID                   uint                   `json:"id"`
	Name                 string                 `json:"name"`
	Region               string                 `json:"region"`
	Address              string                 `json:"address"`
	Status               int16                  `json:"status"`
	LifecycleStatus      string                 `json:"lifecycle_status"`
	IsEnabled            bool                   `json:"is_enabled"`
	ConnectorLastSeenAt  *time.Time             `json:"connector_last_seen_at,omitempty"`
	ConnectorOnline      bool                   `json:"connector_online"`
	SSHConfigured        bool                   `json:"ssh_configured"`
	SSHVerifiedAt        *time.Time             `json:"ssh_verified_at,omitempty"`
	KernelState          *model.NodeKernelState `json:"kernel_state,omitempty"`
	EnabledProtocolCount int64                  `json:"enabled_protocol_count"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

type nodeDetailItem struct {
	nodeListItem
	Remark                         string     `json:"remark"`
	LastSeenAt                     *time.Time `json:"last_seen_at,omitempty"`
	LastSyncAt                     *time.Time `json:"last_sync_at,omitempty"`
	Version                        string     `json:"version"`
	SSHHost                        string     `json:"ssh_host"`
	SSHPort                        int        `json:"ssh_port"`
	SSHUser                        string     `json:"ssh_user"`
	SSHAuthMethod                  string     `json:"ssh_auth_method"`
	SSHPrivilegeMode               string     `json:"ssh_privilege_mode"`
	SSHPrivilegePasswordConfigured bool       `json:"ssh_privilege_password_configured"`
	SSHHostKeyFingerprint          string     `json:"ssh_host_key_fingerprint"`
	NodeCredentialPrefix           string     `json:"node_credential_prefix,omitempty"`
	NodeCredentialRevokedAt        *time.Time `json:"node_credential_revoked_at,omitempty"`
	TrafficSecretPrefix            string     `json:"traffic_secret_prefix,omitempty"`
	TrafficSecretRevokedAt         *time.Time `json:"traffic_secret_revoked_at,omitempty"`
	UptimeSeconds                  uint64     `json:"uptime_seconds"`
	ActiveFlows                    uint64     `json:"active_flows"`
	BytesUp                        uint64     `json:"bytes_up"`
	BytesDown                      uint64     `json:"bytes_down"`
}

func newNodeListItem(node model.Node, enabledProtocolCount int64, cutoff time.Time) nodeListItem {
	return nodeListItem{
		ID: node.ID, Name: node.Name, Region: node.Region, Address: node.Address, Status: node.Status,
		LifecycleStatus: node.LifecycleStatus, IsEnabled: node.IsEnabled,
		ConnectorLastSeenAt:  node.ConnectorLastSeenAt,
		ConnectorOnline:      node.ConnectorLastSeenAt != nil && node.ConnectorLastSeenAt.After(cutoff),
		SSHConfigured:        node.SSHHost != "" && node.SSHUser != "",
		SSHVerifiedAt:        node.SSHVerifiedAt,
		KernelState:          node.KernelState,
		EnabledProtocolCount: enabledProtocolCount,
		CreatedAt:            node.CreatedAt, UpdatedAt: node.UpdatedAt,
	}
}

func newNodeDetailItem(node model.Node, enabledProtocolCount int64, cutoff time.Time) nodeDetailItem {
	return nodeDetailItem{
		nodeListItem: newNodeListItem(node, enabledProtocolCount, cutoff),
		Remark:       node.Remark, LastSeenAt: node.LastSeenAt, LastSyncAt: node.LastSyncAt, Version: node.Version,
		SSHHost: node.SSHHost, SSHPort: node.SSHPort, SSHUser: node.SSHUser, SSHAuthMethod: node.SSHAuthMethod,
		SSHPrivilegeMode: node.SSHPrivilegeMode, SSHPrivilegePasswordConfigured: node.SSHPrivilegePassword != "",
		SSHHostKeyFingerprint: node.SSHHostKeyFingerprint,
		NodeCredentialPrefix:  node.NodeCredentialPrefix, NodeCredentialRevokedAt: node.NodeCredentialRevokedAt,
		TrafficSecretPrefix: node.TrafficSecretPrefix, TrafficSecretRevokedAt: node.TrafficSecretRevokedAt,
		UptimeSeconds: node.UptimeSeconds, ActiveFlows: node.ActiveFlows, BytesUp: node.BytesUp, BytesDown: node.BytesDown,
	}
}

func (h *handlers) nodePage(w http.ResponseWriter, r *http.Request) {
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.Node{})
	if rawID := strings.TrimSpace(r.URL.Query().Get("node_id")); rawID != "" {
		nodeID, parseErr := strconv.ParseUint(rawID, 10, 64)
		if parseErr != nil || nodeID == 0 {
			BadRequest(w, "invalid node_id")
			return
		}
		query = query.Where("nodes.id = ?", nodeID)
	}
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("LOWER(nodes.name) LIKE ? OR LOWER(nodes.address) LIKE ? OR LOWER(nodes.region) LIKE ?", pattern, pattern, pattern)
	}
	if region := strings.TrimSpace(r.URL.Query().Get("region")); region != "" {
		query = query.Where("nodes.region = ?", region)
	}
	if lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle_status")); lifecycle != "" {
		switch lifecycle {
		case "active", "maintenance", "retired":
			query = query.Where("nodes.lifecycle_status = ?", lifecycle)
		default:
			BadRequest(w, "invalid lifecycle_status")
			return
		}
	}
	if rawEnabled := strings.TrimSpace(r.URL.Query().Get("enabled")); rawEnabled != "" {
		enabled, parseErr := strconv.ParseBool(rawEnabled)
		if parseErr != nil {
			BadRequest(w, "invalid enabled")
			return
		}
		query = query.Where("nodes.is_enabled = ?", enabled)
	}
	cutoff := time.Now().UTC().Add(-nodeOnlineWindow)
	if online := strings.TrimSpace(r.URL.Query().Get("connector_online")); online != "" {
		switch online {
		case "true":
			query = query.Where("nodes.connector_last_seen_at >= ?", cutoff)
		case "false":
			query = query.Where("nodes.connector_last_seen_at IS NULL OR nodes.connector_last_seen_at < ?", cutoff)
		default:
			BadRequest(w, "invalid connector_online")
			return
		}
	}
	if kernelStatus := strings.TrimSpace(r.URL.Query().Get("kernel_status")); kernelStatus != "" {
		query = query.Joins("JOIN node_kernel_states ON node_kernel_states.node_id = nodes.id").Where("node_kernel_states.status = ?", kernelStatus)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	sortColumn := map[string]string{
		"id": "nodes.id", "name": "nodes.name", "region": "nodes.region",
		"updated_at": "nodes.updated_at", "last_seen_at": "nodes.connector_last_seen_at",
	}[strings.TrimSpace(r.URL.Query().Get("sort"))]
	if sortColumn == "" {
		sortColumn = "nodes.id"
	}
	direction := "desc"
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "asc") {
		direction = "asc"
	}
	nodes := make([]model.Node, 0, limit)
	if err := query.Order(sortColumn + " " + direction).Offset(offset).Limit(limit).Find(&nodes).Error; err != nil {
		ServerError(w, err)
		return
	}

	items := make([]nodeListItem, 0, len(nodes))
	if len(nodes) == 0 {
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	ids := make([]uint, 0, len(nodes))
	for index := range nodes {
		ids = append(ids, nodes[index].ID)
		nodes[index].IsOnline = nodes[index].LastSeenAt != nil && nodes[index].LastSeenAt.After(cutoff)
		nodes[index].ConnectorOnline = nodes[index].ConnectorLastSeenAt != nil && nodes[index].ConnectorLastSeenAt.After(cutoff)
		nodes[index].SSHPrivilegeConfigured = nodes[index].SSHPrivilegePassword != ""
	}
	states := make([]model.NodeKernelState, 0, len(nodes))
	if err := h.db.Where("node_id IN ?", ids).Find(&states).Error; err != nil {
		ServerError(w, err)
		return
	}
	stateByNode := make(map[uint]*model.NodeKernelState, len(states))
	for index := range states {
		stateByNode[states[index].NodeID] = &states[index]
	}
	type nodeProtocolCount struct {
		NodeID uint
		Count  int64
	}
	counts := make([]nodeProtocolCount, 0, len(nodes))
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Select("node_id, COUNT(*) AS count").
		Where("node_id IN ? AND is_active = ?", ids, true).
		Group("node_id").Scan(&counts).Error; err != nil {
		ServerError(w, err)
		return
	}
	countByNode := make(map[uint]int64, len(counts))
	for _, count := range counts {
		countByNode[count.NodeID] = count.Count
	}
	for index := range nodes {
		nodes[index].KernelState = stateByNode[nodes[index].ID]
		items = append(items, newNodeListItem(nodes[index], countByNode[nodes[index].ID], cutoff))
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) NodeDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.Preload("KernelState").First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	var enabledProtocolCount int64
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND is_active = ?", node.ID, true).Count(&enabledProtocolCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, newNodeDetailItem(node, enabledProtocolCount, time.Now().UTC().Add(-nodeOnlineWindow)))
}

func (h *handlers) NodeCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req nodeCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	req.Region = strings.TrimSpace(req.Region)
	if req.Name == "" {
		BadRequestFields(w, "节点信息校验失败。", map[string]string{"name": "请输入主机名称。"})
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.CommunicationProtocol == 0 {
		req.CommunicationProtocol = 1
	}
	if err := validateOptionalJSONObject("config", req.Config); err != nil {
		BadRequestError(w, err)
		return
	}
	req.SSHHost = strings.TrimSpace(req.SSHHost)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.SSHAuthMethod = normalizeSSHAuthMethod(req.SSHAuthMethod)
	req.SSHPrivilegeMode = normalizeSSHPrivilegeMode(req.SSHPrivilegeMode)
	credential := req.SSHPwd
	if req.SSHAuthMethod == sshAuthPrivateKey {
		credential = req.SSHPrivateKey
	}
	sshConfigured := req.SSHHost != "" || req.SSHUser != "" || credential != ""
	if sshConfigured {
		if err := validateSSHFields(req.SSHHost, req.SSHPort, req.SSHUser, req.SSHAuthMethod, credential, ""); err != nil {
			BadRequestError(w, err)
			return
		}
		if req.SSHAuthMethod == sshAuthPrivateKey {
			if _, err := parseSSHPrivateKey(credential, req.SSHPrivateKeyPassphrase); err != nil {
				BadRequestError(w, err)
				return
			}
		}
	}
	encryptedCredential, err := h.credentialCipher.Encrypt(credential)
	if err != nil {
		ServerError(w, err)
		return
	}
	encryptedPassphrase, err := h.credentialCipher.Encrypt(req.SSHPrivateKeyPassphrase)
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := validateSSHPrivilege(req.SSHPrivilegeMode, req.SSHPrivilegePassword); err != nil {
		BadRequestError(w, err)
		return
	}
	encryptedPrivilegePassword, err := h.credentialCipher.Encrypt(req.SSHPrivilegePassword)
	if err != nil {
		ServerError(w, err)
		return
	}
	req.NodeCredential = strings.TrimSpace(req.NodeCredential)
	encryptedNodeCredential, err := h.credentialCipher.Encrypt(req.NodeCredential)
	if err != nil {
		ServerError(w, err)
		return
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	nodeCredentialPrefix := ""
	if req.NodeCredential != "" {
		if len(req.NodeCredential) < 12 {
			BadRequestFields(w, "节点信息校验失败。", map[string]string{"node_credential": "节点连接凭证至少需要 12 个字符。"})
			return
		}
		nodeCredentialPrefix = req.NodeCredential[:12]
	}
	node := model.Node{
		Name:                    req.Name,
		Region:                  req.Region,
		Address:                 req.Address,
		NodeCredential:          encryptedNodeCredential,
		NodeCredentialPrefix:    nodeCredentialPrefix,
		CommunicationProtocol:   req.CommunicationProtocol,
		Status:                  0,
		LifecycleStatus:         "active",
		Config:                  normalizeOptionalJSON(req.Config, "{}"),
		IsEnabled:               isEnabled,
		Remark:                  strings.TrimSpace(req.Remark),
		IsOnline:                false,
		SSHHost:                 req.SSHHost,
		SSHPort:                 req.SSHPort,
		SSHUser:                 req.SSHUser,
		SSHAuthMethod:           req.SSHAuthMethod,
		SSHPwd:                  encryptedCredential,
		SSHPrivateKeyPassphrase: encryptedPassphrase,
		SSHPrivilegeMode:        req.SSHPrivilegeMode,
		SSHPrivilegePassword:    encryptedPrivilegePassword,
		SSHPrivilegeConfigured:  encryptedPrivilegePassword != "",
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&node).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.NodeKernelState{NodeID: node.ID, Status: "unknown", Phase: "idle", RecommendedAction: "detect"}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.create", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("region=%s", node.Region))
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, node)
}

func (h *handlers) NodeUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req nodeUpdateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		NotFound(w)
		return
	}
	updates := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			BadRequestFields(w, "节点信息校验失败。", map[string]string{"name": "请输入主机名称。"})
			return
		}
		updates["name"] = name
	}
	if req.Region != nil {
		updates["region"] = strings.TrimSpace(*req.Region)
	}
	if req.Address != nil {
		updates["address"] = strings.TrimSpace(*req.Address)
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	if req.LifecycleStatus != nil {
		status := strings.ToLower(strings.TrimSpace(*req.LifecycleStatus))
		if status != "active" && status != "maintenance" && status != "retired" {
			BadRequestFields(w, "节点信息校验失败。", map[string]string{"lifecycle_status": "请选择有效的生命周期。"})
			return
		}
		updates["lifecycle_status"] = status
		if status != "active" {
			updates["is_enabled"] = false
		}
	}
	if req.IsEnabled != nil {
		if lifecycle, ok := updates["lifecycle_status"].(string); ok && lifecycle != "active" && *req.IsEnabled {
			BadRequestFields(w, "节点信息校验失败。", map[string]string{"is_enabled": "维护或退役节点不能承载对外服务。"})
			return
		}
		if node.LifecycleStatus != "" && node.LifecycleStatus != "active" && *req.IsEnabled && req.LifecycleStatus == nil {
			BadRequestFields(w, "节点信息校验失败。", map[string]string{"lifecycle_status": "请先将生命周期恢复为正常，再启用对外服务。"})
			return
		}
		updates["is_enabled"] = *req.IsEnabled
	}
	if len(updates) == 0 {
		BadRequest(w, "no valid update fields")
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.update", fmt.Sprintf("node:%d", node.ID), "metadata or lifecycle updated")
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.db.First(&node, node.ID).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, node)
}

func (h *handlers) NodeDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	blockers := map[string]int64{}
	for name, query := range map[string]*gorm.DB{
		"protocol_endpoints":   h.db.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", node.ID),
		"managed_certificates": h.db.Model(&model.ManagedCertificate{}).Where("node_id = ?", node.ID),
		"managed_dns_records":  h.db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", node.ID),
		"running_operations":   h.db.Model(&model.NodeOperation{}).Where("node_id = ? AND status = ?", node.ID, "running"),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		if count > 0 {
			blockers[name] = count
		}
	}
	if len(blockers) > 0 {
		writeJSON(w, http.StatusConflict, "删除节点前请先删除其协议服务、证书和 DNS 解析，并等待正在运行的节点任务结束。", map[string]interface{}{"blockers": blockers})
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := createAuditLog(tx, claims, "node.delete", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("name=%s", node.Name)); err != nil {
			return err
		}
		if err := tx.Delete(&model.NodeKernelState{}, "node_id = ?", node.ID).Error; err != nil {
			return err
		}
		return tx.Delete(&node).Error
	}); err != nil {
		ServerError(w, err)
		return
	}
	h.invalidateZeroEventCredential(node.ID)
	OK(w, map[string]interface{}{"id": node.ID, "deleted": true, "remote_zero_retained": true})
}

func (h *handlers) NodeSSHTestHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}

	var req nodeSSHTestReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.NodeID == 0 {
		BadRequest(w, "node_id is required")
		return
	}

	node, err := h.loadNode(req.NodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	if err := h.validateNodeSSH(node); err != nil {
		BadRequestError(w, err)
		return
	}

	command := "echo zboard-node-ok"
	if normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) != sshPrivilegeNone {
		command = "test \"$(id -u)\" = \"0\" && echo zboard-node-ok"
	}
	output, elapsed, execErr := h.execSSHCommandWithPrivilege(node, command, normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) != sshPrivilegeNone)
	now := time.Now().UTC()
	updates := map[string]interface{}{"last_sync_at": now}
	if execErr == nil {
		updates["ssh_verified_at"] = now
	} else {
		updates["ssh_verified_at"] = nil
	}
	if saveErr := h.db.Model(&node).Updates(updates).Error; saveErr != nil {
		ServerError(w, saveErr)
		return
	}

	if execErr != nil {
		BadRequest(w, execErr.Error())
		return
	}
	if err := h.db.First(&node, node.ID).Error; err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"node":       node,
		"output":     strings.TrimSpace(output),
		"latency_ms": elapsed.Milliseconds(),
	})
}

func (h *handlers) NodeSSHConfigHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req nodeSSHConfigReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	req.SSHHost = strings.TrimSpace(req.SSHHost)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	authMethod := normalizeSSHAuthMethod(req.SSHAuthMethod)
	if strings.TrimSpace(req.SSHAuthMethod) == "" {
		authMethod = normalizeSSHAuthMethod(node.SSHAuthMethod)
	}
	privilegeMode := normalizeSSHPrivilegeMode(req.SSHPrivilegeMode)
	if strings.TrimSpace(req.SSHPrivilegeMode) == "" {
		privilegeMode = normalizeSSHPrivilegeMode(node.SSHPrivilegeMode)
	}
	credential := req.SSHPwd
	if authMethod == sshAuthPrivateKey {
		credential = req.SSHPrivateKey
	}
	encryptedCredential := node.SSHPwd
	plainCredential := credential
	if credential == "" {
		if authMethod != normalizeSSHAuthMethod(node.SSHAuthMethod) {
			field := "ssh_password"
			if authMethod == sshAuthPrivateKey {
				field = "ssh_private_key"
			}
			BadRequestFields(w, "SSH 配置校验失败。", map[string]string{field: "切换认证方式时必须提供新的登录凭证。"})
			return
		}
		plainCredential, err = h.credentialCipher.Decrypt(node.SSHPwd)
		if err != nil {
			ServerError(w, err)
			return
		}
	} else {
		encryptedCredential, err = h.credentialCipher.Encrypt(credential)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	encryptedPassphrase := node.SSHPrivateKeyPassphrase
	plainPassphrase := ""
	if authMethod == sshAuthPrivateKey {
		if req.SSHPrivateKeyPassphrase != nil {
			plainPassphrase = *req.SSHPrivateKeyPassphrase
			encryptedPassphrase, err = h.credentialCipher.Encrypt(plainPassphrase)
			if err != nil {
				ServerError(w, err)
				return
			}
		} else {
			plainPassphrase, err = h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
			if err != nil {
				ServerError(w, err)
				return
			}
		}
		if _, err := parseSSHPrivateKey(plainCredential, plainPassphrase); err != nil {
			BadRequestError(w, err)
			return
		}
	} else {
		encryptedPassphrase = ""
	}
	encryptedPrivilegePassword := node.SSHPrivilegePassword
	plainPrivilegePassword := ""
	if req.SSHPrivilegePassword != nil {
		plainPrivilegePassword = *req.SSHPrivilegePassword
		encryptedPrivilegePassword, err = h.credentialCipher.Encrypt(plainPrivilegePassword)
		if err != nil {
			ServerError(w, err)
			return
		}
	} else {
		plainPrivilegePassword, err = h.credentialCipher.Decrypt(node.SSHPrivilegePassword)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	if err := validateSSHPrivilege(privilegeMode, plainPrivilegePassword); err != nil {
		BadRequestError(w, err)
		return
	}
	targetChanged := !strings.EqualFold(req.SSHHost, strings.TrimSpace(node.SSHHost)) || req.SSHPort != node.SSHPort
	targetFingerprint := node.SSHHostKeyFingerprint
	if targetChanged {
		targetFingerprint = ""
	}
	if err := validateSSHFields(req.SSHHost, req.SSHPort, req.SSHUser, authMethod, encryptedCredential, targetFingerprint); err != nil {
		BadRequestError(w, err)
		return
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"ssh_host":                   req.SSHHost,
			"ssh_port":                   req.SSHPort,
			"ssh_user":                   req.SSHUser,
			"ssh_auth_method":            authMethod,
			"ssh_pwd":                    encryptedCredential,
			"ssh_private_key_passphrase": encryptedPassphrase,
			"ssh_privilege_mode":         privilegeMode,
			"ssh_privilege_password":     encryptedPrivilegePassword,
			"ssh_verified_at":            nil,
		}
		if targetChanged {
			updates["ssh_host_key_fingerprint"] = ""
		}
		if err := tx.Model(&node).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{
			UserID: auditUserID(claims.UserID),
			Actor:  claims.Email,
			Action: "node.ssh_config.update",
			Target: fmt.Sprintf("node:%d", node.ID),
			Detail: fmt.Sprintf("auth_method=%s privilege_mode=%s target_changed=%t", authMethod, privilegeMode, targetChanged),
		}).Error
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	node.SSHHost = req.SSHHost
	node.SSHPort = req.SSHPort
	node.SSHUser = req.SSHUser
	node.SSHAuthMethod = authMethod
	node.SSHPwd = encryptedCredential
	node.SSHPrivateKeyPassphrase = encryptedPassphrase
	node.SSHPrivilegeMode = privilegeMode
	node.SSHPrivilegePassword = encryptedPrivilegePassword
	node.SSHPrivilegeConfigured = encryptedPrivilegePassword != ""
	node.SSHHostKeyFingerprint = targetFingerprint
	node.SSHVerifiedAt = nil
	OK(w, node)
}

func (h *handlers) NodeSSHHostKeyResetHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(map[string]interface{}{
			"ssh_host_key_fingerprint": "",
			"ssh_verified_at":          nil,
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.ssh_host_key.reset", fmt.Sprintf("node:%d", node.ID), "next successful SSH connection will enroll the host key")
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"node_id": node.ID, "host_key_trust_reset": true})
}

func (h *handlers) NodeConnectorCredentialRotateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	apiKey, prefix, err := newNodeReportSecret()
	if err != nil {
		ServerError(w, err)
		return
	}
	encryptedAPIKey, err := h.credentialCipher.Encrypt(apiKey)
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(map[string]interface{}{
			"node_credential":            encryptedAPIKey,
			"node_credential_prefix":     prefix,
			"node_credential_revoked_at": nil,
			"connector_last_seen_at":     nil,
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.connector_credential.rotate", fmt.Sprintf("node:%d", node.ID), prefix)
	}); err != nil {
		ServerError(w, err)
		return
	}
	h.invalidateZeroEventCredential(node.ID)
	OK(w, map[string]interface{}{
		"node_id":        strconv.FormatUint(uint64(node.ID), 10),
		"api_key":        apiKey,
		"api_key_prefix": prefix,
		"notice":         "api_key is shown once; rotating invalidates the previous connector credential",
	})
}

func (h *handlers) NodeConnectorCredentialRevokeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	now := time.Now().UTC()
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Node{}).
			Where("id = ? AND node_credential <> '' AND node_credential_revoked_at IS NULL", nodeID).
			Updates(map[string]interface{}{"node_credential_revoked_at": now, "connector_last_seen_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return createAuditLog(tx, claims, "node.connector_credential.revoke", fmt.Sprintf("node:%d", nodeID), "credential revoked")
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	h.invalidateZeroEventCredential(nodeID)
	OK(w, map[string]interface{}{"node_id": nodeID, "revoked": true})
}

func (h *handlers) NodeConnectorHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.authenticateNodeConnector(r, nodeID)
	if err != nil {
		Unauthorized(w, "invalid node connector authentication")
		return
	}
	heartbeat, err := decodeNodeConnectorHeartbeat(r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if heartbeat.NodeID != strconv.FormatUint(uint64(nodeID), 10) {
		BadRequest(w, "node_id does not match request path")
		return
	}
	heartbeat.BuildID = strings.TrimSpace(heartbeat.BuildID)
	if len(heartbeat.BuildID) > 64 {
		BadRequest(w, "build_id is too long")
		return
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"is_online":              true,
		"status":                 1,
		"last_seen_at":           now,
		"connector_last_seen_at": now,
		"uptime_seconds":         heartbeat.UptimeSeconds,
		"active_flows":           heartbeat.ActiveFlows,
		"bytes_up":               heartbeat.BytesUp,
		"bytes_down":             heartbeat.BytesDown,
	}
	if heartbeat.BuildID != "" {
		updates["version"] = heartbeat.BuildID
	}
	result := h.db.Model(&model.Node{}).
		Where("id = ? AND is_enabled = ? AND node_credential = ? AND node_credential_revoked_at IS NULL", node.ID, true, node.NodeCredential).
		Updates(updates)
	if result.Error != nil {
		ServerError(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		Unauthorized(w, "invalid node connector authentication")
		return
	}
	go h.reconcileExpiredCredentials(now)
	writeNodeConnectorJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *handlers) NodeConnectorCommandsHandler(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if _, err := h.authenticateNodeConnector(r, nodeID); err != nil {
		Unauthorized(w, "invalid node connector authentication")
		return
	}
	writeNodeConnectorJSON(w, http.StatusOK, []interface{}{})
}

func (h *handlers) NodeReportCredentialRotateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	rawSecret, prefix, err := newNodeReportSecret()
	if err != nil {
		ServerError(w, err)
		return
	}
	encryptedSecret, err := h.credentialCipher.Encrypt(rawSecret)
	if err != nil {
		ServerError(w, err)
		return
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(map[string]interface{}{
			"traffic_secret":            encryptedSecret,
			"traffic_secret_prefix":     prefix,
			"traffic_secret_revoked_at": nil,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{
			UserID: auditUserID(claims.UserID),
			Actor:  claims.Email,
			Action: "node.traffic_credential.rotate",
			Target: fmt.Sprintf("node:%d", node.ID),
			Detail: prefix,
		}).Error
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"node_id":       node.ID,
		"secret":        rawSecret,
		"secret_prefix": prefix,
		"notice":        "secret is shown once; rotating invalidates the previous credential",
	})
}

func (h *handlers) NodeReportCredentialRevokeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	now := time.Now().UTC()
	err = h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Node{}).
			Where("id = ? AND traffic_secret <> '' AND traffic_secret_revoked_at IS NULL", nodeID).
			Update("traffic_secret_revoked_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(&model.AuditLog{
			UserID: auditUserID(claims.UserID),
			Actor:  claims.Email,
			Action: "node.traffic_credential.revoke",
			Target: fmt.Sprintf("node:%d", nodeID),
			Detail: "credential revoked",
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"node_id": nodeID, "revoked": true})
}

func (h *handlers) ProtocolEndpointCreateHandler(w http.ResponseWriter, r *http.Request) {
	h.saveProtocolEndpoint(w, r, 0)
}

func (h *handlers) ProtocolEndpointUpdateHandler(w http.ResponseWriter, r *http.Request) {
	endpointID, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.saveProtocolEndpoint(w, r, endpointID)
}

func (h *handlers) ProtocolEndpointDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	endpointID, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var endpoint model.ProtocolEndpoint
	if err := h.db.First(&endpoint, endpointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	var activePlanCount int64
	if err := h.db.Table("node_group_endpoints").
		Joins("JOIN plans ON plans.node_group_id = node_group_endpoints.node_group_id").
		Where("node_group_endpoints.protocol_endpoint_id = ? AND plans.is_active = ?", endpoint.ID, true).
		Count(&activePlanCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if activePlanCount > 0 {
		writeJSON(w, http.StatusConflict, "删除协议服务前请先从所有已发布套餐的节点组中解绑。", map[string]interface{}{"blockers": map[string]int64{"active_plans": activePlanCount}})
		return
	}
	var runningDeployments int64
	if err := h.db.Model(&model.ProtocolDeployment{}).
		Where("protocol_endpoint_id = ? AND status = ?", endpoint.ID, "running").
		Count(&runningDeployments).Error; err != nil {
		ServerError(w, err)
		return
	}
	if runningDeployments > 0 {
		writeJSON(w, http.StatusConflict, "该协议服务仍有发布任务运行，请等待任务结束后再删除。", nil)
		return
	}

	wasActive := endpoint.IsActive
	if wasActive {
		if err := h.db.Model(&endpoint).Update("is_active", false).Error; err != nil {
			ServerError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
		_, _, publishErr := h.publishNodeConfigForNode(ctx, endpoint.NodeID, endpoint.ID, claims.UserID)
		cancel()
		if publishErr != nil {
			if restoreErr := h.db.Model(&endpoint).Update("is_active", true).Error; restoreErr != nil {
				ServerError(w, fmt.Errorf("remove protocol from node: %w; restore active state: %v", publishErr, restoreErr))
				return
			}
			BadRequest(w, "删除前无法从节点运行配置移除该协议服务："+publishErr.Error())
			return
		}
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := createAuditLog(tx, claims, "protocol_endpoint.delete", fmt.Sprintf("protocol_endpoint:%d", endpoint.ID),
			fmt.Sprintf("node=%d protocol=%s was_active=%t", endpoint.NodeID, endpoint.Protocol, wasActive)); err != nil {
			return err
		}
		if err := tx.Delete(&model.NodeGroupEndpoint{}, "protocol_endpoint_id = ?", endpoint.ID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.CertificateProtocolEndpoint{}, "protocol_endpoint_id = ?", endpoint.ID).Error; err != nil {
			return err
		}
		revokedAt := time.Now().UTC()
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("protocol_endpoint_id = ? AND revoked_at IS NULL", endpoint.ID).
			Updates(map[string]interface{}{"status": protocolCredentialStatusRevoked, "revoked_at": revokedAt}).Error; err != nil {
			return err
		}
		return tx.Delete(&endpoint).Error
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": endpoint.ID, "deleted": true, "removed_from_runtime": wasActive})
}

func (h *handlers) saveProtocolEndpoint(w http.ResponseWriter, r *http.Request, endpointID uint) {
	requestStartedAt := time.Now()
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req protocolEndpointWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	membershipChanges, err := normalizeProtocolEndpointNodeGroupMembershipChanges(req.NodeGroupMembershipChanges, endpointID == 0)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "vmess"
	}
	if !h.isProtocolSupported(protocol) {
		BadRequestFields(w, "协议服务校验失败。", map[string]string{"protocol": "请选择受支持的协议类型。"})
		return
	}
	var existing model.ProtocolEndpoint
	var existingEffectSnapshot *protocolEndpointEffectSnapshot
	var existingManagedCertificateID uint
	if endpointID != 0 {
		if err := h.db.First(&existing, endpointID).Error; err != nil {
			NotFound(w)
			return
		}
		existingServerConfig, decryptErr := h.credentialCipher.Decrypt(existing.ServerConfig)
		if decryptErr != nil {
			ServerError(w, decryptErr)
			return
		}
		existingClientConfig := existing.ClientConfig
		if !strings.EqualFold(existing.Protocol, "mieru") {
			existingServerConfig, existingClientConfig, err = normalizeManagedProtocolTemplates(existing.Protocol, existingServerConfig, existingClientConfig)
			if err != nil {
				ServerError(w, err)
				return
			}
		}
		managedCertificateIDs, loadErr := h.loadManagedCertificateIDsForEndpoints([]uint{existing.ID})
		if loadErr != nil {
			ServerError(w, loadErr)
			return
		}
		if managedCertificateID := managedCertificateIDs[existing.ID]; managedCertificateID != nil {
			existingManagedCertificateID = *managedCertificateID
		}
		snapshot := protocolEndpointEffectSnapshot{
			NodeID: existing.NodeID, Name: existing.Name, Protocol: existing.Protocol, Address: existing.Address,
			Port: existing.Port, PublicPort: existing.PublicPort, Cipher: existing.Cipher,
			ParentProtocolID: existing.ParentProtocolID, MultiplierMilli: existing.MultiplierMilli,
			ServerConfig: existingServerConfig, ClientConfig: existingClientConfig,
			OptionalConfig: existing.OptionalConfig, Tags: existing.Tags,
			IsActive: existing.IsActive, SortOrder: existing.SortOrder,
			ManagedCertificateID: existingManagedCertificateID,
		}
		existingEffectSnapshot = &snapshot
	}
	if req.NodeID == 0 {
		BadRequestFields(w, "协议服务校验失败。", map[string]string{"node_id": "请选择承载节点。"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	fields := make(map[string]string)
	if req.Name == "" {
		fields["name"] = "请输入服务名称。"
	}
	if req.Address == "" {
		fields["address"] = "请输入客户端可访问的对外地址。"
	}
	if req.Port <= 0 || req.Port > 65535 {
		fields["port"] = "监听端口必须在 1–65535 之间。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "协议服务校验失败。", fields)
		return
	}
	if req.PublicPort == 0 {
		req.PublicPort = req.Port
	}
	if req.PublicPort <= 0 || req.PublicPort > 65535 {
		BadRequestFields(w, "协议服务校验失败。", map[string]string{"public_port": "客户端连接端口必须在 1–65535 之间。"})
		return
	}
	if req.MultiplierMilli <= 0 || req.MultiplierMilli > 100000 {
		BadRequestFields(w, "协议服务校验失败。", map[string]string{"multiplier_milli": "流量倍率必须大于 0 且不超过 100。"})
		return
	}
	if protocol == "mieru" {
		req.Config, req.ClientConfig, err = h.prepareMieruEndpointConfigs(endpointID, req.Config, req.ClientConfig)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		if err != nil {
			var validationErr *requestValidationError
			if errors.As(err, &validationErr) {
				BadRequestError(w, err)
			} else {
				ServerError(w, err)
			}
			return
		}
	}
	if err := validateNodeProtocolConfigs(protocol, req.Config, req.ClientConfig); err != nil {
		BadRequestError(w, err)
		return
	}
	if err := validateOptionalJSONObject("optional_config", req.OptionalConfig); err != nil {
		BadRequestError(w, err)
		return
	}
	if err := validateOptionalJSONArray("tags", req.Tags); err != nil {
		BadRequestError(w, err)
		return
	}

	node, err := h.loadNode(req.NodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequestFields(w, "协议服务校验失败。", map[string]string{"node_id": "所选承载节点不存在。"})
			return
		}
		ServerError(w, err)
		return
	}
	if supported, reason := h.protocolKernelSupportForNode(protocol, node); !supported {
		// Existing records on an older kernel remain recoverable: administrators
		// may disable them, while creation, re-enabling and ordinary edits wait
		// until that concrete node runs a compatible Zero release.
		canDisableExisting := existing.ID != 0 &&
			strings.EqualFold(existing.Protocol, protocol) &&
			req.IsActive != nil && !*req.IsActive
		if !canDisableExisting {
			BadRequestFields(w, "协议服务校验失败。", map[string]string{"protocol": reason})
			return
		}
	}
	if req.Config, req.ClientConfig, err = normalizeManagedProtocolTemplates(protocol, req.Config, req.ClientConfig); err != nil {
		BadRequestError(w, err)
		return
	}
	var managedCertificate *model.ManagedCertificate
	if req.ManagedCertificateID != nil && *req.ManagedCertificateID != 0 {
		certificate, certificateErr := h.loadUsableManagedCertificate(*req.ManagedCertificateID, node.ID, protocol, time.Now().UTC())
		if certificateErr != nil {
			BadRequestFields(w, "协议服务校验失败。", map[string]string{"managed_certificate_id": certificateErr.Error()})
			return
		}
		managedCertificate = &certificate
	}
	if err := h.validateProtocolParent(endpointID, node.ID, req.ParentProtocolID); err != nil {
		BadRequestError(w, err)
		return
	}
	runtimeKey := uuid.NewString()
	isActive := false
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	var previousNodeID uint
	if endpointID != 0 {
		previousNodeID = existing.NodeID
		if existing.Protocol != protocol || (strings.EqualFold(existing.Protocol, "shadowsocks") && (existing.Port != req.Port || existing.PublicPort != req.PublicPort)) {
			var credentialCount int64
			if err := h.db.Model(&model.ProtocolCredential{}).Where("protocol_endpoint_id = ?", existing.ID).Count(&credentialCount).Error; err != nil {
				ServerError(w, err)
				return
			}
			if credentialCount > 0 {
				BadRequestFields(w, "协议服务校验失败。", map[string]string{"protocol": "该服务已有订阅凭证；请创建新服务后迁移，不能直接更换协议或 Shadowsocks 端口。"})
				return
			}
		}
		runtimeKey = existing.RuntimeKey
		if req.IsActive == nil {
			isActive = existing.IsActive
		}
	}

	nextManagedCertificateID := uint(0)
	if managedCertificate != nil {
		nextManagedCertificateID = managedCertificate.ID
	}
	// Business delivery order is changed only through the complete-scope ordering command.
	// Ordinary endpoint edits preserve the current position; new endpoints are appended in the transaction below.
	effectiveSortOrder := existing.SortOrder
	changeEffects := classifyProtocolEndpointChange(existingEffectSnapshot, protocolEndpointEffectSnapshot{
		NodeID: node.ID, Name: req.Name, Protocol: protocol, Address: req.Address,
		Port: req.Port, PublicPort: req.PublicPort, Cipher: req.Cipher,
		ParentProtocolID: req.ParentProtocolID, MultiplierMilli: req.MultiplierMilli,
		ServerConfig: req.Config, ClientConfig: req.ClientConfig,
		OptionalConfig: normalizeOptionalJSON(req.OptionalConfig, "{}"),
		Tags:           normalizeOptionalJSON(req.Tags, "[]"), IsActive: isActive,
		SortOrder: effectiveSortOrder, ManagedCertificateID: nextManagedCertificateID,
	})

	encryptedServerConfig, err := h.credentialCipher.Encrypt(req.Config)
	if err != nil {
		ServerError(w, err)
		return
	}

	endpoint := existing
	endpoint.ID = endpointID
	endpoint.NodeID = node.ID
	endpoint.Name = req.Name
	endpoint.RuntimeKey = runtimeKey
	endpoint.Protocol = protocol
	endpoint.Address = req.Address
	endpoint.Port = req.Port
	endpoint.PublicPort = req.PublicPort
	endpoint.Cipher = req.Cipher
	endpoint.ParentProtocolID = req.ParentProtocolID
	endpoint.MultiplierMilli = req.MultiplierMilli
	if protocol != "mieru" {
		endpoint.MieruPrincipalReady = false
	}
	if protocol != "trojan" && protocol != "hysteria2" || endpointID == 0 || existing.NodeID != node.ID || !strings.EqualFold(existing.Protocol, protocol) {
		endpoint.ManagedPrincipalReady = false
	}
	endpoint.ServerConfig = encryptedServerConfig
	endpoint.ClientConfig = req.ClientConfig
	endpoint.OptionalConfig = normalizeOptionalJSON(req.OptionalConfig, "{}")
	endpoint.Tags = normalizeOptionalJSON(req.Tags, "[]")
	endpoint.IsActive = isActive
	endpoint.SortOrder = effectiveSortOrder

	action := "protocol_endpoint.create"
	if endpointID != 0 {
		action = "protocol_endpoint.update"
	}
	var membershipMutation *protocolEndpointNodeGroupMutationResult
	removedMemberships := protocolEndpointNodeGroupRemovalSet(membershipChanges)
	validationFinishedAt := time.Now()
	transactionErr := h.db.Transaction(func(tx *gorm.DB) error {
		if endpointID == 0 {
			var last model.ProtocolEndpoint
			lastResult := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id", "sort_order").
				Order("sort_order desc, id desc").
				First(&last)
			if lastResult.Error != nil && !errors.Is(lastResult.Error, gorm.ErrRecordNotFound) {
				return lastResult.Error
			}
			if lastResult.Error == nil {
				endpoint.SortOrder = last.SortOrder + 1
			}
			if err := tx.Create(&endpoint).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Save(&endpoint).Error; err != nil {
				return err
			}
			if err := h.migrateProtocolEndpointCredentials(tx, existing, endpoint); err != nil {
				return err
			}
		}
		if !endpoint.IsActive {
			if err := h.validateProtocolEndpointDeactivationMemberships(tx, endpoint.ID, removedMemberships); err != nil {
				return err
			}
		}
		var membershipErr error
		membershipMutation, membershipErr = h.applyProtocolEndpointNodeGroupMembershipChanges(tx, claims, endpoint, membershipChanges)
		if membershipErr != nil {
			return membershipErr
		}
		if err := tx.Where("protocol_endpoint_id = ?", endpoint.ID).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
			return err
		}
		if managedCertificate != nil {
			if err := tx.Create(&model.CertificateProtocolEndpoint{
				ManagedCertificateID: managedCertificate.ID,
				ProtocolEndpointID:   endpoint.ID,
			}).Error; err != nil {
				return err
			}
		}
		detail := fmt.Sprintf("node=%d protocol=%s multiplier_milli=%d", node.ID, protocol, endpoint.MultiplierMilli)
		if managedCertificate != nil {
			detail += fmt.Sprintf(" managed_certificate=%d", managedCertificate.ID)
		}
		if previousNodeID != 0 && previousNodeID != node.ID {
			detail += fmt.Sprintf(" previous_node=%d", previousNodeID)
		}
		detail += fmt.Sprintf(" effect=%s publish_status=%s", changeEffects.Effect, changeEffects.PublishStatus)
		if membershipMutation != nil {
			detail += fmt.Sprintf(" node_groups_added=%d node_groups_removed=%d membership_publish_status=%s", len(membershipMutation.AddedNodeGroupIDs), len(membershipMutation.RemovedNodeGroupIDs), membershipMutation.PublishStatus)
		}
		return createAuditLog(tx, claims, action, fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), detail)
	})
	transactionFinishedAt := time.Now()
	if err := transactionErr; err != nil {
		var conflict *protocolEndpointNodeGroupRevisionConflictError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, "节点组已被其他管理员更新，请重新加载协议服务后再保存。", map[string]interface{}{"conflicts": conflict.Conflicts})
			return
		}
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		BadRequest(w, err.Error())
		return
	}
	if membershipMutation != nil {
		for index := range membershipMutation.ReconcileTasks {
			_ = h.startPersistedAdminTask(&membershipMutation.ReconcileTasks[index])
		}
	}
	membershipPublishedNodeIDs := []uint(nil)
	if membershipMutation != nil {
		membershipPublishedNodeIDs = membershipMutation.AffectedNodeIDs
	}
	for _, affectedNodeID := range protocolEndpointDirectPublishNodeIDs(changeEffects.AffectedNodeIDs, membershipPublishedNodeIDs) {
		h.scheduleNodeConfigPublish(affectedNodeID, endpoint.ID, claims.UserID)
	}
	taskEnqueueFinishedAt := time.Now()
	memberships, err := loadProtocolEndpointNodeGroupMemberships(h.db, endpoint.ID)
	if err != nil {
		ServerError(w, err)
		return
	}
	responseFinishedAt := time.Now()
	timing := newProtocolEndpointMutationTiming(requestStartedAt, validationFinishedAt, transactionFinishedAt, taskEnqueueFinishedAt, responseFinishedAt)
	w.Header().Set("Server-Timing", fmt.Sprintf("validation;dur=%d, transaction;dur=%d, task_enqueue;dur=%d, response_preparation;dur=%d", timing.ValidationMS, timing.TransactionMS, timing.TaskEnqueueMS, timing.ResponsePreparationMS))
	OK(w, protocolEndpointMutationResponse{
		ProtocolEndpoint:              endpoint,
		protocolEndpointChangeEffects: changeEffects,
		NodeGroupMemberships:          memberships,
		NodeGroupMembership:           membershipMutation,
		Timing:                        timing,
	})
}

func (h *handlers) ProtocolEndpointDeployHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	endpointID, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
	defer cancel()
	deployment, elapsed, err := h.publishNodeConfig(ctx, endpointID, claims.UserID)
	if err != nil {
		BadRequest(w, "protocol publish failed: "+err.Error())
		return
	}
	OK(w, map[string]interface{}{"deployment": deployment, "latency_ms": elapsed.Milliseconds()})
}

type protocolEndpointAdminDetail struct {
	model.ProtocolEndpoint
	Config                  string                                `json:"config"`
	ManagedCertificateID    *uint                                 `json:"managed_certificate_id,omitempty"`
	LatestDeployment        *model.ProtocolDeployment             `json:"latest_deployment,omitempty"`
	Usage                   protocolEndpointUsage                 `json:"usage"`
	KernelSupported         bool                                  `json:"kernel_supported"`
	KernelUnsupportedReason string                                `json:"kernel_unsupported_reason,omitempty"`
	NodeGroupMemberships    []protocolEndpointNodeGroupMembership `json:"node_group_memberships"`
}

type protocolEndpointUsage struct {
	ActiveFlows       int64      `json:"active_flows"`
	ActiveUsers       int64      `json:"active_users"`
	ActiveCredentials int64      `json:"active_credentials"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	UsedBytesToday    int64      `json:"used_bytes_today"`
	UsedBytesTotal    int64      `json:"used_bytes_total"`
}

type protocolDeploymentListItem struct {
	ID         uint       `json:"id"`
	Status     string     `json:"status"`
	HasError   bool       `json:"has_error"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type protocolEndpointListItem struct {
	ID                      uint                        `json:"id"`
	NodeID                  uint                        `json:"node_id"`
	NodeName                string                      `json:"node_name"`
	Name                    string                      `json:"name"`
	Protocol                string                      `json:"protocol"`
	Address                 string                      `json:"address"`
	Port                    int                         `json:"port"`
	PublicPort              int                         `json:"public_port"`
	ParentProtocolID        *uint                       `json:"parent_protocol_id,omitempty"`
	ManagedCertificateID    *uint                       `json:"managed_certificate_id,omitempty"`
	MultiplierMilli         int64                       `json:"multiplier_milli"`
	ManagedPrincipalReady   bool                        `json:"managed_principal_ready"`
	MieruPrincipalReady     bool                        `json:"mieru_principal_ready"`
	IsActive                bool                        `json:"is_active"`
	SortOrder               int                         `json:"sort_order"`
	LatestDeployment        *protocolDeploymentListItem `json:"latest_deployment,omitempty"`
	Usage                   protocolEndpointUsage       `json:"usage"`
	KernelSupported         bool                        `json:"kernel_supported"`
	KernelUnsupportedReason string                      `json:"kernel_unsupported_reason,omitempty"`
	CreatedAt               time.Time                   `json:"created_at"`
	UpdatedAt               time.Time                   `json:"updated_at"`
}

func newProtocolEndpointListItem(endpoint model.ProtocolEndpoint, nodeName string, managedCertificateID *uint, deployment *model.ProtocolDeployment, usage protocolEndpointUsage, kernelSupported bool, kernelUnsupportedReason string) protocolEndpointListItem {
	item := protocolEndpointListItem{
		ID: endpoint.ID, NodeID: endpoint.NodeID, NodeName: nodeName, Name: endpoint.Name,
		Protocol: endpoint.Protocol, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
		ParentProtocolID: endpoint.ParentProtocolID, ManagedCertificateID: managedCertificateID, MultiplierMilli: endpoint.MultiplierMilli,
		ManagedPrincipalReady: endpoint.ManagedPrincipalReady, MieruPrincipalReady: endpoint.MieruPrincipalReady, IsActive: endpoint.IsActive, SortOrder: endpoint.SortOrder, Usage: usage,
		KernelSupported: kernelSupported, KernelUnsupportedReason: kernelUnsupportedReason,
		CreatedAt: endpoint.CreatedAt, UpdatedAt: endpoint.UpdatedAt,
	}
	if deployment != nil {
		item.LatestDeployment = &protocolDeploymentListItem{
			ID: deployment.ID, Status: deployment.Status, HasError: deployment.Error != "",
			StartedAt: deployment.StartedAt, FinishedAt: deployment.FinishedAt, CreatedAt: deployment.CreatedAt,
		}
	}
	return item
}

func (h *handlers) ProtocolEndpointDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	endpointID, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var endpoint model.ProtocolEndpoint
	if err := h.db.First(&endpoint, endpointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	serverConfig, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
	if err != nil {
		ServerError(w, fmt.Errorf("decrypt protocol endpoint config: %w", err))
		return
	}
	if strings.EqualFold(endpoint.Protocol, "mieru") {
		serverConfig, endpoint.ClientConfig = redactMieruEndpointAdminConfigs(serverConfig, endpoint.ClientConfig)
	} else if serverConfig, endpoint.ClientConfig, err = normalizeManagedProtocolTemplates(endpoint.Protocol, serverConfig, endpoint.ClientConfig); err != nil {
		ServerError(w, fmt.Errorf("normalize protocol endpoint template: %w", err))
		return
	}
	usage, err := h.loadProtocolEndpointUsage(endpoint.ID, time.Now().UTC())
	if err != nil {
		ServerError(w, err)
		return
	}
	managedCertificateIDs, err := h.loadManagedCertificateIDsForEndpoints([]uint{endpoint.ID})
	if err != nil {
		ServerError(w, err)
		return
	}
	node, err := h.loadNode(endpoint.NodeID)
	if err != nil {
		ServerError(w, err)
		return
	}
	kernelSupported, kernelUnsupportedReason := h.protocolKernelSupportForNode(endpoint.Protocol, node)
	memberships, err := loadProtocolEndpointNodeGroupMemberships(h.db, endpoint.ID)
	if err != nil {
		ServerError(w, err)
		return
	}
	detail := protocolEndpointAdminDetail{
		ProtocolEndpoint: endpoint, Config: serverConfig, ManagedCertificateID: managedCertificateIDs[endpoint.ID],
		Usage: usage, KernelSupported: kernelSupported, KernelUnsupportedReason: kernelUnsupportedReason,
		NodeGroupMemberships: memberships,
	}
	var deployment model.ProtocolDeployment
	if err := h.db.Where("protocol_endpoint_id = ?", endpoint.ID).Order("id desc").First(&deployment).Error; err == nil {
		detail.LatestDeployment = &deployment
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

func (h *handlers) ProtocolDeploymentListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.ProtocolDeployment{})
	for key, column := range map[string]string{"node_id": "node_id", "protocol_endpoint_id": "protocol_endpoint_id"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			parsed, parseErr := strconv.ParseUint(value, 10, 64)
			if parseErr != nil || parsed == 0 {
				BadRequest(w, "invalid "+key)
				return
			}
			query = query.Where(column+" = ?", parsed)
		}
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if status != "running" && status != "succeeded" && status != "failed" {
			BadRequest(w, "invalid status")
			return
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.ProtocolDeployment, 0)
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) applyProtocolEndpointFilters(query *gorm.DB, values url.Values) (*gorm.DB, error) {
	if rawIDs := strings.TrimSpace(values.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		ids := make([]uint64, 0, len(parts))
		for _, part := range parts {
			parsed, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
			if err != nil || parsed == 0 {
				return nil, errors.New("invalid ids")
			}
			ids = append(ids, parsed)
		}
		if len(ids) > 100 {
			return nil, errors.New("ids cannot contain more than 100 values")
		}
		query = query.Where("protocol_endpoints.id IN ?", ids)
	}
	if nodeID := strings.TrimSpace(values.Get("node_id")); nodeID != "" {
		parsed, err := strconv.ParseUint(nodeID, 10, 64)
		if err != nil || parsed == 0 {
			return nil, errors.New("invalid node_id")
		}
		query = query.Where("protocol_endpoints.node_id = ?", parsed)
	}
	if search := strings.ToLower(strings.TrimSpace(values.Get("q"))); search != "" {
		if len([]byte(search)) > 100 {
			return nil, validationError("协议端点筛选条件校验失败。", map[string]string{"q": "搜索内容不能超过 100 个 UTF-8 字节。"})
		}
		pattern := "%" + search + "%"
		query = query.Where("LOWER(protocol_endpoints.name) LIKE ? OR LOWER(protocol_endpoints.address) LIKE ?", pattern, pattern)
	}
	if protocol := strings.ToLower(strings.TrimSpace(values.Get("protocol"))); protocol != "" {
		if !h.isProtocolSupported(protocol) {
			return nil, errors.New("invalid protocol")
		}
		query = query.Where("protocol_endpoints.protocol = ?", protocol)
	}
	if rawActive := strings.TrimSpace(values.Get("active")); rawActive != "" {
		active, err := strconv.ParseBool(rawActive)
		if err != nil {
			return nil, errors.New("invalid active")
		}
		query = query.Where("protocol_endpoints.is_active = ?", active)
	}
	if deploymentStatus := strings.TrimSpace(values.Get("deployment_status")); deploymentStatus != "" {
		if deploymentStatus != "running" && deploymentStatus != "succeeded" && deploymentStatus != "failed" && deploymentStatus != "never" {
			return nil, errors.New("invalid deployment_status")
		}
		latestDeploymentIDs := h.db.Model(&model.ProtocolDeployment{}).Select("MAX(id)").Group("protocol_endpoint_id")
		if deploymentStatus == "never" {
			deployedEndpointIDs := h.db.Model(&model.ProtocolDeployment{}).Select("DISTINCT protocol_endpoint_id")
			query = query.Where("protocol_endpoints.id NOT IN (?)", deployedEndpointIDs)
		} else {
			matchingEndpointIDs := h.db.Model(&model.ProtocolDeployment{}).
				Select("protocol_endpoint_id").Where("id IN (?) AND status = ?", latestDeploymentIDs, deploymentStatus)
			query = query.Where("protocol_endpoints.id IN (?)", matchingEndpointIDs)
		}
	}
	return query, nil
}

func (h *handlers) ProtocolEndpointSelectionHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	query, err := h.applyProtocolEndpointFilters(h.db.Model(&model.ProtocolEndpoint{}), r.URL.Query())
	if err != nil {
		BadRequestError(w, err)
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	if total > maxEndpointSelection {
		BadRequestFields(w, "协议端点筛选结果过多。", map[string]string{
			"q": fmt.Sprintf("当前筛选匹配 %d 个端点，批量快照上限为 %d 个；请缩小搜索范围。", total, maxEndpointSelection),
		})
		return
	}
	ids := make([]uint, 0, int(total))
	if err := query.Order("protocol_endpoints.id asc").Pluck("protocol_endpoints.id", &ids).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, protocolEndpointSelectionSnapshot{
		IDs:        ids,
		Total:      total,
		ResolvedAt: time.Now().UTC(),
	})
}

func (h *handlers) ProtocolEndpointListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	paged := wantsPagedList(r)
	offset, limit := 0, 50
	var err error
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	endpoints := make([]model.ProtocolEndpoint, 0)
	query, err := h.applyProtocolEndpointFilters(h.db.Model(&model.ProtocolEndpoint{}), r.URL.Query())
	if err != nil {
		BadRequestError(w, err)
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	sortColumn := map[string]string{
		"sort_order": "protocol_endpoints.sort_order", "id": "protocol_endpoints.id", "name": "protocol_endpoints.name", "protocol": "protocol_endpoints.protocol",
		"node_id": "protocol_endpoints.node_id", "multiplier": "protocol_endpoints.multiplier_milli", "updated_at": "protocol_endpoints.updated_at",
	}[strings.TrimSpace(r.URL.Query().Get("sort"))]
	if sortColumn == "" {
		sortColumn = "protocol_endpoints.sort_order"
	}
	direction := "asc"
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "desc") {
		direction = "desc"
	}
	query = query.Order(sortColumn + " " + direction + ", protocol_endpoints.id asc")
	if paged {
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Find(&endpoints).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]protocolEndpointListItem, 0, len(endpoints))
	now := time.Now().UTC()
	usageByEndpoint, err := h.loadProtocolEndpointUsageBatch(endpoints, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	deploymentByEndpoint, err := h.loadLatestProtocolDeployments(endpoints)
	if err != nil {
		ServerError(w, err)
		return
	}
	nodesByID, err := h.loadProtocolEndpointNodes(endpoints)
	if err != nil {
		ServerError(w, err)
		return
	}
	managedCertificateIDs, err := h.loadManagedCertificateIDsForEndpoints(protocolEndpointIDs(endpoints))
	if err != nil {
		ServerError(w, err)
		return
	}
	for _, endpoint := range endpoints {
		node := nodesByID[endpoint.NodeID]
		kernelSupported, kernelUnsupportedReason := h.protocolKernelSupportForNode(endpoint.Protocol, node)
		items = append(items, newProtocolEndpointListItem(endpoint, node.Name, managedCertificateIDs[endpoint.ID], deploymentByEndpoint[endpoint.ID], usageByEndpoint[endpoint.ID], kernelSupported, kernelUnsupportedReason))
	}
	if paged {
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	OK(w, items)
}

func (h *handlers) loadProtocolEndpointUsage(endpointID uint, now time.Time) (protocolEndpointUsage, error) {
	usageByEndpoint, err := h.loadProtocolEndpointUsageBatch([]model.ProtocolEndpoint{{ID: endpointID}}, now)
	return usageByEndpoint[endpointID], err
}

func protocolEndpointIDs(endpoints []model.ProtocolEndpoint) []uint {
	ids := make([]uint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ids = append(ids, endpoint.ID)
	}
	return ids
}

func (h *handlers) loadProtocolEndpointUsageBatch(endpoints []model.ProtocolEndpoint, now time.Time) (map[uint]protocolEndpointUsage, error) {
	usageByEndpoint := make(map[uint]protocolEndpointUsage, len(endpoints))
	ids := protocolEndpointIDs(endpoints)
	if len(ids) == 0 {
		return usageByEndpoint, nil
	}
	for _, id := range ids {
		usageByEndpoint[id] = protocolEndpointUsage{}
	}
	type countRow struct {
		ProtocolEndpointID uint
		ActiveFlows        int64
		ActiveUsers        int64
	}
	flowRows := make([]countRow, 0, len(ids))
	if err := h.db.Model(&model.FlowUsage{}).
		Select("flow_usages.protocol_endpoint_id, COUNT(*) AS active_flows, COUNT(DISTINCT subscriptions.user_id) AS active_users").
		Joins("JOIN subscriptions ON subscriptions.id = flow_usages.subscription_id").
		Where("flow_usages.protocol_endpoint_id IN ? AND flow_usages.status = ? AND flow_usages.last_seen_at >= ?", ids, "active", now.Add(-protocolActivityWindow)).
		Group("flow_usages.protocol_endpoint_id").Scan(&flowRows).Error; err != nil {
		return nil, err
	}
	for _, row := range flowRows {
		usage := usageByEndpoint[row.ProtocolEndpointID]
		usage.ActiveFlows = row.ActiveFlows
		usage.ActiveUsers = row.ActiveUsers
		usageByEndpoint[row.ProtocolEndpointID] = usage
	}
	type credentialRow struct {
		ProtocolEndpointID uint
		ActiveCredentials  int64
		LastUsedAt         *time.Time
	}
	credentialRows := make([]credentialRow, 0, len(ids))
	if err := h.db.Model(&model.ProtocolCredential{}).
		Select("protocol_endpoint_id, SUM(CASE WHEN status = ? AND revoked_at IS NULL AND expires_at > ? THEN 1 ELSE 0 END) AS active_credentials, MAX(last_used_at) AS last_used_at", protocolCredentialStatusActive, now).
		Where("protocol_endpoint_id IN ?", ids).
		Group("protocol_endpoint_id").Scan(&credentialRows).Error; err != nil {
		return nil, err
	}
	for _, row := range credentialRows {
		usage := usageByEndpoint[row.ProtocolEndpointID]
		usage.ActiveCredentials = row.ActiveCredentials
		usage.LastUsedAt = row.LastUsedAt
		usageByEndpoint[row.ProtocolEndpointID] = usage
	}
	type trafficRow struct {
		ProtocolEndpointID uint
		UsedBytesToday     int64
		UsedBytesTotal     int64
	}
	trafficRows := make([]trafficRow, 0, len(ids))
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := h.db.Model(&model.TrafficRecord{}).
		Select("protocol_endpoint_id, COALESCE(SUM(used_bytes), 0) AS used_bytes_total, COALESCE(SUM(CASE WHEN record_at >= ? THEN used_bytes ELSE 0 END), 0) AS used_bytes_today", dayStart).
		Where("protocol_endpoint_id IN ?", ids).
		Group("protocol_endpoint_id").Scan(&trafficRows).Error; err != nil {
		return nil, err
	}
	for _, row := range trafficRows {
		usage := usageByEndpoint[row.ProtocolEndpointID]
		usage.UsedBytesToday = row.UsedBytesToday
		usage.UsedBytesTotal = row.UsedBytesTotal
		usageByEndpoint[row.ProtocolEndpointID] = usage
	}
	return usageByEndpoint, nil
}

func (h *handlers) loadLatestProtocolDeployments(endpoints []model.ProtocolEndpoint) (map[uint]*model.ProtocolDeployment, error) {
	result := make(map[uint]*model.ProtocolDeployment, len(endpoints))
	ids := protocolEndpointIDs(endpoints)
	if len(ids) == 0 {
		return result, nil
	}
	latestIDs := h.db.Model(&model.ProtocolDeployment{}).
		Select("MAX(id)").Where("protocol_endpoint_id IN ?", ids).Group("protocol_endpoint_id")
	deployments := make([]model.ProtocolDeployment, 0, len(ids))
	if err := h.db.Where("id IN (?)", latestIDs).Find(&deployments).Error; err != nil {
		return nil, err
	}
	for index := range deployments {
		result[deployments[index].ProtocolEndpointID] = &deployments[index]
	}
	return result, nil
}

func (h *handlers) loadProtocolEndpointNodeNames(endpoints []model.ProtocolEndpoint) (map[uint]string, error) {
	result := make(map[uint]string)
	nodeIDs := make([]uint, 0, len(endpoints))
	seen := make(map[uint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if _, ok := seen[endpoint.NodeID]; ok {
			continue
		}
		seen[endpoint.NodeID] = struct{}{}
		nodeIDs = append(nodeIDs, endpoint.NodeID)
	}
	if len(nodeIDs) == 0 {
		return result, nil
	}
	type nodeNameRow struct {
		ID   uint
		Name string
	}
	rows := make([]nodeNameRow, 0, len(nodeIDs))
	if err := h.db.Model(&model.Node{}).Select("id, name").Where("id IN ?", nodeIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.Name
	}
	return result, nil
}

func (h *handlers) loadProtocolEndpointNodes(endpoints []model.ProtocolEndpoint) (map[uint]model.Node, error) {
	result := make(map[uint]model.Node)
	nodeIDs := make([]uint, 0, len(endpoints))
	seen := make(map[uint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if _, ok := seen[endpoint.NodeID]; ok {
			continue
		}
		seen[endpoint.NodeID] = struct{}{}
		nodeIDs = append(nodeIDs, endpoint.NodeID)
	}
	if len(nodeIDs) == 0 {
		return result, nil
	}
	var nodes []model.Node
	if err := h.db.Preload("KernelState").Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, node := range nodes {
		result[node.ID] = node
	}
	return result, nil
}

func (h *handlers) NodeGroupListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	paged := wantsPagedList(r)
	offset, limit := 0, 50
	var err error
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	query := h.db.Model(&model.NodeGroup{})
	if rawGroupID := strings.TrimSpace(r.URL.Query().Get("group_id")); rawGroupID != "" {
		groupID, parseErr := strconv.ParseUint(rawGroupID, 10, 64)
		if parseErr != nil || groupID == 0 {
			BadRequest(w, "invalid group_id")
			return
		}
		query = query.Where("id = ?", uint(groupID))
	}
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(description) LIKE ?", pattern, pattern, pattern)
	}
	if rawEnabled := strings.TrimSpace(r.URL.Query().Get("enabled")); rawEnabled != "" {
		enabled, parseErr := strconv.ParseBool(rawEnabled)
		if parseErr != nil {
			BadRequest(w, "invalid enabled")
			return
		}
		query = query.Where("is_enabled = ?", enabled)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	groups := make([]model.NodeGroup, 0)
	query = query.Order("name asc, id asc")
	if paged {
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Find(&groups).Error; err != nil {
		ServerError(w, err)
		return
	}
	groupIDs := make([]uint, 0, len(groups))
	groupByID := make(map[uint]*model.NodeGroup, len(groups))
	for index := range groups {
		groupIDs = append(groupIDs, groups[index].ID)
		groupByID[groups[index].ID] = &groups[index]
	}
	endpointCounts := make(map[uint]int64, len(groupIDs))
	if len(groupIDs) > 0 {
		type endpointCountRow struct {
			NodeGroupID uint
			Count       int64
		}
		endpointRows := make([]endpointCountRow, 0, len(groupIDs))
		if err := h.db.Model(&model.NodeGroupEndpoint{}).
			Select("node_group_id, COUNT(*) AS count").
			Where("node_group_id IN ?", groupIDs).
			Group("node_group_id").
			Scan(&endpointRows).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, count := range endpointRows {
			endpointCounts[count.NodeGroupID] = count.Count
		}
		if !paged {
			links := make([]model.NodeGroupEndpoint, 0)
			if err := h.db.Where("node_group_id IN ?", groupIDs).Order("node_group_id asc, sort_order asc, id asc").Find(&links).Error; err != nil {
				ServerError(w, err)
				return
			}
			for _, link := range links {
				groupByID[link.NodeGroupID].ProtocolEndpointIDs = append(groupByID[link.NodeGroupID].ProtocolEndpointIDs, link.ProtocolEndpointID)
			}
		}
		type planCountRow struct {
			NodeGroupID uint
			Count       int64
		}
		planRows := make([]planCountRow, 0, len(groupIDs))
		if err := h.db.Model(&model.Plan{}).Select("node_group_id, COUNT(*) AS count").Where("node_group_id IN ?", groupIDs).Group("node_group_id").Scan(&planRows).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, count := range planRows {
			groupByID[count.NodeGroupID].PlanCount = count.Count
		}
	}
	if paged {
		items := make([]nodeGroupSummaryItem, 0, len(groups))
		for _, group := range groups {
			items = append(items, nodeGroupSummaryItem{
				ID:                    group.ID,
				Name:                  group.Name,
				Code:                  group.Code,
				Description:           group.Description,
				IsEnabled:             group.IsEnabled,
				Revision:              group.Revision,
				ProtocolEndpointCount: endpointCounts[group.ID],
				PlanCount:             group.PlanCount,
				CreatedAt:             group.CreatedAt,
				UpdatedAt:             group.UpdatedAt,
			})
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	OK(w, groups)
}

type nodeGroupSummaryItem struct {
	ID                    uint      `json:"id"`
	Name                  string    `json:"name"`
	Code                  string    `json:"code"`
	Description           string    `json:"description"`
	IsEnabled             bool      `json:"is_enabled"`
	Revision              uint64    `json:"revision"`
	ProtocolEndpointCount int64     `json:"protocol_endpoint_count"`
	PlanCount             int64     `json:"plan_count"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func (h *handlers) NodeGroupDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/node-groups/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var group model.NodeGroup
	if err := h.db.First(&group, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.NodeGroupEndpoint{}).
		Where("node_group_id = ?", group.ID).
		Order("sort_order asc, id asc").
		Pluck("protocol_endpoint_id", &group.ProtocolEndpointIDs).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Plan{}).Where("node_group_id = ?", group.ID).Count(&group.PlanCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, group)
}

func (h *handlers) NodeGroupCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req nodeGroupCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	fields := map[string]string{}
	if req.Name == "" {
		fields["name"] = "请输入节点组名称。"
	}
	if req.Code == "" {
		fields["code"] = "请输入节点组代码。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "节点组信息校验失败。", fields)
		return
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}
	group := model.NodeGroup{Name: req.Name, Code: req.Code, Description: strings.TrimSpace(req.Description), IsEnabled: isEnabled, Revision: 1}
	var reconcileTask model.Task
	endpointIDs := uniqueUintIDs(req.ProtocolEndpointIDs)
	if isEnabled && len(endpointIDs) == 0 {
		BadRequestFields(w, "节点组信息校验失败。", map[string]string{"protocol_endpoint_ids": "启用的节点组至少需要一个可用协议端点。"})
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		if err := replaceNodeGroupEndpoints(tx, group.ID, endpointIDs); err != nil {
			return err
		}
		if err := createAuditLog(tx, claims, "node_group.create", fmt.Sprintf("node_group:%d", group.ID), fmt.Sprintf("endpoint_count=%d", len(endpointIDs))); err != nil {
			return err
		}
		targets, err := h.nodeGroupCredentialPublishTargets(tx, group.ID, endpointIDs)
		if err != nil {
			return err
		}
		task, items, err := prepareNodeGroupReconcileTask(claims, group.ID, group.Revision, targets)
		if err != nil {
			return err
		}
		reconcileTask = task
		return persistAdminTaskRecords(tx, claims, &reconcileTask, items)
	}); err != nil {
		if isDuplicateError(err) {
			BadRequestFields(w, "节点组信息校验失败。", map[string]string{"code": "节点组代码已存在，请更换后重试。"})
			return
		}
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		ServerError(w, err)
		return
	}
	group.ProtocolEndpointIDs = endpointIDs
	_ = h.startPersistedAdminTask(&reconcileTask)
	OK(w, nodeGroupMutationResponse{NodeGroup: group, ReconcileTask: &reconcileTask})
}

func (h *handlers) NodeGroupUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/node-groups/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req nodeGroupUpdateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	var group model.NodeGroup
	if err := h.db.First(&group, id).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			ServerError(w, err)
			return
		}
		NotFound(w)
		return
	}
	if req.ExpectedRevision == nil {
		writeJSON(w, http.StatusPreconditionRequired, "保存节点组前需要提供当前版本号。", map[string]interface{}{"current_revision": group.Revision})
		return
	}
	updates := map[string]interface{}{}
	fields := map[string]string{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			fields["name"] = "请输入节点组名称。"
		} else {
			updates["name"] = name
		}
	}
	if req.Code != nil {
		code := strings.ToLower(strings.TrimSpace(*req.Code))
		if code == "" {
			fields["code"] = "请输入节点组代码。"
		} else {
			updates["code"] = code
		}
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.IsEnabled != nil {
		updates["is_enabled"] = *req.IsEnabled
	}
	if len(updates) == 0 && req.ProtocolEndpointIDs == nil {
		if len(fields) > 0 {
			BadRequestFields(w, "节点组信息校验失败。", fields)
			return
		}
		BadRequest(w, "no valid update fields")
		return
	}
	if len(fields) > 0 {
		BadRequestFields(w, "节点组信息校验失败。", fields)
		return
	}
	currentRevision := group.Revision
	var reconcileTask model.Task
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var locked model.NodeGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, id).Error; err != nil {
			return err
		}
		currentRevision = locked.Revision
		if locked.Revision != *req.ExpectedRevision {
			return errNodeGroupRevisionConflict
		}
		targetEnabled := locked.IsEnabled
		if req.IsEnabled != nil {
			targetEnabled = *req.IsEnabled
			if !targetEnabled {
				var activePlans int64
				if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", locked.ID, true).Count(&activePlans).Error; err != nil {
					return err
				}
				if activePlans > 0 {
					return validationError("节点组状态校验失败。", map[string]string{"is_enabled": "请先停用使用该节点组的已发布套餐。"})
				}
			}
		}
		endpointIDs := []uint(nil)
		if req.ProtocolEndpointIDs != nil {
			endpointIDs = uniqueUintIDs(*req.ProtocolEndpointIDs)
			if len(endpointIDs) == 0 && targetEnabled {
				return validationError("节点组成员校验失败。", map[string]string{"protocol_endpoint_ids": "启用的节点组至少需要一个可用协议端点。"})
			}
			if len(endpointIDs) == 0 {
				var activePlans int64
				if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", locked.ID, true).Count(&activePlans).Error; err != nil {
					return err
				}
				if activePlans > 0 {
					return validationError("节点组成员校验失败。", map[string]string{"protocol_endpoint_ids": "已发布套餐使用的节点组必须保留至少一个可用协议端点。"})
				}
			}
		} else if targetEnabled && !locked.IsEnabled {
			var activeEndpoints int64
			if err := tx.Model(&model.NodeGroupEndpoint{}).
				Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
				Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", locked.ID, true).
				Count(&activeEndpoints).Error; err != nil {
				return err
			}
			if activeEndpoints == 0 {
				return validationError("节点组成员校验失败。", map[string]string{"protocol_endpoint_ids": "启用节点组前请至少加入一个可用协议端点。"})
			}
		}
		updates["revision"] = locked.Revision + 1
		if len(updates) > 0 {
			if err := tx.Model(&locked).Updates(updates).Error; err != nil {
				return err
			}
		}
		if req.ProtocolEndpointIDs != nil {
			var existingLinks []model.NodeGroupEndpoint
			if err := tx.Where("node_group_id = ?", locked.ID).Find(&existingLinks).Error; err != nil {
				return err
			}
			changedEndpointIDs := nodeGroupMembershipChangedEndpointIDs(existingLinks, endpointIDs)
			if err := replaceNodeGroupEndpoints(tx, locked.ID, endpointIDs); err != nil {
				return err
			}
			targets, err := h.nodeGroupCredentialPublishTargets(tx, locked.ID, changedEndpointIDs)
			if err != nil {
				return err
			}
			task, items, err := prepareNodeGroupReconcileTask(claims, locked.ID, locked.Revision+1, targets)
			if err != nil {
				return err
			}
			reconcileTask = task
			if err := persistAdminTaskRecords(tx, claims, &reconcileTask, items); err != nil {
				return err
			}
		}
		detail := fmt.Sprintf("revision=%d membership_updated=%t", locked.Revision+1, req.ProtocolEndpointIDs != nil)
		if req.ProtocolEndpointIDs != nil {
			detail += fmt.Sprintf(" endpoint_count=%d", len(endpointIDs))
		}
		return createAuditLog(tx, claims, "node_group.update", fmt.Sprintf("node_group:%d", locked.ID), detail)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(err, errNodeGroupRevisionConflict) {
			writeJSON(w, http.StatusConflict, "节点组已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
			return
		}
		if isDuplicateError(err) {
			BadRequestFields(w, "节点组信息校验失败。", map[string]string{"code": "节点组代码已存在，请更换后重试。"})
			return
		}
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.db.First(&group, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Order("sort_order asc, id asc").Pluck("protocol_endpoint_id", &group.ProtocolEndpointIDs).Error; err != nil {
		ServerError(w, err)
		return
	}
	var responseTask *model.Task
	if reconcileTask.ID > 0 {
		_ = h.startPersistedAdminTask(&reconcileTask)
		responseTask = &reconcileTask
	}
	OK(w, nodeGroupMutationResponse{NodeGroup: group, ReconcileTask: responseTask})
}

var errNodeGroupRevisionConflict = errors.New("node group revision conflict")

func replaceNodeGroupEndpoints(tx *gorm.DB, nodeGroupID uint, endpointIDs []uint) error {
	endpointIDs = uniqueUintIDs(endpointIDs)
	activeIDs := make([]uint, 0, len(endpointIDs))
	for start := 0; start < len(endpointIDs); start += 500 {
		end := start + 500
		if end > len(endpointIDs) {
			end = len(endpointIDs)
		}
		var batch []uint
		if err := tx.Model(&model.ProtocolEndpoint{}).
			Where("id IN ? AND is_active = ?", endpointIDs[start:end], true).
			Pluck("id", &batch).Error; err != nil {
			return err
		}
		activeIDs = append(activeIDs, batch...)
	}
	if missingID, missing := firstMissingUintID(endpointIDs, activeIDs); missing {
		return validationError("节点组成员校验失败。", map[string]string{"protocol_endpoint_ids": fmt.Sprintf("协议端点 #%d 不存在或已停用，请重新选择。", missingID)})
	}

	var existing []model.NodeGroupEndpoint
	if err := tx.Where("node_group_id = ?", nodeGroupID).Find(&existing).Error; err != nil {
		return err
	}
	desired := make(map[uint]struct{}, len(endpointIDs))
	for _, endpointID := range endpointIDs {
		desired[endpointID] = struct{}{}
	}
	removed := make([]uint, 0)
	for _, link := range existing {
		if _, keep := desired[link.ProtocolEndpointID]; !keep {
			removed = append(removed, link.ProtocolEndpointID)
		}
	}
	for start := 0; start < len(removed); start += 500 {
		end := start + 500
		if end > len(removed) {
			end = len(removed)
		}
		if err := tx.Where("node_group_id = ? AND protocol_endpoint_id IN ?", nodeGroupID, removed[start:end]).
			Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
			return err
		}
	}
	links := make([]model.NodeGroupEndpoint, 0, len(endpointIDs))
	for index, endpointID := range endpointIDs {
		links = append(links, model.NodeGroupEndpoint{NodeGroupID: nodeGroupID, ProtocolEndpointID: endpointID, SortOrder: index})
	}
	if len(links) > 0 {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_group_id"}, {Name: "protocol_endpoint_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"sort_order"}),
		}).CreateInBatches(&links, 500).Error; err != nil {
			return err
		}
	}
	return nil
}

func firstMissingUintID(requested, existing []uint) (uint, bool) {
	available := make(map[uint]struct{}, len(existing))
	for _, id := range existing {
		available[id] = struct{}{}
	}
	for _, id := range requested {
		if _, ok := available[id]; !ok {
			return id, true
		}
	}
	return 0, false
}

type planNodeGroupSummary struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	IsEnabled bool   `json:"is_enabled"`
}

type planSummaryItem struct {
	ID             uint                  `json:"id"`
	Name           string                `json:"name"`
	Slug           string                `json:"slug"`
	Summary        string                `json:"summary"`
	NodeGroupID    uint                  `json:"node_group_id"`
	NodeGroup      *planNodeGroupSummary `json:"node_group,omitempty"`
	IsActive       bool                  `json:"is_active"`
	SortOrder      int                   `json:"sort_order"`
	Revision       uint64                `json:"revision"`
	SKUCount       int64                 `json:"sku_count"`
	ActiveSKUCount int64                 `json:"active_sku_count"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

type planDetailItem struct {
	planSummaryItem
	Description            string `json:"description"`
	TrafficBytes           int64  `json:"traffic_bytes"`
	SpeedLimitMbps         int    `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int    `json:"max_active_subscriptions"`
	IsRenewable            bool   `json:"is_renewable"`
	DeviceLimit            int    `json:"device_limit"`
	FamilyLimit            int    `json:"family_limit"`
	ResetPolicy            int16  `json:"reset_policy"`
	TrafficCalcMode        int16  `json:"traffic_calc_mode"`
}

type planCatalogItem struct {
	planSummaryItem
	TrafficBytes   int64          `json:"traffic_bytes"`
	SpeedLimitMbps int            `json:"speed_limit_mbps"`
	DeviceLimit    int            `json:"device_limit"`
	PrimarySKU     *model.PlanSKU `json:"primary_sku,omitempty"`
}

type planSKUCountRow struct {
	PlanID         uint  `gorm:"column:plan_id"`
	SKUCount       int64 `gorm:"column:sku_count"`
	ActiveSKUCount int64 `gorm:"column:active_sku_count"`
}

func newPlanNodeGroupSummary(group *model.NodeGroup) *planNodeGroupSummary {
	if group == nil {
		return nil
	}
	return &planNodeGroupSummary{
		ID: group.ID, Name: group.Name, Code: group.Code, IsEnabled: group.IsEnabled,
	}
}

func newPlanSummaryItem(plan model.Plan, counts planSKUCountRow) planSummaryItem {
	return planSummaryItem{
		ID: plan.ID, Name: plan.Name, Slug: plan.Slug, Summary: plan.Summary,
		NodeGroupID: plan.NodeGroupID, NodeGroup: newPlanNodeGroupSummary(plan.NodeGroup),
		IsActive: plan.IsActive, SortOrder: plan.SortOrder, Revision: plan.Revision,
		SKUCount: counts.SKUCount, ActiveSKUCount: counts.ActiveSKUCount,
		CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt,
	}
}

func newPlanDetailItem(plan model.Plan, counts planSKUCountRow) planDetailItem {
	return planDetailItem{
		planSummaryItem:        newPlanSummaryItem(plan, counts),
		Description:            plan.Description,
		TrafficBytes:           plan.TrafficBytes,
		SpeedLimitMbps:         plan.SpeedLimitMbps,
		MaxActiveSubscriptions: plan.MaxActiveSubscriptions,
		IsRenewable:            plan.IsRenewable,
		DeviceLimit:            plan.DeviceLimit,
		FamilyLimit:            plan.FamilyLimit,
		ResetPolicy:            plan.ResetPolicy,
		TrafficCalcMode:        plan.TrafficCalcMode,
	}
}

func newPlanCatalogItem(plan model.Plan, counts planSKUCountRow, primarySKU *model.PlanSKU) planCatalogItem {
	return planCatalogItem{
		planSummaryItem: newPlanSummaryItem(plan, counts),
		TrafficBytes:    plan.TrafficBytes,
		SpeedLimitMbps:  plan.SpeedLimitMbps,
		DeviceLimit:     plan.DeviceLimit,
		PrimarySKU:      primarySKU,
	}
}

func loadPlanSKUCounts(db *gorm.DB, planIDs []uint) (map[uint]planSKUCountRow, error) {
	counts := make(map[uint]planSKUCountRow, len(planIDs))
	if len(planIDs) == 0 {
		return counts, nil
	}
	rows := make([]planSKUCountRow, 0, len(planIDs))
	if err := db.Model(&model.PlanSKU{}).
		Select("plan_id, COUNT(*) AS sku_count, SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END) AS active_sku_count").
		Where("plan_id IN ?", planIDs).
		Group("plan_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.PlanID] = row
	}
	return counts, nil
}

func loadPrimaryPlanSKUs(db *gorm.DB, planIDs []uint) (map[uint]model.PlanSKU, error) {
	items := make(map[uint]model.PlanSKU, len(planIDs))
	if len(planIDs) == 0 {
		return items, nil
	}
	rows := make([]model.PlanSKU, 0, len(planIDs))
	if err := db.Table("plan_skus AS candidate").
		Where("candidate.plan_id IN ?", planIDs).
		Where("candidate.is_active = ?", true).
		Where("candidate.sku_type = ?", "new").
		Where(`NOT EXISTS (
			SELECT 1
			FROM plan_skus AS earlier
			WHERE earlier.plan_id = candidate.plan_id
			  AND earlier.is_active = 1
			  AND earlier.sku_type = 'new'
			  AND (
			    earlier.price_cents < candidate.price_cents
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order < candidate.sort_order)
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order = candidate.sort_order AND earlier.id < candidate.id)
			  )
		)`).
		Order("candidate.plan_id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		items[row.PlanID] = row
	}
	return items, nil
}

func (h *handlers) PlanListHandler(w http.ResponseWriter, r *http.Request) {
	plans := make([]model.Plan, 0)
	claims, claimErr := h.authFromRequest(r)
	isAdmin := claimErr == nil && claims.IsAdmin
	paged := r.URL.Query().Get("paged") == "true"
	offset, limit := 0, 50
	var err error
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	query := h.db.Model(&model.Plan{}).Order("sort_order asc, id desc")

	if !isAdmin {
		query = query.Where("is_active = 1")
	} else if parseBoolQuery(r.URL.Query().Get("include_inactive")) {
		// admin can view inactive plans for management.
	} else {
		query = query.Where("is_active = 1")
	}
	if paged {
		if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
			if len(search) > 128 {
				BadRequest(w, "q must not exceed 128 bytes")
				return
			}
			pattern := "%" + search + "%"
			query = query.Where("LOWER(plans.name) LIKE ? OR LOWER(plans.slug) LIKE ? OR LOWER(plans.summary) LIKE ?", pattern, pattern, pattern)
		}
	}
	if isAdmin {
		if rawActive := strings.TrimSpace(r.URL.Query().Get("active")); rawActive != "" {
			active, parseErr := strconv.ParseBool(rawActive)
			if parseErr != nil {
				BadRequest(w, "invalid active")
				return
			}
			query = query.Where("plans.is_active = ?", active)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	if paged {
		query = query.Offset(offset).Limit(limit)
	}
	if paged {
		query = query.Preload("NodeGroup")
	} else {
		query = query.Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			if !isAdmin {
				db = db.Where("is_active = ?", true)
			}
			return db.Order("sort_order asc, id asc")
		}).Preload("NodeGroup")
	}
	if err := query.Find(&plans).Error; err != nil {
		ServerError(w, err)
		return
	}
	if paged {
		planIDs := make([]uint, 0, len(plans))
		for _, plan := range plans {
			planIDs = append(planIDs, plan.ID)
		}
		counts, err := loadPlanSKUCounts(h.db, planIDs)
		if err != nil {
			ServerError(w, err)
			return
		}
		if isAdmin {
			items := make([]planSummaryItem, 0, len(plans))
			for _, plan := range plans {
				items = append(items, newPlanSummaryItem(plan, counts[plan.ID]))
			}
			OK(w, pagedData(items, total, offset, limit))
			return
		}
		primarySKUs, err := loadPrimaryPlanSKUs(h.db, planIDs)
		if err != nil {
			ServerError(w, err)
			return
		}
		items := make([]planCatalogItem, 0, len(plans))
		for _, plan := range plans {
			count := counts[plan.ID]
			count.SKUCount = count.ActiveSKUCount
			var primarySKU *model.PlanSKU
			if item, ok := primarySKUs[plan.ID]; ok {
				itemCopy := item
				primarySKU = &itemCopy
			}
			items = append(items, newPlanCatalogItem(plan, count, primarySKU))
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	OK(w, plans)
}

func (h *handlers) PublicPlanDetailHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var plan model.Plan
	if err := h.db.Preload("NodeGroup").Where("is_active = ?", true).First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	counts, err := loadPlanSKUCounts(h.db, []uint{plan.ID})
	if err != nil {
		ServerError(w, err)
		return
	}
	count := counts[plan.ID]
	count.SKUCount = count.ActiveSKUCount
	primarySKUs, err := loadPrimaryPlanSKUs(h.db, []uint{plan.ID})
	if err != nil {
		ServerError(w, err)
		return
	}
	var primarySKU *model.PlanSKU
	if item, ok := primarySKUs[plan.ID]; ok {
		itemCopy := item
		primarySKU = &itemCopy
	}
	OK(w, newPlanCatalogItem(plan, count, primarySKU))
}

func (h *handlers) PlanDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var plan model.Plan
	if err := h.db.
		Preload("NodeGroup").
		First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	counts, err := loadPlanSKUCounts(h.db, []uint{plan.ID})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, newPlanDetailItem(plan, counts[plan.ID]))
}

func (h *handlers) PlanCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req planCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	fields := make(map[string]string)
	if req.Name == "" {
		fields["name"] = "请输入商品名称。"
	}
	if req.Slug == "" {
		fields["slug"] = "请输入商品 Slug。"
	}
	if len(req.SKUs) == 0 {
		fields["skus"] = "请至少配置一个销售规格。"
	}
	if req.NodeGroupID == 0 {
		fields["node_group_id"] = "请选择节点组。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "商品信息校验失败。", fields)
		return
	}

	policy, err := normalizePlanPolicy(req, req.SKUs[0])
	if err != nil {
		BadRequestError(w, err)
		return
	}
	plan := model.Plan{
		Name: req.Name, Slug: req.Slug, Summary: strings.TrimSpace(req.Summary),
		Description: strings.TrimSpace(req.Description), IsActive: req.IsActive, SortOrder: req.SortOrder, Revision: 1,
		TrafficBytes: policy.TrafficBytes, SpeedLimitMbps: policy.SpeedLimitMbps,
		MaxActiveSubscriptions: policy.MaxActiveSubscriptions, IsRenewable: policy.IsRenewable,
		DeviceLimit: policy.DeviceLimit, FamilyLimit: policy.FamilyLimit,
		ResetPolicy: policy.ResetPolicy, TrafficCalcMode: policy.TrafficCalcMode,
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var group model.NodeGroup
		if err := tx.First(&group, req.NodeGroupID).Error; err != nil {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "所选节点组不存在。"})
		}
		if plan.IsActive && !group.IsEnabled {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品必须选择已启用的节点组。"})
		}
		plan.NodeGroupID = group.ID
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		for index, skuReq := range req.SKUs {
			sku, err := buildPlanSKU(plan.ID, skuReq)
			if err != nil {
				return prefixValidationError(err, fmt.Sprintf("skus.%d.", index))
			}
			if err := tx.Create(&sku).Error; err != nil {
				return err
			}
		}
		if req.IsActive {
			var activeSKUCount int64
			if err := tx.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", plan.ID, true).Count(&activeSKUCount).Error; err != nil || activeSKUCount == 0 {
				return validationError("商品信息校验失败。", map[string]string{"skus": "已发布商品至少需要一个可售 SKU。"})
			}
		}
		if plan.IsActive {
			var endpointCount int64
			if err := tx.Model(&model.NodeGroupEndpoint{}).
				Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
				Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", plan.NodeGroupID, true).
				Count(&endpointCount).Error; err != nil || endpointCount == 0 {
				return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品的节点组至少需要一个已启用协议端点。"})
			}
		}
		return createAuditLog(tx, claims, "plan.create", fmt.Sprintf("plan:%d", plan.ID), fmt.Sprintf("skus=%d node_group=%d", len(req.SKUs), plan.NodeGroupID))
	})
	if err != nil {
		BadRequestError(w, err)
		return
	}
	_ = h.db.Preload("SKUs", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order asc, id asc") }).First(&plan, plan.ID).Error
	OK(w, plan)
}

func (h *handlers) PlanUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var req planUpdateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	var plan model.Plan
	if err := h.db.Preload("SKUs").First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if req.ExpectedRevision == nil {
		writeJSON(w, http.StatusPreconditionRequired, "保存商品前需要提供当前版本号。", map[string]interface{}{"current_revision": plan.Revision})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"name": "请输入商品名称。"})
			return
		}
		updates["name"] = name
	}
	if req.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*req.Slug))
		if slug == "" {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"slug": "请输入商品 Slug。"})
			return
		}
		updates["slug"] = slug
	}
	if req.Summary != nil {
		updates["summary"] = strings.TrimSpace(*req.Summary)
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.NodeGroupID != nil {
		if *req.NodeGroupID == 0 {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"node_group_id": "请选择节点组。"})
			return
		}
		var group model.NodeGroup
		if err := h.db.First(&group, *req.NodeGroupID).Error; err != nil {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"node_group_id": "所选节点组不存在。"})
			return
		}
		updates["node_group_id"] = *req.NodeGroupID
	}
	if req.TrafficBytes != nil {
		if *req.TrafficBytes <= 0 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"traffic_bytes": "流量配额必须大于 0。"})
			return
		}
		updates["traffic_bytes"] = *req.TrafficBytes
	}
	if req.SpeedLimitMbps != nil {
		if *req.SpeedLimitMbps < 0 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"speed_limit_mbps": "速率限制不能小于 0。"})
			return
		}
		updates["speed_limit_mbps"] = *req.SpeedLimitMbps
	}
	if req.MaxActiveSubscriptions != nil {
		if *req.MaxActiveSubscriptions < 0 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"max_active_subscriptions": "最大有效订阅数不能小于 0。"})
			return
		}
		updates["max_active_subscriptions"] = *req.MaxActiveSubscriptions
	}
	if req.IsRenewable != nil {
		updates["is_renewable"] = *req.IsRenewable
	}
	if req.DeviceLimit != nil {
		if *req.DeviceLimit <= 0 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"device_limit": "设备数必须大于 0。"})
			return
		}
		updates["device_limit"] = *req.DeviceLimit
	}
	if req.FamilyLimit != nil {
		if *req.FamilyLimit < 0 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"family_limit": "家庭共享人数不能小于 0。"})
			return
		}
		updates["family_limit"] = *req.FamilyLimit
	}
	if req.ResetPolicy != nil {
		if *req.ResetPolicy < 0 || *req.ResetPolicy > 5 {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"reset_policy": "请选择有效的流量重置策略。"})
			return
		}
		updates["reset_policy"] = *req.ResetPolicy
	}
	if req.TrafficCalcMode != nil {
		if !validTrafficCalcMode(*req.TrafficCalcMode) {
			BadRequestFields(w, "套餐策略校验失败。", map[string]string{"traffic_calc_mode": "请选择有效的流量计算方式。"})
			return
		}
		updates["traffic_calc_mode"] = *req.TrafficCalcMode
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if len(updates) == 0 {
		BadRequest(w, "no valid update fields")
		return
	}

	targetActive := plan.IsActive
	if req.IsActive != nil {
		targetActive = *req.IsActive
	}
	targetNodeGroupID := plan.NodeGroupID
	if req.NodeGroupID != nil {
		targetNodeGroupID = *req.NodeGroupID
	}
	if targetActive && (req.IsActive != nil || req.NodeGroupID != nil) {
		var group model.NodeGroup
		if err := h.db.Where("id = ? AND is_enabled = ?", targetNodeGroupID, true).First(&group).Error; err != nil {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"node_group_id": "已发布商品必须选择已启用的节点组。"})
			return
		}
		var endpointCount int64
		if err := h.db.Model(&model.NodeGroupEndpoint{}).
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
			Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", targetNodeGroupID, true).
			Count(&endpointCount).Error; err != nil || endpointCount == 0 {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{"node_group_id": "已发布商品的节点组至少需要一个已启用协议端点。"})
			return
		}
		var activeSKUCount int64
		if err := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", id, true).Count(&activeSKUCount).Error; err != nil || activeSKUCount == 0 {
			BadRequest(w, "an active plan must have at least one active sku")
			return
		}
	}

	currentRevision := plan.Revision
	updates["revision"] = gorm.Expr("revision + 1")
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Plan{}).Where("id = ? AND revision = ?", plan.ID, *req.ExpectedRevision).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var latest struct{ Revision uint64 }
			if err := tx.Model(&model.Plan{}).Select("revision").Where("id = ?", plan.ID).Scan(&latest).Error; err != nil {
				return err
			}
			currentRevision = latest.Revision
			return errPlanRevisionConflict
		}
		currentRevision = *req.ExpectedRevision + 1
		return createAuditLog(tx, claims, "plan.update", fmt.Sprintf("plan:%d", plan.ID), fmt.Sprintf("fields=%d node_group=%d revision=%d", len(updates)-1, targetNodeGroupID, currentRevision))
	}); err != nil {
		if errors.Is(err, errPlanRevisionConflict) {
			writeJSON(w, http.StatusConflict, "商品已被其他会话更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
			return
		}
		BadRequest(w, err.Error())
		return
	}

	if err := h.db.First(&plan, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, plan)
}

var errPlanRevisionConflict = errors.New("plan revision conflict")

type normalizedPlanPolicy struct {
	TrafficBytes           int64
	SpeedLimitMbps         int
	MaxActiveSubscriptions int
	IsRenewable            bool
	DeviceLimit            int
	FamilyLimit            int
	ResetPolicy            int16
	TrafficCalcMode        int16
}

func normalizePlanPolicy(req planCreateReq, firstSKU planSKUReq) (normalizedPlanPolicy, error) {
	_ = firstSKU // Kept in the compatibility signature; Plan is authoritative.
	policy := normalizedPlanPolicy{
		TrafficBytes: req.TrafficBytes, SpeedLimitMbps: req.SpeedLimitMbps,
		MaxActiveSubscriptions: req.MaxActiveSubscriptions, DeviceLimit: req.DeviceLimit,
		FamilyLimit: req.FamilyLimit, ResetPolicy: req.ResetPolicy,
		TrafficCalcMode: req.TrafficCalcMode,
		IsRenewable:     true,
	}
	if req.IsRenewable != nil {
		policy.IsRenewable = *req.IsRenewable
	}
	fields := make(map[string]string)
	if policy.TrafficBytes <= 0 {
		fields["traffic_bytes"] = "流量配额必须大于 0。"
	}
	if policy.DeviceLimit <= 0 {
		fields["device_limit"] = "设备数必须大于 0。"
	}
	if policy.SpeedLimitMbps < 0 {
		fields["speed_limit_mbps"] = "速率限制不能小于 0。"
	}
	if policy.MaxActiveSubscriptions < 0 {
		fields["max_active_subscriptions"] = "最大有效订阅数不能小于 0。"
	}
	if policy.FamilyLimit < 0 {
		fields["family_limit"] = "家庭共享人数不能小于 0。"
	}
	if policy.ResetPolicy < 0 || policy.ResetPolicy > 5 {
		fields["reset_policy"] = "请选择有效的流量重置策略。"
	}
	if !validTrafficCalcMode(policy.TrafficCalcMode) {
		fields["traffic_calc_mode"] = "请选择有效的流量计算方式。"
	}
	if len(fields) > 0 {
		return normalizedPlanPolicy{}, validationError("套餐策略校验失败。", fields)
	}
	return policy, nil
}

func validTrafficCalcMode(mode int16) bool {
	return mode == trafficCalcBoth || mode == trafficCalcUpload || mode == trafficCalcDownload
}

func buildPlanSKU(planID uint, req planSKUReq) (model.PlanSKU, error) {
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	req.Name = strings.TrimSpace(req.Name)
	req.SKUType = strings.ToLower(strings.TrimSpace(req.SKUType))
	if req.SKUType == "" {
		req.SKUType = "new"
	}
	req.BillingUnit = strings.ToLower(strings.TrimSpace(req.BillingUnit))
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	fields := make(map[string]string)
	if req.Code == "" {
		fields["code"] = "请输入 SKU 编码。"
	}
	if req.Name == "" {
		fields["name"] = "请输入规格名称。"
	}
	if req.Currency == "" {
		fields["currency"] = "请输入币种。"
	}
	switch req.SKUType {
	case "new", "renewal", "upgrade", "traffic_pack":
	default:
		fields["sku_type"] = "请选择有效的规格类型。"
	}
	switch req.BillingUnit {
	case "day", "month", "year", "once":
		if req.BillingValue <= 0 {
			fields["billing_value"] = "周期数量必须大于 0。"
		}
	default:
		fields["billing_unit"] = "请选择有效的计费单位。"
	}
	if req.PriceCents < 0 {
		fields["price_cents"] = "价格不能小于 0。"
	}
	if req.SKUType == "traffic_pack" {
		if req.TrafficBytes <= 0 {
			fields["grant_traffic_bytes"] = "流量包的附加流量必须大于 0。"
		}
		if req.DeviceLimit != 0 || req.SpeedLimitMbps != 0 {
			fields["entitlements"] = "流量包只能增加流量，不能修改设备数或限速。"
		}
	} else if req.TrafficBytes != 0 || req.DeviceLimit != 0 || req.SpeedLimitMbps != 0 {
		fields["entitlements"] = "周期规格继承商品权益，不能单独配置流量、设备数或限速。"
	}
	if len(fields) > 0 {
		return model.PlanSKU{}, validationError("销售规格校验失败。", fields)
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	return model.PlanSKU{
		PlanID: planID, Code: req.Code, Name: req.Name, SKUType: req.SKUType,
		BillingUnit: req.BillingUnit, BillingValue: req.BillingValue,
		PriceCents: req.PriceCents, Currency: req.Currency, TrafficBytes: req.TrafficBytes,
		DeviceLimit: req.DeviceLimit, SpeedLimitMbps: req.SpeedLimitMbps,
		IsActive: isActive, SortOrder: req.SortOrder,
	}, nil
}

func prefixValidationError(err error, prefix string) error {
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		return err
	}
	fields := make(map[string]string, len(validation.fields))
	for name, message := range validation.fields {
		fields[prefix+name] = message
	}
	return validationError(validation.message, fields)
}

func uniqueUintIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (h *handlers) PlanSKUListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var planCount int64
	if err := h.db.Model(&model.Plan{}).Where("id = ?", planID).Count(&planCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if planCount == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.PlanSKU{}).Where("plan_id = ?", planID)
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		if len(search) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		pattern := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(currency) LIKE ?", pattern, pattern, pattern)
	}
	if rawActive := strings.TrimSpace(r.URL.Query().Get("active")); rawActive != "" {
		active, parseErr := strconv.ParseBool(rawActive)
		if parseErr != nil {
			BadRequest(w, "invalid active")
			return
		}
		query = query.Where("is_active = ?", active)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.PlanSKU, 0)
	if err := query.Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) PublicPlanSKUListHandler(w http.ResponseWriter, r *http.Request) {
	planID, err := parsePathID(r.URL.Path, "/api/v1/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var planCount int64
	if err := h.db.Model(&model.Plan{}).Where("id = ? AND is_active = ?", planID, true).Count(&planCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if planCount == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", planID, true)
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		if len(search) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		pattern := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(currency) LIKE ?", pattern, pattern, pattern)
	}
	if skuType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sku_type"))); skuType != "" {
		switch skuType {
		case "new", "renewal", "upgrade", "traffic_pack":
			query = query.Where("sku_type = ?", skuType)
		default:
			BadRequest(w, "invalid sku_type")
			return
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.PlanSKU, 0)
	if err := query.Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) PlanSKUGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var sku model.PlanSKU
	if err := h.db.First(&sku, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, sku)
}

func (h *handlers) PlanSKUCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req planSKUReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	sku, err := buildPlanSKU(planID, req)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		var plan model.Plan
		if err := tx.First(&plan, planID).Error; err != nil {
			return err
		}
		if err := tx.Create(&sku).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plan.sku.create", fmt.Sprintf("plan_sku:%d", sku.ID), fmt.Sprintf("plan=%d code=%s", planID, sku.Code))
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, sku)
}

func (h *handlers) PlanSKUUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req planSKUReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	var existing model.PlanSKU
	if err := h.db.First(&existing, id).Error; err != nil {
		NotFound(w)
		return
	}
	sku, err := buildPlanSKU(existing.PlanID, req)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	sku.ID = existing.ID
	if !sku.IsActive {
		var plan model.Plan
		if err := h.db.First(&plan, existing.PlanID).Error; err != nil {
			ServerError(w, err)
			return
		}
		if plan.IsActive {
			var otherActive int64
			if err := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND id <> ? AND is_active = ?", existing.PlanID, existing.ID, true).Count(&otherActive).Error; err != nil || otherActive == 0 {
				BadRequestFields(w, "销售规格校验失败。", map[string]string{"is_active": "已发布商品必须保留至少一个可售 SKU。"})
				return
			}
		}
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&existing).Updates(map[string]interface{}{
			"code": sku.Code, "name": sku.Name, "sku_type": sku.SKUType, "billing_unit": sku.BillingUnit,
			"billing_value": sku.BillingValue, "price_cents": sku.PriceCents,
			"currency": sku.Currency, "traffic_bytes": sku.TrafficBytes,
			"device_limit": sku.DeviceLimit, "speed_limit_mbps": sku.SpeedLimitMbps,
			"is_active": sku.IsActive, "sort_order": sku.SortOrder,
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plan.sku.update", fmt.Sprintf("plan_sku:%d", sku.ID), sku.Code)
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, sku)
}

func (h *handlers) OrderListHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/orders")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	var orders []model.Order
	query := h.db.Model(&model.Order{})

	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			parsed, parseErr := strconv.ParseUint(target, 10, 64)
			if parseErr != nil || parsed == 0 {
				BadRequest(w, "invalid user_id")
				return
			}
			query = query.Where("orders.user_id = ?", parsed)
		}
		if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
			if len(search) > 128 {
				BadRequest(w, "q must not exceed 128 bytes")
				return
			}
			pattern := "%" + strings.ToLower(search) + "%"
			condition := `LOWER(orders.trade_no) LIKE ? OR LOWER(COALESCE(orders.provider_trade_no, '')) LIKE ?
				OR LOWER(orders.plan_name) LIKE ? OR LOWER(orders.sku_name) LIKE ? OR LOWER(orders.channel) LIKE ?`
			args := []interface{}{pattern, pattern, pattern, pattern, pattern}
			if parsed, parseErr := strconv.ParseUint(search, 10, 64); parseErr == nil && parsed > 0 {
				condition += " OR orders.id = ? OR orders.user_id = ? OR orders.subscription_id = ?"
				args = append(args, parsed, parsed, parsed)
			}
			query = query.Where(condition, args...)
		}
		if orderType := strings.TrimSpace(r.URL.Query().Get("order_type")); orderType != "" {
			if !isValidOrderType(orderType) {
				BadRequest(w, "invalid order_type")
				return
			}
			query = query.Where("orders.order_type = ?", orderType)
		}
	} else {
		query = query.Where("orders.user_id = ?", claims.UserID)
	}

	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		statuses, valid := h.orderListStatusValues(status, adminScope)
		if !valid {
			BadRequest(w, "invalid status")
			return
		}
		query = query.Where("orders.status IN ?", statuses)
	}
	if adminScope {
		window, present, windowErr := parseOptionalDateWindow(r.URL.Query(), "created_from", "created_to", historyMaxWindowDays)
		if windowErr != nil {
			BadRequest(w, windowErr.Error())
			return
		}
		if present {
			query = applyHistoryWindow(query, "orders.created_at", window)
		}
	}

	paged := wantsPagedList(r)
	offset, limit := 0, 50
	var total int64
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Order("orders.id desc").Find(&orders).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := newAdminOrderList(orders)
	if paged {
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	OK(w, items)
}

func (h *handlers) OrderCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var req orderCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.PlanSKUID == 0 {
		BadRequest(w, "plan_sku_id is required")
		return
	}

	var plan model.Plan
	var sku model.PlanSKU
	if err := h.db.Where("is_active = 1").First(&sku, req.PlanSKUID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "plan sku not found")
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.db.Where("is_active = 1").First(&plan, sku.PlanID).Error; err != nil {
		BadRequest(w, "plan is not available")
		return
	}

	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = "manual"
	}
	orderType := strings.ToLower(strings.TrimSpace(req.OrderType))
	if orderType == "" {
		orderType = sku.SKUType
	}
	if orderType == "" {
		orderType = "new"
	}
	switch orderType {
	case "new", "renewal", "upgrade", "traffic_pack":
	default:
		BadRequest(w, "order_type must be new, renewal, upgrade or traffic_pack")
		return
	}
	var targetSubscriptionID *uint
	if req.TargetSubscriptionID != 0 {
		var target model.Subscription
		if err := h.db.Where("id = ? AND user_id = ?", req.TargetSubscriptionID, claims.UserID).First(&target).Error; err != nil {
			BadRequest(w, "target subscription not found")
			return
		}
		targetSubscriptionID = &target.ID
	}
	if (orderType == "renewal" || orderType == "upgrade" || orderType == "traffic_pack") && targetSubscriptionID == nil {
		BadRequest(w, "target_subscription_id is required for this order type")
		return
	}
	trafficBytes := plan.TrafficBytes
	deviceLimit := plan.DeviceLimit
	speedLimitMbps := plan.SpeedLimitMbps
	if orderType == "traffic_pack" {
		trafficBytes = sku.TrafficBytes
		deviceLimit = 0
		speedLimitMbps = 0
	}
	order := model.Order{
		UserID: claims.UserID, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: uuid.NewString(), OrderType: orderType, TargetSubscriptionID: targetSubscriptionID,
		AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents, Currency: sku.Currency,
		Channel: channel, Status: orderStatusPending,
		PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit,
		BillingValue: sku.BillingValue, RenewalEffect: sku.RenewalEffect, TrafficBytes: trafficBytes,
		DeviceLimit: deviceLimit, SpeedLimitMbps: speedLimitMbps,
	}
	if err := h.db.Create(&order).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

func (h *handlers) OrderPayHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	orderID, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var order model.Order
	if err := h.db.First(&order, orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "order not found")
			return
		}
		ServerError(w, err)
		return
	}

	force := parseBoolQuery(r.URL.Query().Get("force"))

	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, orderID).Error; err != nil {
			return err
		}
		if !orderTransitionAllowed(order.Status, orderStatusPaid, force) {
			return errOrderNotPayable
		}
		previousStatus := order.Status
		if err := h.setOrderPaid(tx, &order, time.Now().UTC()); err != nil {
			return err
		}
		if previousStatus != order.Status {
			return createAuditLog(tx, claims, "order.pay", fmt.Sprintf("order:%d", order.ID), previousStatus+"->"+order.Status)
		}
		return nil
	})
	if errors.Is(err, errOrderNotPayable) {
		BadRequest(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if order.SubscriptionID != 0 && order.Status == orderStatusPaid {
		h.scheduleSubscriptionConfigPublishes(order.SubscriptionID, claims.UserID)
	}
	OK(w, order)
}

func (h *handlers) OrderCancelHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/orders/")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	orderID, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var order model.Order
	if err := h.db.First(&order, orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "order not found")
			return
		}
		ServerError(w, err)
		return
	}

	if !adminScope && order.UserID != claims.UserID {
		Forbidden(w, "no permission")
		return
	}

	force := parseBoolQuery(r.URL.Query().Get("force"))
	if force && !adminScope {
		Forbidden(w, "force cancellation requires admin")
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, orderID).Error; err != nil {
			return err
		}
		if !orderTransitionAllowed(order.Status, orderStatusCanceled, force) {
			return errOrderNotCancelable
		}
		if order.Status == orderStatusCanceled {
			return nil
		}
		previousStatus := order.Status
		order.Status = orderStatusCanceled
		order.UpdatedAt = time.Now().UTC()
		order.CanceledAt = &order.UpdatedAt
		if err := tx.Model(&order).Updates(map[string]interface{}{
			"status":      order.Status,
			"canceled_at": order.CanceledAt,
			"updated_at":  order.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "order.cancel", fmt.Sprintf("order:%d", order.ID), previousStatus+"->"+order.Status)
	})
	if errors.Is(err, errOrderNotCancelable) {
		BadRequest(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

func (h *handlers) OrderPayCallbackHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	orderID, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var req orderCallbackReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = orderStatusPaid
	}

	if status != orderStatusPaid && status != orderStatusSuccess && status != orderStatusFailed && status != orderStatusCanceled {
		BadRequest(w, "invalid callback status")
		return
	}

	if status == orderStatusSuccess {
		status = orderStatusPaid
	}
	var order model.Order
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, orderID).Error; err != nil {
			return err
		}
		if !orderTransitionAllowed(order.Status, status, false) {
			return errOrderTransitionRejected
		}
		previousStatus := order.Status
		now := time.Now().UTC()
		if status == orderStatusPaid {
			if err := h.setOrderPaid(tx, &order, now); err != nil {
				return err
			}
		} else if order.Status != status {
			order.Status = status
			order.UpdatedAt = now
		}
		order.RawCallback = req.RawCallback
		if err := tx.Model(&order).Updates(map[string]interface{}{
			"status":       order.Status,
			"raw_callback": order.RawCallback,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		if previousStatus != order.Status {
			return createAuditLog(tx, claims, "order.payment_result", fmt.Sprintf("order:%d", order.ID), previousStatus+"->"+order.Status)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		BadRequest(w, "order not found")
		return
	}
	if errors.Is(err, errOrderTransitionRejected) {
		BadRequest(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if order.SubscriptionID != 0 && order.Status == orderStatusPaid {
		h.scheduleSubscriptionConfigPublishes(order.SubscriptionID, claims.UserID)
	}
	OK(w, order)
}

func (h *handlers) setOrderPaid(tx *gorm.DB, order *model.Order, now time.Time) error {
	if tx == nil {
		tx = h.db
	}

	if order.Status == orderStatusPaid {
		return nil
	}

	subscription, err := h.allocateOrRenewSubscription(tx, *order, now)
	if err != nil {
		return err
	}

	if err := tx.Model(order).Updates(map[string]interface{}{
		"status":          orderStatusPaid,
		"subscription_id": subscription.ID,
		"paid_amount":     order.PayableAmount,
		"paid_at":         now,
		"fulfilled_at":    now,
		"updated_at":      now,
	}).Error; err != nil {
		return err
	}
	order.Status = orderStatusPaid
	order.SubscriptionID = subscription.ID
	order.PaidAmount = order.PayableAmount
	order.PaidAt = &now
	order.FulfilledAt = &now
	order.UpdatedAt = now
	return nil
}

func (h *handlers) allocateOrRenewSubscription(tx *gorm.DB, order model.Order, now time.Time) (model.Subscription, error) {
	if tx == nil {
		tx = h.db
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, order.UserID).Error; err != nil {
		return model.Subscription{}, err
	}
	var plan model.Plan
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, order.PlanID).Error; err != nil {
		return model.Subscription{}, err
	}
	var sku model.PlanSKU
	if err := tx.First(&sku, order.PlanSKUID).Error; err != nil {
		return model.Subscription{}, err
	}
	if err := expireSubscriptions(tx, order.UserID, now); err != nil {
		return model.Subscription{}, err
	}

	var sub model.Subscription
	var err error
	if order.TargetSubscriptionID != nil {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error
	} else {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND plan_sku_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", order.UserID, order.PlanSKUID, subStatusActive, now).
			Order("end_at desc").First(&sub).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Subscription{}, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if order.TargetSubscriptionID != nil {
			return model.Subscription{}, errors.New("target subscription is unavailable")
		}
		if plan.MaxActiveSubscriptions > 0 {
			var activeCount int64
			if err := tx.Model(&model.Subscription{}).
				Where("plan_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", plan.ID, subStatusActive, now).
				Count(&activeCount).Error; err != nil {
				return model.Subscription{}, err
			}
			if activeCount >= int64(plan.MaxActiveSubscriptions) {
				return model.Subscription{}, errors.New("plan subscription capacity is exhausted")
			}
		}
		periodEnd, err := addBillingPeriod(now, order.BillingUnit, order.BillingValue)
		if err != nil {
			return model.Subscription{}, err
		}
		resetPolicy := effectiveResetPolicy(order.BillingUnit, plan.ResetPolicy)
		nextResetAt := nextTrafficReset(now, resetPolicy)
		renewalPrice := int64(0)
		if plan.IsRenewable {
			renewalPrice = sku.PriceCents
		}
		sub = model.Subscription{
			UserID: order.UserID, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID,
			NodeGroupID: plan.NodeGroupID, SubscriptionType: 1,
			StartAt: now, EndAt: periodEnd, Status: subStatusActive,
			FlowTotal: order.TrafficBytes, FlowUsed: 0,
			SpeedLimitMbps: order.SpeedLimitMbps, DeviceLimit: order.DeviceLimit,
			FamilyLimit: plan.FamilyLimit, RenewalPriceMinor: renewalPrice,
			ResetPolicy: resetPolicy, NextResetAt: nextResetAt,
			TrafficCalcMode: plan.TrafficCalcMode,
			Config:          "{}",
		}
		if err := tx.Create(&sub).Error; err != nil {
			return model.Subscription{}, err
		}
		if _, err := h.ensureSubscriptionCredentials(tx, sub); err != nil {
			return model.Subscription{}, err
		}
		if err := createQuotaEvent(tx, sub, "purchase", order.TrafficBytes, 0, sub.FlowTotal, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
			return model.Subscription{}, err
		}
		return sub, nil
	}

	fulfillment, err := renewalFulfillmentForOrder(order)
	if err != nil {
		return model.Subscription{}, err
	}
	if !plan.IsRenewable && order.OrderType == "renewal" {
		return model.Subscription{}, errors.New("plan does not support renewal")
	}
	if fulfillment.makePermanent {
		sub.EndAt = perpetualSubscriptionEnd
	} else if fulfillment.extendPeriod {
		periodBase := sub.EndAt
		if periodBase.Before(now) || (isPerpetualSubscriptionEnd(periodBase) && order.BillingUnit != "once") {
			periodBase = now
		}
		sub.EndAt, err = addBillingPeriod(periodBase, order.BillingUnit, order.BillingValue)
		if err != nil {
			return model.Subscription{}, err
		}
	}
	before := sub.FlowTotal - sub.FlowUsed
	quotaDelta := int64(0)
	if fulfillment.addQuota {
		quotaDelta = order.TrafficBytes
		sub.FlowTotal += quotaDelta
	}
	sub.Status = subStatusActive
	if order.OrderType != "traffic_pack" {
		sub.PlanID = plan.ID
		sub.PlanSKUID = sku.ID
		sub.NodeGroupID = plan.NodeGroupID
		sub.SpeedLimitMbps = order.SpeedLimitMbps
		sub.DeviceLimit = order.DeviceLimit
		sub.FamilyLimit = plan.FamilyLimit
		sub.ResetPolicy = effectiveResetPolicy(order.BillingUnit, plan.ResetPolicy)
		sub.NextResetAt = nextTrafficReset(now, sub.ResetPolicy)
		sub.TrafficCalcMode = plan.TrafficCalcMode
		if plan.IsRenewable {
			sub.RenewalPriceMinor = sku.PriceCents
		} else {
			sub.RenewalPriceMinor = 0
		}
	}

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, err
	}
	if _, err := h.ensureSubscriptionCredentials(tx, sub); err != nil {
		return model.Subscription{}, err
	}
	if err := createQuotaEvent(tx, sub, order.OrderType, quotaDelta, before, before+quotaDelta, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
		return model.Subscription{}, err
	}
	return sub, nil
}

type renewalFulfillment struct {
	extendPeriod  bool
	addQuota      bool
	makePermanent bool
}

func renewalFulfillmentForOrder(order model.Order) (renewalFulfillment, error) {
	switch order.OrderType {
	case "traffic_pack":
		return renewalFulfillment{addQuota: true}, nil
	case "upgrade":
		return renewalFulfillment{extendPeriod: true, addQuota: true}, nil
	case "renewal":
		effect := strings.TrimSpace(order.RenewalEffect)
		if effect == "" {
			// Compatibility for an order created before the renewal-effect snapshot
			// existed. Reconciliation persists this same interpretation.
			if order.BillingUnit == "once" {
				effect = skuRenewalAddQuotaOnly
			} else {
				effect = skuRenewalExtendAndAdd
			}
		}
		switch effect {
		case skuRenewalExtendOnly:
			return renewalFulfillment{extendPeriod: true}, nil
		case skuRenewalExtendAndAdd:
			return renewalFulfillment{extendPeriod: true, addQuota: true}, nil
		case skuRenewalAddQuotaOnly:
			return renewalFulfillment{addQuota: true, makePermanent: order.BillingUnit == "once"}, nil
		default:
			return renewalFulfillment{}, fmt.Errorf("unsupported renewal effect %q", effect)
		}
	default:
		return renewalFulfillment{extendPeriod: true, addQuota: true}, nil
	}
}

func createQuotaEvent(tx *gorm.DB, sub model.Subscription, eventType string, delta, before, after int64, referenceType, referenceID string) error {
	return tx.Create(&model.QuotaEvent{
		SubscriptionID: sub.ID, EventType: eventType, DeltaBytes: delta,
		BalanceBefore: before, BalanceAfter: after,
		ReferenceType: referenceType, ReferenceID: referenceID, Detail: "{}",
	}).Error
}

func nextTrafficReset(base time.Time, policy int16) *time.Time {
	base = base.UTC()
	var next time.Time
	switch policy {
	case 1:
		next = time.Date(base.Year(), base.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	case 2:
		next = addCalendarMonths(base, 1)
	case 3:
		next = time.Date(base.Year()+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	case 4:
		next = base.AddDate(1, 0, 0)
	default:
		return nil
	}
	return &next
}

func effectiveResetPolicy(billingUnit string, planPolicy int16) int16 {
	if billingUnit == "once" {
		return 5
	}
	return planPolicy
}

func isPerpetualSubscriptionEnd(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= perpetualSubscriptionEnd.Year()
}

func addBillingPeriod(base time.Time, unit string, value int) (time.Time, error) {
	if value <= 0 {
		return time.Time{}, errors.New("billing value must be positive")
	}
	switch unit {
	case "day":
		return base.AddDate(0, 0, value), nil
	case "month":
		return addCalendarMonths(base, value), nil
	case "year":
		return addCalendarMonths(base, value*12), nil
	case "once":
		return perpetualSubscriptionEnd, nil
	default:
		return time.Time{}, errors.New("unsupported billing unit")
	}
}

func addCalendarMonths(base time.Time, months int) time.Time {
	monthIndex := int(base.Month()) - 1 + months
	year := base.Year() + monthIndex/12
	monthIndex %= 12
	if monthIndex < 0 {
		monthIndex += 12
		year--
	}
	month := time.Month(monthIndex + 1)
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, base.Location()).Day()
	day := base.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), base.Location())
}

func expireSubscriptions(db *gorm.DB, userID uint, now time.Time) error {
	if db == nil {
		return errors.New("database is required")
	}
	query := db.Model(&model.Subscription{}).
		Where("status = ? AND (end_at <= ? OR flow_used >= flow_total)", subStatusActive, now)
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Update("status", subStatusExpired).Error; err != nil {
		return err
	}
	credentialQuery := db.Model(&model.ProtocolCredential{}).
		Where("status IN ? AND subscription_id IN (?)", []string{protocolCredentialStatusActive, protocolCredentialStatusPrepared},
			db.Model(&model.Subscription{}).Select("id").Where("status <> ?", subStatusActive))
	if userID != 0 {
		credentialQuery = credentialQuery.Where("user_id = ?", userID)
	}
	return credentialQuery.Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error
}

func (h *handlers) SubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/subscriptions")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	scopeUserID := claims.UserID
	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			if parsed, parseErr := strconv.ParseUint(target, 10, 64); parseErr == nil && parsed > 0 {
				scopeUserID = uint(parsed)
			} else {
				BadRequest(w, "invalid user_id")
				return
			}
		} else {
			scopeUserID = 0
		}
	}
	now := time.Now().UTC()

	paged := wantsPagedList(r)
	var subs []model.Subscription
	query := h.db.Model(&model.Subscription{})
	if adminScope || paged {
		query = query.
			Joins("LEFT JOIN users ON users.id = subscriptions.user_id").
			Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").
			Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id")
	}
	if scopeUserID != 0 {
		query = query.Where("subscriptions.user_id = ?", scopeUserID)
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !isValidSubscriptionStatus(status) {
			BadRequest(w, "invalid status")
			return
		}
		query = applyEffectiveSubscriptionStatusFilter(query, status, now)
	}
	if adminScope {
		if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
			if len(search) > 128 {
				BadRequest(w, "q must not exceed 128 bytes")
				return
			}
			pattern := "%" + strings.ToLower(search) + "%"
			condition := `LOWER(users.email) LIKE ? OR LOWER(plans.name) LIKE ? OR LOWER(plan_skus.name) LIKE ?`
			args := []interface{}{pattern, pattern, pattern}
			if parsed, parseErr := strconv.ParseUint(search, 10, 64); parseErr == nil && parsed > 0 {
				condition += " OR subscriptions.id = ? OR subscriptions.user_id = ? OR subscriptions.plan_id = ?"
				args = append(args, parsed, parsed, parsed)
			}
			query = query.Where(condition, args...)
		}
		if quota := strings.TrimSpace(r.URL.Query().Get("quota")); quota != "" {
			if !isValidSubscriptionQuotaFilter(quota) {
				BadRequest(w, "invalid quota")
				return
			}
			if quota == "available" {
				query = query.Where("subscriptions.flow_used < subscriptions.flow_total")
			} else {
				query = query.Where("subscriptions.flow_used >= subscriptions.flow_total")
			}
		}
		window, present, windowErr := parseOptionalDateWindow(r.URL.Query(), "expires_from", "expires_to", historyMaxWindowDays)
		if windowErr != nil {
			BadRequest(w, windowErr.Error())
			return
		}
		if present {
			query = applyHistoryWindow(query, "subscriptions.end_at", window)
		}
	}
	offset, limit := 0, 50
	var total int64
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		var items []adminSubscriptionListItem
		if err := query.
			Select(`subscriptions.id, subscriptions.user_id, users.email AS user_email,
				subscriptions.plan_id, plans.name AS plan_name,
				subscriptions.plan_sku_id, plan_skus.name AS sku_name,
				subscriptions.node_group_id, subscriptions.subscription_type,
				subscriptions.start_at, subscriptions.end_at,
				CASE
					WHEN subscriptions.status = 'active'
						AND (subscriptions.end_at <= ? OR subscriptions.flow_used >= subscriptions.flow_total)
					THEN 'expired'
					ELSE subscriptions.status
				END AS status,
				subscriptions.flow_total, subscriptions.flow_used,
				subscriptions.speed_limit_mbps, subscriptions.device_limit,
				subscriptions.family_limit, subscriptions.renewal_price_minor,
				subscriptions.reset_policy, subscriptions.next_reset_at,
				subscriptions.traffic_calc_mode, subscriptions.created_at,
				subscriptions.updated_at`, now).
			Order("subscriptions.id desc").Offset(offset).Limit(limit).Scan(&items).Error; err != nil {
			ServerError(w, err)
			return
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	if err := query.Order("subscriptions.id desc").Find(&subs).Error; err != nil {
		ServerError(w, err)
		return
	}
	for index := range subs {
		subs[index].Status = effectiveSubscriptionStatus(subs[index], now)
	}
	OK(w, subs)
}

func (h *handlers) SubscriptionAccessHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var token model.SubscriptionToken
	err = h.db.Where("user_id = ?", claims.UserID).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		OK(w, map[string]interface{}{"configured": false})
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if token.RevokedAt != nil {
		OK(w, map[string]interface{}{
			"configured":   false,
			"token_prefix": token.TokenPrefix,
			"last_used_at": token.LastUsedAt,
			"revoked_at":   token.RevokedAt,
			"updated_at":   token.UpdatedAt,
		})
		return
	}

	if token.TokenCiphertext == "" {
		OK(w, map[string]interface{}{
			"configured":   false,
			"token_prefix": token.TokenPrefix,
			"last_used_at": token.LastUsedAt,
			"updated_at":   token.UpdatedAt,
		})
		return
	}
	rawToken, err := h.readableSubscriptionToken(&token)
	if err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"configured":       true,
		"token":            rawToken,
		"token_prefix":     token.TokenPrefix,
		"subscription_url": "/api/v1/client/subscription/" + rawToken,
		"last_used_at":     token.LastUsedAt,
		"revoked_at":       token.RevokedAt,
		"updated_at":       token.UpdatedAt,
	})
}

func (h *handlers) SubscriptionAccessRotateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	rawToken, tokenHash, tokenPrefix, err := newSubscriptionToken()
	if err != nil {
		ServerError(w, err)
		return
	}
	encryptedToken, err := h.credentialCipher.Encrypt(rawToken)
	if err != nil {
		ServerError(w, err)
		return
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var token model.SubscriptionToken
		findErr := tx.Where("user_id = ?", claims.UserID).First(&token).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		token.UserID = claims.UserID
		token.TokenHash = tokenHash
		token.TokenCiphertext = encryptedToken
		token.TokenPrefix = tokenPrefix
		token.LastUsedAt = nil
		token.RevokedAt = nil
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if err := tx.Create(&token).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&token).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{
			UserID: auditUserID(claims.UserID),
			Actor:  claims.Email,
			Action: "subscription_token.rotate",
			Target: fmt.Sprintf("user:%d", claims.UserID),
			Detail: tokenPrefix,
		}).Error
	})
	if err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"configured":       true,
		"token":            rawToken,
		"token_prefix":     tokenPrefix,
		"subscription_url": "/api/v1/client/subscription/" + rawToken,
		"notice":           "the link remains available in the account; rotating invalidates previous URLs",
	})
}

func (h *handlers) readableSubscriptionToken(token *model.SubscriptionToken) (string, error) {
	if token == nil || token.ID == 0 {
		return "", errors.New("subscription token is required")
	}
	if token.TokenCiphertext == "" {
		return "", errors.New("subscription token is not recoverable; rotate it to generate a new link")
	}
	raw, err := h.credentialCipher.Decrypt(token.TokenCiphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt subscription token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(hashSubscriptionToken(raw)), []byte(token.TokenHash)) != 1 {
		return "", errors.New("subscription token integrity check failed")
	}
	return raw, nil
}

func (h *handlers) SubscriptionAccessRevokeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	now := time.Now().UTC()
	revoked := false
	err = h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SubscriptionToken{}).
			Where("user_id = ? AND revoked_at IS NULL", claims.UserID).
			Update("revoked_at", now)
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected > 0
		if !revoked {
			return nil
		}
		return createAuditLog(tx, claims, "subscription_token.revoke", fmt.Sprintf("user:%d", claims.UserID), "credential revoked")
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"configured": false, "revoked": revoked})
}

func (h *handlers) ClientSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/client/subscription/"))
	if rawToken == "" || strings.Contains(rawToken, "/") {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}

	var access model.SubscriptionToken
	tokenHash := hashSubscriptionToken(rawToken)
	if err := h.db.Where("token_hash = ? AND revoked_at IS NULL", tokenHash).First(&access).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", access.UserID, userStatusActive).First(&user).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}

	now := time.Now().UTC()
	if err := expireSubscriptions(h.db, access.UserID, now); err != nil {
		ServerError(w, err)
		return
	}
	subscriptions := make([]model.Subscription, 0)
	if err := h.db.Where(
		"user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total",
		access.UserID, subStatusActive, now,
	).Order("end_at asc, id asc").Find(&subscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}
	if len(subscriptions) == 0 {
		Forbidden(w, "subscription is inactive, expired, or out of traffic")
		return
	}
	if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
		ServerError(w, err)
		return
	}

	nodeGroupIDs := make([]uint, 0, len(subscriptions))
	subscriptionIDs := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		nodeGroupIDs = append(nodeGroupIDs, subscription.NodeGroupID)
		subscriptionIDs = append(subscriptionIDs, subscription.ID)
	}
	manifestNodes := make([]subscriptionManifestNode, 0)
	var credentials []model.ProtocolCredential
	if err := h.db.Where("subscription_id IN ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", uniqueUintIDs(subscriptionIDs), protocolCredentialStatusActive, now).
		Order("protocol_endpoint_id asc, id asc").Find(&credentials).Error; err != nil {
		ServerError(w, err)
		return
	}
	for _, credential := range credentials {
		var endpoint model.ProtocolEndpoint
		if err := h.db.Where("id = ? AND is_active = ?", credential.ProtocolEndpointID, true).First(&endpoint).Error; err != nil {
			continue
		}
		if !h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		var node model.Node
		if err := h.db.Select("id", "region", "last_seen_at", "is_enabled").First(&node, endpoint.NodeID).Error; err != nil || !node.IsEnabled || node.LastSeenAt == nil || node.LastSeenAt.Before(now.Add(-nodeOnlineWindow)) {
			continue
		}
		if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, node); !supported {
			continue
		}
		clientConfig, err := h.credentialClientConfig(endpoint, credential)
		if err != nil {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: credential.SubscriptionID,
			CredentialID: credential.CredentialID, Name: endpoint.Name, Region: node.Region,
			Address: endpoint.Address, Port: credential.ListenPort, PublicPort: credential.PublicPort,
			Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli, Config: clientConfig,
		})
	}

	// Protocols without a native attributed-user contract remain available via
	// their legacy endpoint template. They are deliberately kept separate from
	// the credential-backed protocols above so the panel never pretends those
	// flows have per-subscription attribution.
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Select("DISTINCT protocol_endpoints.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN nodes ON nodes.id = protocol_endpoints.node_id").
		Where("node_group_endpoints.node_group_id IN ? AND protocol_endpoints.is_active = ? AND nodes.last_seen_at >= ? AND nodes.is_enabled = ? AND protocol_endpoints.client_config <> ''", uniqueUintIDs(nodeGroupIDs), true, now.Add(-nodeOnlineWindow), true).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").Find(&endpoints).Error; err != nil {
		ServerError(w, err)
		return
	}
	for _, endpoint := range endpoints {
		if h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		var node model.Node
		if err := h.db.Select("id", "region").First(&node, endpoint.NodeID).Error; err != nil {
			continue
		}
		if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, node); !supported {
			continue
		}
		clientConfig, err := h.endpointSubscriptionClientConfig(endpoint)
		if err != nil {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Region: node.Region,
			Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort, Protocol: endpoint.Protocol,
			MultiplierMilli: endpoint.MultiplierMilli, Config: clientConfig,
		})
	}
	if err := h.sortSubscriptionManifestNodes(subscriptions, manifestNodes); err != nil {
		ServerError(w, fmt.Errorf("resolve subscription delivery order: %w", err))
		return
	}

	var total, used int64
	var expiresAt time.Time
	for _, sub := range subscriptions {
		total += sub.FlowTotal
		used += sub.FlowUsed
		if sub.EndAt.After(expiresAt) {
			expiresAt = sub.EndAt
		}
	}
	_ = h.db.Model(&access).Update("last_used_at", now).Error
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", used, total, expiresAt.Unix()))
	manifest := subscriptionManifest{
		Version:     "zboard.subscription/v1",
		GeneratedAt: now.Format(time.RFC3339),
		Subscription: subscriptionManifestSummary{
			ExpiresAt: expiresAt.Format(time.RFC3339), FlowTotal: total, FlowUsed: used, FlowRemaining: total - used,
		},
		ProtocolEndpoints: manifestNodes,
	}
	delivery := resolveSubscriptionDelivery(r.URL.Query().Get("template"), r.UserAgent())
	if delivery.UsesUserAgent {
		w.Header().Add("Vary", "User-Agent")
	}
	if delivery.TemplateSlug != "" {
		if err := h.writeSubscriptionTemplate(r.Context(), w, delivery.TemplateSlug, manifest); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if delivery.UsesUserAgent {
					if err := writeBase64SubscriptionManifest(w, manifest, subscriptionDeliveryNative); err != nil {
						ServerError(w, err)
					}
					return
				}
				NotFound(w)
				return
			}
			ServerError(w, fmt.Errorf("render subscription template: %w", err))
		}
		return
	}
	if err := writeBase64SubscriptionManifest(w, manifest, delivery.Format); err != nil {
		ServerError(w, err)
	}
}

func writeBase64SubscriptionManifest(w http.ResponseWriter, manifest subscriptionManifest, format string) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode native subscription manifest: %w", err)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Zboard-Subscription-Format", format+"-base64")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(base64.StdEncoding.EncodeToString(raw)))
	return err
}

func (h *handlers) redirectSubscriptionCamouflage(w http.ResponseWriter, r *http.Request) {
	configuredTarget := ""
	var config model.SystemConfig
	if err := h.db.Where("config_key = ?", "subscription_camouflage_url").First(&config).Error; err == nil {
		configuredTarget = config.Value
	}
	siteURL := ""
	if strings.TrimSpace(configuredTarget) == "" {
		var installation model.Installation
		if err := h.db.Select("site_url").First(&installation, 1).Error; err == nil {
			siteURL = installation.SiteURL
		}
	}
	writeSubscriptionCamouflageRedirect(w, r, subscriptionCamouflageTarget(configuredTarget, siteURL))
}

func subscriptionCamouflageTarget(configuredTarget, siteURL string) string {
	if target := strings.TrimSpace(configuredTarget); target != "" {
		return target
	}
	if target := strings.TrimSpace(siteURL); target != "" {
		return target
	}
	return "/"
}

func writeSubscriptionCamouflageRedirect(w http.ResponseWriter, r *http.Request, target string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *handlers) endpointDeliversSubscriptionCredential(endpoint model.ProtocolEndpoint) bool {
	switch strings.ToLower(strings.TrimSpace(endpoint.Protocol)) {
	case "mieru":
		return endpoint.MieruPrincipalReady
	case "trojan", "hysteria2":
		return endpoint.ManagedPrincipalReady
	default:
		return h.protocolUsesSubscriptionCredential(endpoint.Protocol)
	}
}

func newSubscriptionToken() (string, string, string, error) {
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(entropy)
	return raw, hashSubscriptionToken(raw), raw[:12], nil
}

func hashSubscriptionToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func (h *handlers) TrafficSummaryHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	userFilter := claims.UserID
	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			parsed, parseErr := strconv.ParseUint(target, 10, 64)
			if parseErr != nil {
				BadRequest(w, "invalid user_id")
				return
			}
			userFilter = uint(parsed)
		} else {
			userFilter = 0
		}
	}
	now := time.Now().UTC()

	var totalUsed int64
	usedQuery := h.db.Model(&model.TrafficRecord{}).Select("COALESCE(SUM(used_bytes), 0)")
	if userFilter > 0 {
		usedQuery = usedQuery.Where("user_id = ?", userFilter)
	}
	if err := usedQuery.Scan(&totalUsed).Error; err != nil {
		ServerError(w, err)
		return
	}

	var currentRemain int64
	subQuery := h.db.Model(&model.Subscription{})
	if userFilter > 0 {
		subQuery = subQuery.Where("user_id = ?", userFilter)
	}
	subQuery = subQuery.Where("status = ? AND end_at > ? AND flow_used < flow_total", subStatusActive, now).
		Select("COALESCE(SUM(flow_total - flow_used), 0)")
	if err := subQuery.Scan(&currentRemain).Error; err != nil {
		ServerError(w, err)
		return
	}

	todayStart := now.Truncate(24 * time.Hour)
	var usedToday int64
	todayQuery := h.db.Model(&model.TrafficRecord{}).Where("record_at >= ?", todayStart).Select("COALESCE(SUM(used_bytes), 0)")
	if userFilter > 0 {
		todayQuery = todayQuery.Where("user_id = ?", userFilter)
	}
	if err := todayQuery.Scan(&usedToday).Error; err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"total_used_bytes": totalUsed,
		"remaining_bytes":  currentRemain,
		"used_bytes_today": usedToday,
		"scope_user":       userFilter,
		"as_of":            now.Format(time.RFC3339),
	})
}

func (h *handlers) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	now := time.Now().UTC()

	var users int64
	var nodes int64
	var plans int64
	var orders int64
	var subscriptions int64
	var activeSubscriptions int64
	var paidOrders int64
	var paidRevenue int64
	var pendingOrders int64
	var failedOrders int64
	var offlineNodes int64
	var pendingTickets int64
	var failedTasks int64
	var failedDeployments int64
	var connectorOnlineNodes int64
	var sshVerifiedNodes int64
	var trafficReadyNodes int64
	var protocolEndpoints int64
	var activeProtocolEndpoints int64

	var activeUsers int64
	var trafficTotal int64

	if err := h.db.Model(&model.User{}).Count(&users).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Node{}).Count(&nodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Plan{}).Where("is_active = 1").Count(&plans).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Order{}).Count(&orders).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Subscription{}).Count(&subscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Subscription{}).
		Where("status = ? AND end_at > ? AND flow_used < flow_total", subStatusActive, now).
		Count(&activeSubscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Order{}).Where("status = ?", orderStatusPaid).Count(&paidOrders).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Order{}).Where("status = ?", orderStatusPaid).Select("COALESCE(SUM(amount_cents),0)").Scan(&paidRevenue).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Order{}).Where("status = ?", orderStatusPending).Count(&pendingOrders).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Order{}).Where("status = ?", orderStatusFailed).Count(&failedOrders).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Node{}).
		Where("is_enabled = ? AND (last_seen_at IS NULL OR last_seen_at < ?)", true, now.Add(-nodeOnlineWindow)).
		Count(&offlineNodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Node{}).
		Where("is_enabled = ? AND connector_last_seen_at >= ?", true, now.Add(-nodeOnlineWindow)).
		Count(&connectorOnlineNodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Node{}).
		Where("ssh_verified_at IS NOT NULL AND ssh_host_key_fingerprint <> ''").
		Count(&sshVerifiedNodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Node{}).
		Where("traffic_secret_prefix <> '' AND traffic_secret_revoked_at IS NULL").
		Count(&trafficReadyNodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Count(&protocolEndpoints).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("is_active = ?", true).Count(&activeProtocolEndpoints).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Ticket{}).Where("status IN ?", []string{ticketStatusOpen, ticketStatusPendingAdmin}).Count(&pendingTickets).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Task{}).Where("status = ?", taskStatusFailed).Count(&failedTasks).Error; err != nil {
		ServerError(w, err)
		return
	}
	latestDeploymentIDs := h.db.Model(&model.ProtocolDeployment{}).
		Select("MAX(id)").
		Group("protocol_endpoint_id")
	if err := h.db.Model(&model.ProtocolDeployment{}).
		Where("id IN (?) AND status = ?", latestDeploymentIDs, "failed").
		Count(&failedDeployments).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.User{}).Where("status = ?", userStatusActive).Count(&activeUsers).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Subscription{}).
		Where("status = ? AND end_at > ? AND flow_used < flow_total", subStatusActive, now).
		Select("COALESCE(SUM(flow_total - flow_used), 0)").Scan(&trafficTotal).Error; err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"users":                     users,
		"users_active":              activeUsers,
		"nodes":                     nodes,
		"plans":                     plans,
		"orders":                    orders,
		"orders_paid":               paidOrders,
		"orders_pending":            pendingOrders,
		"orders_failed":             failedOrders,
		"revenue_cents":             paidRevenue,
		"subscriptions":             subscriptions,
		"subscriptions_active":      activeSubscriptions,
		"traffic_pool_bytes":        trafficTotal,
		"nodes_offline":             offlineNodes,
		"tickets_pending":           pendingTickets,
		"tasks_failed":              failedTasks,
		"deployments_failed":        failedDeployments,
		"nodes_connector_online":    connectorOnlineNodes,
		"nodes_ssh_verified":        sshVerifiedNodes,
		"nodes_traffic_ready":       trafficReadyNodes,
		"protocol_endpoints":        protocolEndpoints,
		"protocol_endpoints_active": activeProtocolEndpoints,
	})
}

func (h *handlers) AuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	window, err := parseHistoryWindow(r.URL.Query(), 30)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	cursor, err := decodeHistoryCursor(r.URL.Query().Get("cursor"), nil)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	query := applyHistoryWindow(h.db.Model(&model.AuditLog{}), "created_at", window)
	if actor := strings.TrimSpace(r.URL.Query().Get("actor")); actor != "" {
		query = query.Where("actor = ?", actor)
	}
	if action := strings.TrimSpace(r.URL.Query().Get("action")); action != "" {
		query = query.Where("action = ?", action)
	}
	if target := strings.TrimSpace(r.URL.Query().Get("target")); target != "" {
		query = query.Where("target = ?", target)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	logs := make([]model.AuditLog, 0)
	if cursor == nil && offset > 0 {
		if err := query.Order("created_at desc, id desc").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
			ServerError(w, err)
			return
		}
		OK(w, pagedData(auditLogSummaries(logs), total, offset, limit))
		return
	}
	if err := applySimpleHistoryCursor(query, "created_at", cursor).Order(simpleHistoryOrder("created_at", cursor)).Limit(limit + 1).Find(&logs).Error; err != nil {
		ServerError(w, err)
		return
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	if cursor != nil && cursor.Direction == historyDirectionNewer {
		reverseHistoryPage(logs)
	}
	var nextCursor, previousCursor *string
	if len(logs) > 0 {
		nextCursor, previousCursor, err = historyPageCursorValues(
			historyKey{At: logs[0].CreatedAt, ID: logs[0].ID},
			historyKey{At: logs[len(logs)-1].CreatedAt, ID: logs[len(logs)-1].ID},
			cursor,
			hasMore,
		)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, cursorPagedData(auditLogSummaries(logs), total, limit, nextCursor, previousCursor))
}

func (h *handlers) TrafficRecordsHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	var userFilter, nodeFilter, protocolEndpointFilter, subscriptionFilter uint64
	if !adminScope {
		userFilter = uint64(claims.UserID)
	} else if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid user_id")
			return
		}
		userFilter = parsed
	}
	if target := strings.TrimSpace(r.URL.Query().Get("node_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid node_id")
			return
		}
		nodeFilter = parsed
	}
	if target := strings.TrimSpace(r.URL.Query().Get("protocol_endpoint_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid protocol_endpoint_id")
			return
		}
		protocolEndpointFilter = parsed
	}
	if target := strings.TrimSpace(r.URL.Query().Get("subscription_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid subscription_id")
			return
		}
		subscriptionFilter = parsed
	}
	baseQuery := func() *gorm.DB {
		query := h.db.Model(&model.TrafficRecord{})
		if userFilter > 0 {
			query = query.Where("user_id = ?", userFilter)
		}
		if nodeFilter > 0 {
			query = query.Where("node_id = ?", nodeFilter)
		}
		if protocolEndpointFilter > 0 {
			query = query.Where("protocol_endpoint_id = ?", protocolEndpointFilter)
		}
		if subscriptionFilter > 0 {
			query = query.Where("subscription_id = ?", subscriptionFilter)
		}
		return query
	}

	paged := wantsPagedList(r)
	offset, limit := 0, 50
	var total int64
	records := make([]model.TrafficRecord, 0)
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		window, windowErr := parseHistoryWindow(r.URL.Query(), 7)
		if windowErr != nil {
			BadRequest(w, windowErr.Error())
			return
		}
		cursor, cursorErr := decodeHistoryCursor(r.URL.Query().Get("cursor"), nil)
		if cursorErr != nil {
			BadRequest(w, cursorErr.Error())
			return
		}
		windowedQuery := func() *gorm.DB {
			return applyHistoryWindow(baseQuery(), "record_at", window)
		}
		var aggregates trafficRecordAggregates
		if err := windowedQuery().Select(`
			COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
			COALESCE(SUM(used_bytes), 0) AS used_bytes,
			COUNT(DISTINCT user_id) AS user_count,
			COUNT(DISTINCT NULLIF(subscription_id, 0)) AS subscription_count,
			COUNT(DISTINCT node_id) AS node_count,
			COUNT(DISTINCT protocol_endpoint_id) AS protocol_endpoint_count
		`).Scan(&aggregates).Error; err != nil {
			ServerError(w, err)
			return
		}
		if err := windowedQuery().Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		if cursor == nil && offset > 0 {
			if err := windowedQuery().Order("record_at desc, id desc").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
				ServerError(w, err)
				return
			}
			data := pagedData(trafficRecordSummaries(records), total, offset, limit)
			data["aggregates"] = aggregates
			OK(w, data)
			return
		}
		if err := applySimpleHistoryCursor(windowedQuery(), "record_at", cursor).Order(simpleHistoryOrder("record_at", cursor)).Limit(limit + 1).Find(&records).Error; err != nil {
			ServerError(w, err)
			return
		}
		hasMore := len(records) > limit
		if hasMore {
			records = records[:limit]
		}
		if cursor != nil && cursor.Direction == historyDirectionNewer {
			reverseHistoryPage(records)
		}
		var nextCursor, previousCursor *string
		if len(records) > 0 {
			nextCursor, previousCursor, err = historyPageCursorValues(
				historyKey{At: records[0].At, ID: records[0].ID},
				historyKey{At: records[len(records)-1].At, ID: records[len(records)-1].ID},
				cursor,
				hasMore,
			)
			if err != nil {
				ServerError(w, err)
				return
			}
		}
		data := cursorPagedData(trafficRecordSummaries(records), total, limit, nextCursor, previousCursor)
		data["aggregates"] = aggregates
		OK(w, data)
		return
	}
	if err := baseQuery().Order("id desc").Find(&records).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, records)
}

func trafficRecordSummaries(records []model.TrafficRecord) []trafficRecordListItem {
	items := make([]trafficRecordListItem, 0, len(records))
	for _, record := range records {
		items = append(items, trafficRecordListItem{
			ID:                      record.ID,
			UserID:                  record.UserID,
			SubscriptionID:          record.SubscriptionID,
			NodeID:                  record.NodeID,
			ProtocolEndpointID:      record.ProtocolEndpointID,
			RawBytes:                record.RawBytes,
			UploadBytes:             record.UploadBytes,
			DownloadBytes:           record.DownloadBytes,
			ProtocolMultiplierMilli: record.ProtocolMultiplierMilli,
			UsedBytes:               record.UsedBytes,
			RecordAt:                record.At,
		})
	}
	return items
}

func (h *handlers) TrafficReconciliationHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	userID := claims.UserID
	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			parsed, parseErr := strconv.ParseUint(target, 10, 64)
			if parseErr != nil || parsed == 0 {
				BadRequest(w, "invalid user_id")
				return
			}
			userID = uint(parsed)
		} else {
			userID = 0
		}
	}
	now := time.Now().UTC()

	var subscriptionID uint
	if target := strings.TrimSpace(r.URL.Query().Get("subscription_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid subscription_id")
			return
		}
		subscriptionID = uint(parsed)
	}
	issuesOnly := false
	if adminScope {
		if rawIssuesOnly := strings.TrimSpace(r.URL.Query().Get("issues_only")); rawIssuesOnly != "" {
			issuesOnly, err = strconv.ParseBool(rawIssuesOnly)
			if err != nil {
				BadRequest(w, "invalid issues_only")
				return
			}
		}
	}
	baseQuery := func() *gorm.DB {
		query := h.db.Model(&model.Subscription{})
		if userID != 0 {
			query = query.Where("subscriptions.user_id = ?", userID)
		}
		if subscriptionID != 0 {
			query = query.Where("subscriptions.id = ?", subscriptionID)
		}
		return query
	}
	trafficTotalsQuery := func() *gorm.DB {
		return h.db.Model(&model.TrafficRecord{}).
			Select("subscription_id, COALESCE(SUM(used_bytes), 0) AS recorded_bytes").
			Group("subscription_id")
	}
	paged := adminScope && r.URL.Query().Get("paged") == "true"
	offset, limit := 0, 50
	var total int64
	aggregates := trafficReconciliationAggregates{}
	query := baseQuery().Order("subscriptions.id desc")
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if err := baseQuery().
			Joins("LEFT JOIN (?) AS reconciliation_totals ON reconciliation_totals.subscription_id = subscriptions.id", trafficTotalsQuery()).
			Select(`
				COUNT(*) AS subscription_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used = COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS matched_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used > COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS missing_records_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used < COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS over_recorded_count,
				COALESCE(SUM(subscriptions.flow_used), 0) AS flow_used,
				COALESCE(SUM(COALESCE(reconciliation_totals.recorded_bytes, 0)), 0) AS recorded_bytes,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used > COALESCE(reconciliation_totals.recorded_bytes, 0) THEN subscriptions.flow_used - COALESCE(reconciliation_totals.recorded_bytes, 0) ELSE 0 END), 0) AS missing_bytes,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used < COALESCE(reconciliation_totals.recorded_bytes, 0) THEN COALESCE(reconciliation_totals.recorded_bytes, 0) - subscriptions.flow_used ELSE 0 END), 0) AS over_recorded_bytes
			`).
			Scan(&aggregates).Error; err != nil {
			ServerError(w, err)
			return
		}
		if issuesOnly {
			query = query.
				Joins("LEFT JOIN (?) AS reconciliation_totals ON reconciliation_totals.subscription_id = subscriptions.id", trafficTotalsQuery()).
				Where("subscriptions.flow_used <> COALESCE(reconciliation_totals.recorded_bytes, 0)")
		}
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		query = query.Offset(offset).Limit(limit)
	}
	subscriptions := make([]model.Subscription, 0)
	if err := query.Find(&subscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}

	totals := make(map[uint]int64, len(subscriptions))
	if len(subscriptions) > 0 {
		ids := make([]uint, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			ids = append(ids, subscription.ID)
		}
		var aggregates []struct {
			SubscriptionID uint  `gorm:"column:subscription_id"`
			RecordedBytes  int64 `gorm:"column:recorded_bytes"`
		}
		if err := h.db.Model(&model.TrafficRecord{}).
			Select("subscription_id, COALESCE(SUM(used_bytes), 0) AS recorded_bytes").
			Where("subscription_id IN ?", ids).
			Group("subscription_id").Scan(&aggregates).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, aggregate := range aggregates {
			totals[aggregate.SubscriptionID] = aggregate.RecordedBytes
		}
	}

	items := make([]trafficReconciliationItem, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		recorded := totals[subscription.ID]
		difference := subscription.FlowUsed - recorded
		items = append(items, trafficReconciliationItem{
			SubscriptionID: subscription.ID,
			UserID:         subscription.UserID,
			PlanID:         subscription.PlanID,
			Status:         effectiveSubscriptionStatus(subscription, now),
			FlowUsed:       subscription.FlowUsed,
			RecordedBytes:  recorded,
			Difference:     difference,
			Result:         trafficReconciliationResult(difference),
		})
	}
	if paged {
		data := pagedData(items, total, offset, limit)
		data["aggregates"] = aggregates
		OK(w, data)
		return
	}
	OK(w, items)
}

func trafficReconciliationResult(difference int64) string {
	if difference > 0 {
		return "missing_records"
	}
	if difference < 0 {
		return "over_recorded"
	}
	return "matched"
}

func (h *handlers) TrafficReportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		BadRequest(w, "request body is required")
		return
	}
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil {
		BadRequest(w, "failed to read request body")
		return
	}
	if len(rawBody) > nodeReportMaxBodyBytes {
		BadRequest(w, "request body is too large")
		return
	}

	authenticated, err := h.authenticateNodeReport(r, rawBody, time.Now().UTC())
	if err != nil {
		Unauthorized(w, "invalid node report authentication")
		return
	}
	nodeVersion := strings.TrimSpace(r.Header.Get("X-Zboard-Node-Version"))
	if len(nodeVersion) > 64 {
		BadRequest(w, "node version is too long")
		return
	}

	var req trafficReportReq
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		BadRequest(w, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		BadRequest(w, "request body must contain one JSON object")
		return
	}
	req.ReportID = strings.TrimSpace(req.ReportID)
	if !validNodeReportIdentifier(req.ReportID, 8, 64) {
		BadRequest(w, "report_id must be 8-64 URL-safe characters")
		return
	}
	if req.UserID == 0 {
		BadRequest(w, "user_id is required")
		return
	}
	if req.ProtocolEndpointID == 0 {
		BadRequest(w, "protocol_endpoint_id is required")
		return
	}
	if req.RawBytes < 0 || req.UploadBytes < 0 || req.DownloadBytes < 0 ||
		req.RawBytes > 1<<50 || req.UploadBytes > 1<<50 || req.DownloadBytes > 1<<50 {
		BadRequest(w, "traffic byte values must be between 0 and 1 PiB")
		return
	}
	if req.UploadBytes == 0 && req.DownloadBytes == 0 {
		req.DownloadBytes = req.RawBytes
	}
	if req.UploadBytes == 0 && req.DownloadBytes == 0 {
		BadRequest(w, "upload_bytes or download_bytes is required")
		return
	}

	var sub model.Subscription
	var record model.TrafficRecord
	duplicate := false
	quotaExhausted := false
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var lockedNode model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedNode, authenticated.node.ID).Error; err != nil {
			return err
		}
		if !lockedNode.IsEnabled || lockedNode.TrafficSecret == "" || lockedNode.TrafficSecretRevokedAt != nil || lockedNode.TrafficSecret != authenticated.node.TrafficSecret {
			return errNodeReportCredentialChanged
		}
		nodeUpdates := map[string]interface{}{
			"is_online": true, "status": 1,
			"last_seen_at": authenticated.timestamp, "last_sync_at": authenticated.timestamp,
		}
		if nodeVersion != "" {
			nodeUpdates["version"] = nodeVersion
		}
		if err := tx.Model(&lockedNode).Updates(nodeUpdates).Error; err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.Where("id = ? AND node_id = ? AND is_active = ?", req.ProtocolEndpointID, lockedNode.ID, true).First(&endpoint).Error; err != nil {
			return errProtocolEndpointUnavailable
		}

		err := tx.Where("node_id = ? AND report_id = ?", lockedNode.ID, req.ReportID).First(&record).Error
		if err == nil {
			duplicate = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var nonceRecord model.TrafficRecord
		err = tx.Where("node_id = ? AND nonce = ?", lockedNode.ID, authenticated.nonce).First(&nonceRecord).Error
		if err == nil {
			return errNodeReportNonceReplayed
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now().UTC()
		if err := expireSubscriptions(tx, req.UserID, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
			Where("subscriptions.user_id = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND node_group_endpoints.protocol_endpoint_id = ?", req.UserID, subStatusActive, now, endpoint.ID).
			Order("end_at desc").First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errSubscriptionNotFound
			}
			return err
		}

		remaining := sub.FlowTotal - sub.FlowUsed
		if remaining <= 0 {
			if err := tx.Model(&sub).Update("status", subStatusExpired).Error; err != nil {
				return err
			}
			quotaExhausted = true
			return nil
		}
		rawBytes := trafficBytesForMode(req.UploadBytes, req.DownloadBytes, sub.TrafficCalcMode)
		if rawBytes <= 0 {
			return errNoBillableTraffic
		}
		used, err := billedTrafficBytesChecked(rawBytes, endpoint.MultiplierMilli)
		if err != nil {
			return err
		}
		if used > remaining {
			used = remaining
		}
		sub.FlowUsed += used
		if sub.FlowUsed >= sub.FlowTotal {
			sub.Status = subStatusExpired
		}
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		if err := createQuotaEvent(tx, sub, "usage", -used, remaining, remaining-used, "traffic_report", req.ReportID); err != nil {
			return err
		}
		record = model.TrafficRecord{
			UserID:                  req.UserID,
			SubscriptionID:          sub.ID,
			NodeID:                  lockedNode.ID,
			ProtocolEndpointID:      endpoint.ID,
			ReportID:                req.ReportID,
			Nonce:                   authenticated.nonce,
			RawBytes:                rawBytes,
			UploadBytes:             req.UploadBytes,
			DownloadBytes:           req.DownloadBytes,
			TrafficCalcMode:         sub.TrafficCalcMode,
			ProtocolMultiplierMilli: endpoint.MultiplierMilli,
			UsedBytes:               used,
			At:                      authenticated.timestamp,
			Meta:                    req.Meta,
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, errNodeReportCredentialChanged):
			Unauthorized(w, "invalid node report authentication")
		case errors.Is(err, errNodeReportNonceReplayed):
			BadRequest(w, "report nonce was already used")
		case errors.Is(err, errSubscriptionNotFound):
			BadRequest(w, err.Error())
		case errors.Is(err, errSubscriptionQuotaExhausted):
			BadRequest(w, err.Error())
		case errors.Is(err, errProtocolEndpointUnavailable):
			BadRequest(w, err.Error())
		case errors.Is(err, errNoBillableTraffic):
			BadRequest(w, err.Error())
		default:
			ServerError(w, err)
		}
		return
	}
	if quotaExhausted {
		BadRequest(w, errSubscriptionQuotaExhausted.Error())
		return
	}

	response := map[string]interface{}{
		"report_id":                 record.ReportID,
		"subscription_id":           record.SubscriptionID,
		"node_id":                   record.NodeID,
		"protocol_endpoint_id":      record.ProtocolEndpointID,
		"raw_bytes":                 record.RawBytes,
		"upload_bytes":              record.UploadBytes,
		"download_bytes":            record.DownloadBytes,
		"traffic_calc_mode":         record.TrafficCalcMode,
		"protocol_multiplier_milli": record.ProtocolMultiplierMilli,
		"used_bytes":                record.UsedBytes,
		"duplicate":                 duplicate,
	}
	if !duplicate {
		response["flow_used"] = sub.FlowUsed
		response["flow_total"] = sub.FlowTotal
		response["flow_remaining"] = sub.FlowTotal - sub.FlowUsed
		response["subscription_end"] = sub.EndAt.Format(time.RFC3339)
	}
	OK(w, response)
}

func billedTrafficBytes(rawBytes, multiplierMilli int64) int64 {
	billed, _ := billedTrafficBytesChecked(rawBytes, multiplierMilli)
	return billed
}

func trafficBytesForMode(uploadBytes, downloadBytes int64, mode int16) int64 {
	switch mode {
	case trafficCalcUpload:
		return uploadBytes
	case trafficCalcDownload:
		return downloadBytes
	default:
		return uploadBytes + downloadBytes
	}
}

func billedTrafficBytesChecked(rawBytes, multiplierMilli int64) (int64, error) {
	if rawBytes <= 0 || multiplierMilli <= 0 {
		return 0, nil
	}
	value := big.NewInt(rawBytes)
	value.Mul(value, big.NewInt(multiplierMilli))
	value.Add(value, big.NewInt(999))
	value.Div(value, big.NewInt(1000))
	maxInt64 := new(big.Int).SetUint64(^uint64(0) >> 1)
	if value.Cmp(maxInt64) > 0 {
		return 0, errors.New("calculated traffic exceeds supported range")
	}
	return value.Int64(), nil
}

func newNodeReportSecret() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(random)
	return secret, secret[:12], nil
}

func decodeNodeConnectorHeartbeat(r *http.Request) (nodeConnectorHeartbeat, error) {
	if r.Body == nil {
		return nodeConnectorHeartbeat{}, errors.New("request body is required")
	}
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil {
		return nodeConnectorHeartbeat{}, errors.New("failed to read request body")
	}
	if len(rawBody) > nodeReportMaxBodyBytes {
		return nodeConnectorHeartbeat{}, errors.New("request body is too large")
	}
	var heartbeat nodeConnectorHeartbeat
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	if err := decoder.Decode(&heartbeat); err != nil {
		return nodeConnectorHeartbeat{}, errors.New("invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nodeConnectorHeartbeat{}, errors.New("request body must contain one JSON object")
	}
	heartbeat.NodeID = strings.TrimSpace(heartbeat.NodeID)
	if heartbeat.NodeID == "" {
		return nodeConnectorHeartbeat{}, errors.New("node_id is required")
	}
	return heartbeat, nil
}

func extractBearerToken(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(raw) < len("Bearer ") || !strings.EqualFold(raw[:len("Bearer ")], "Bearer ") {
		return "", errors.New("bearer authorization required")
	}
	token := strings.TrimSpace(raw[len("Bearer "):])
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", errors.New("invalid bearer authorization")
	}
	return token, nil
}

func (h *handlers) authenticateNodeConnector(r *http.Request, nodeID uint) (model.Node, error) {
	token, err := extractBearerToken(r)
	if err != nil {
		return model.Node{}, err
	}
	var node model.Node
	if err := h.db.Where("id = ? AND is_enabled = ?", nodeID, true).First(&node).Error; err != nil {
		return model.Node{}, errors.New("invalid node credential")
	}
	if node.NodeCredential == "" || node.NodeCredentialRevokedAt != nil {
		return model.Node{}, errors.New("invalid node credential")
	}
	expected, err := h.credentialCipher.Decrypt(node.NodeCredential)
	if err != nil || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return model.Node{}, errors.New("invalid node credential")
	}
	return node, nil
}

func writeNodeConnectorJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *handlers) authenticateNodeReport(r *http.Request, body []byte, now time.Time) (authenticatedNodeReport, error) {
	nodeIDHeader := strings.TrimSpace(r.Header.Get("X-Zboard-Node-ID"))
	nodeID, err := strconv.ParseUint(nodeIDHeader, 10, strconv.IntSize)
	if err != nil || nodeID == 0 || strconv.FormatUint(nodeID, 10) != nodeIDHeader {
		return authenticatedNodeReport{}, errors.New("invalid node id")
	}
	timestampHeader := strings.TrimSpace(r.Header.Get("X-Zboard-Timestamp"))
	timestamp, err := validateNodeReportTimestamp(timestampHeader, now)
	if err != nil {
		return authenticatedNodeReport{}, err
	}
	nonce := strings.TrimSpace(r.Header.Get("X-Zboard-Nonce"))
	if !validNodeReportIdentifier(nonce, 16, 64) {
		return authenticatedNodeReport{}, errors.New("invalid nonce")
	}
	signature, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Zboard-Signature")))
	if err != nil || len(signature) != sha256.Size {
		return authenticatedNodeReport{}, errors.New("invalid signature")
	}

	node, err := h.loadNode(uint(nodeID))
	if err != nil || node.TrafficSecret == "" || node.TrafficSecretRevokedAt != nil {
		return authenticatedNodeReport{}, errors.New("invalid node credential")
	}
	secret, err := h.credentialCipher.Decrypt(node.TrafficSecret)
	if err != nil {
		return authenticatedNodeReport{}, errors.New("invalid node credential")
	}
	expected := nodeReportSignature(secret, nodeIDHeader, timestampHeader, nonce, body)
	if !hmac.Equal(signature, expected) {
		return authenticatedNodeReport{}, errors.New("invalid signature")
	}
	return authenticatedNodeReport{node: node, timestamp: timestamp, nonce: nonce}, nil
}

func validateNodeReportTimestamp(value string, now time.Time) (time.Time, error) {
	unixSeconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || strconv.FormatInt(unixSeconds, 10) != value {
		return time.Time{}, errors.New("invalid timestamp")
	}
	timestamp := time.Unix(unixSeconds, 0).UTC()
	now = now.UTC().Truncate(time.Second)
	if timestamp.Before(now.Add(-nodeReportTimeWindow)) || timestamp.After(now.Add(nodeReportTimeWindow)) {
		return time.Time{}, errors.New("timestamp outside allowed window")
	}
	return timestamp, nil
}

func validNodeReportIdentifier(value string, minLength, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return false
	}
	return true
}

func nodeReportSignature(secret, nodeID, timestamp, nonce string, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	canonical := nodeID + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return mac.Sum(nil)
}

func (h *handlers) authFromRequest(r *http.Request) (authClaims, error) {
	raw := r.Header.Get("Authorization")
	if raw == "" {
		return authClaims{}, errors.New("authorization required")
	}
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = raw[len("bearer "):]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return authClaims{}, errors.New("authorization required")
	}

	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return authClaims{}, errors.New("invalid token")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return authClaims{}, errors.New("invalid token")
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return authClaims{}, errors.New("invalid token")
	}
	expect := h.sign(payloadBytes)
	if !hmac.Equal(sigBytes, expect) {
		return authClaims{}, errors.New("invalid token")
	}

	var payload authClaims
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return authClaims{}, errors.New("invalid token payload")
	}
	if payload.Expiry > 0 && payload.Expiry < time.Now().Unix() {
		return authClaims{}, errors.New("token expired")
	}
	if payload.UserID == 0 || payload.Email == "" {
		return authClaims{}, errors.New("invalid token payload")
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", payload.UserID, userStatusActive).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authClaims{}, errors.New("user not found")
		}
		return authClaims{}, errors.New("token validation failed")
	}
	payload.Email = user.Email
	payload.IsAdmin = user.IsAdmin
	return payload, nil
}

func (h *handlers) requireAdmin(w http.ResponseWriter, r *http.Request) (authClaims, error) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return authClaims{}, err
	}
	if !claims.IsAdmin {
		Forbidden(w, "admin required")
		return claims, errors.New("admin required")
	}
	return claims, nil
}

func createAuditLog(db *gorm.DB, claims authClaims, action, target, detail string) error {
	return db.Create(&model.AuditLog{
		UserID: auditUserID(claims.UserID),
		Actor:  claims.Email,
		Action: action,
		Target: target,
		Detail: detail,
	}).Error
}

func parsePagination(offsetValue, limitValue string) (int, int, error) {
	offset := 0
	limit := 50
	var err error
	if value := strings.TrimSpace(offsetValue); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
	}
	if value := strings.TrimSpace(limitValue); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 200 {
			return 0, 0, errors.New("limit must be an integer between 1 and 200")
		}
	}
	return offset, limit, nil
}

func wantsPagedList(r *http.Request) bool {
	return r != nil && r.URL != nil && r.URL.Query().Get("paged") == "true"
}

type pageMetadata struct {
	Offset         int     `json:"offset"`
	Limit          int     `json:"limit"`
	Total          int64   `json:"total"`
	NextCursor     *string `json:"next_cursor"`
	PreviousCursor *string `json:"previous_cursor"`
}

func pagedData(items interface{}, total int64, offset, limit int) map[string]interface{} {
	return map[string]interface{}{
		"items":      items,
		"page":       pageMetadata{Offset: offset, Limit: limit, Total: total},
		"aggregates": map[string]interface{}{},
		"facets":     map[string]interface{}{},
		// Compatibility fields remain until every shipped client consumes page.
		"total":  total,
		"offset": offset,
		"limit":  limit,
	}
}

func (h *handlers) loadNode(nodeID uint) (model.Node, error) {
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		return node, err
	}
	node.SSHPrivilegeConfigured = node.SSHPrivilegePassword != ""
	return node, nil
}

func (h *handlers) isProtocolSupported(proto string) bool {
	_, ok := supportedProtocols[strings.ToLower(proto)]
	return ok
}

type protocolKernelCapability struct {
	Supported          bool   `json:"supported"`
	Reason             string `json:"reason,omitempty"`
	MinimumZeroVersion string `json:"minimum_zero_version,omitempty"`
}

func (h *handlers) protocolKernelSupport(proto string) (bool, string) {
	protocol := strings.ToLower(strings.TrimSpace(proto))
	if !h.isProtocolSupported(protocol) {
		return false, "面板无法识别该协议。"
	}
	return true, ""
}

func (h *handlers) protocolKernelSupportForVersion(proto, zeroVersion string) (bool, string) {
	supported, reason := h.protocolKernelSupport(proto)
	if !supported {
		return supported, reason
	}
	if strings.EqualFold(strings.TrimSpace(proto), "mieru") && !zeroSupportsMieruPrincipal(zeroVersion) {
		return false, protocolKernelMieruUnavailableReason
	}
	if (strings.EqualFold(strings.TrimSpace(proto), "trojan") || strings.EqualFold(strings.TrimSpace(proto), "hysteria2")) &&
		!zeroSupportsNativeManagedAccess(zeroVersion) {
		return false, protocolKernelManagedUsersUnavailableReason
	}
	return true, ""
}

func (h *handlers) nodeKernelVersion(node model.Node) string {
	if node.KernelState != nil {
		if installed := strings.TrimSpace(node.KernelState.InstalledVersion); installed != "" {
			return installed
		}
	}
	if h.db != nil {
		var state model.NodeKernelState
		if err := h.db.Select("installed_version").First(&state, "node_id = ?", node.ID).Error; err == nil {
			if installed := strings.TrimSpace(state.InstalledVersion); installed != "" {
				return installed
			}
		}
	}
	return strings.TrimSpace(node.Version)
}

func (h *handlers) protocolKernelSupportForNode(proto string, node model.Node) (bool, string) {
	return h.protocolKernelSupportForVersion(proto, h.nodeKernelVersion(node))
}

func (h *handlers) protocolKernelCapabilities() map[string]protocolKernelCapability {
	capabilities := make(map[string]protocolKernelCapability, len(supportedProtocols))
	for protocol := range supportedProtocols {
		supported, reason := h.protocolKernelSupport(protocol)
		capability := protocolKernelCapability{Supported: supported, Reason: reason}
		if protocol == "mieru" {
			capability.MinimumZeroVersion = zeroMieruPrincipalSince
		} else if protocol == "trojan" || protocol == "hysteria2" {
			capability.MinimumZeroVersion = zeroNativeAccessSince
		}
		capabilities[protocol] = capability
	}
	return capabilities
}

func validateNodeProtocolConfigs(protocol, serverConfig, clientConfig string) error {
	fields := make(map[string]string)
	var serverObject map[string]interface{}
	if strings.TrimSpace(serverConfig) == "" || json.Unmarshal([]byte(serverConfig), &serverObject) != nil || serverObject == nil {
		fields["config"] = "服务端配置必须是 JSON 对象。"
	} else {
		serverType, ok := serverObject["type"].(string)
		if !ok || !strings.EqualFold(strings.TrimSpace(serverType), strings.TrimSpace(protocol)) {
			fields["config"] = "服务端配置的 type 必须与协议类型一致。"
		} else if strings.EqualFold(protocol, "mieru") && mieruEndpointPassword(serverObject) == "" {
			fields["config"] = "Mieru 服务端配置必须包含系统生成的用户凭据。"
		}
	}
	var clientObject map[string]interface{}
	if strings.TrimSpace(clientConfig) == "" || json.Unmarshal([]byte(clientConfig), &clientObject) != nil || clientObject == nil {
		fields["client_config"] = "客户端配置必须是 JSON 对象。"
	} else if strings.EqualFold(protocol, "mieru") {
		if clientType, _ := clientObject["type"].(string); !strings.EqualFold(strings.TrimSpace(clientType), "mieru") {
			fields["client_config"] = "Mieru 客户端配置的 type 必须为 mieru。"
		}
	}
	if len(fields) == 0 && strings.EqualFold(protocol, "vless") {
		if err := validateVLESSRealityConfigs(serverObject, clientObject); err != nil {
			return err
		}
	}
	if len(fields) == 0 {
		if err := validateProtocolTransportConfigs(protocol, serverObject, clientObject); err != nil {
			return err
		}
	}
	if len(fields) > 0 {
		return validationError("协议配置校验失败。", fields)
	}
	return nil
}

func validateProtocolTransportConfigs(protocol string, server, client map[string]interface{}) error {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol != "vless" && protocol != "vmess" {
		return nil
	}
	serverKind, err := selectableTransportKind(server)
	if err != nil {
		return validationError("协议配置校验失败。", map[string]string{"config": err.Error()})
	}
	clientKind, err := selectableTransportKind(client)
	if err != nil {
		return validationError("协议配置校验失败。", map[string]string{"client_config": err.Error()})
	}
	if serverKind != clientKind {
		return validationError("协议配置校验失败。", map[string]string{"client_config": "服务端与客户端必须使用相同的 TCP、WebSocket 或 gRPC 传输方式。"})
	}
	if protocol == "vless" && serverKind != "tcp" && (server["reality"] != nil || client["reality"] != nil) {
		return validationError("协议配置校验失败。", map[string]string{"config": "Zero 0.0.15 的 VLESS Reality 仅支持原始 TCP。"})
	}
	switch serverKind {
	case "ws":
		serverPath, serverHeaders, err := websocketTransportFields(server["ws"])
		if err != nil {
			return validationError("协议配置校验失败。", map[string]string{"config": err.Error()})
		}
		clientPath, clientHeaders, err := websocketTransportFields(client["ws"])
		if err != nil {
			return validationError("协议配置校验失败。", map[string]string{"client_config": err.Error()})
		}
		if serverPath != clientPath || serverHeaders != clientHeaders {
			return validationError("协议配置校验失败。", map[string]string{"client_config": "WebSocket 路径和请求头必须与服务端一致。"})
		}
	case "grpc":
		serverNames, err := grpcTransportServiceNames(server["grpc"])
		if err != nil {
			return validationError("协议配置校验失败。", map[string]string{"config": err.Error()})
		}
		clientNames, err := grpcTransportServiceNames(client["grpc"])
		if err != nil {
			return validationError("协议配置校验失败。", map[string]string{"client_config": err.Error()})
		}
		if strings.Join(serverNames, "\x00") != strings.Join(clientNames, "\x00") {
			return validationError("协议配置校验失败。", map[string]string{"client_config": "gRPC Service Name 必须与服务端一致。"})
		}
	}
	return nil
}

func selectableTransportKind(config map[string]interface{}) (string, error) {
	hasWS := config["ws"] != nil
	hasGRPC := config["grpc"] != nil
	if hasWS && hasGRPC {
		return "", errors.New("WebSocket 与 gRPC 不能同时启用。")
	}
	if hasWS {
		return "ws", nil
	}
	if hasGRPC {
		return "grpc", nil
	}
	return "tcp", nil
}

func websocketTransportFields(value interface{}) (string, string, error) {
	config, ok := value.(map[string]interface{})
	if !ok {
		return "", "", errors.New("WebSocket 配置必须是 JSON 对象。")
	}
	path := "/"
	if configured, exists := config["path"]; exists {
		path, ok = configured.(string)
		if !ok || !strings.HasPrefix(strings.TrimSpace(path), "/") {
			return "", "", errors.New("WebSocket 路径必须以 / 开头。")
		}
		path = strings.TrimSpace(path)
	}
	headers := map[string]interface{}{}
	if configured, exists := config["headers"]; exists {
		headers, ok = configured.(map[string]interface{})
		if !ok {
			return "", "", errors.New("WebSocket headers 必须是 JSON 对象。")
		}
	}
	payload, _ := json.Marshal(headers)
	return path, string(payload), nil
}

func grpcTransportServiceNames(value interface{}) ([]string, error) {
	config, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New("gRPC 配置必须是 JSON 对象。")
	}
	value, exists := config["service_names"]
	if !exists {
		value, exists = config["service_name"]
	}
	if !exists {
		return nil, errors.New("gRPC 配置必须包含 service_names。")
	}
	var names []string
	switch typed := value.(type) {
	case string:
		names = []string{typed}
	case []interface{}:
		for _, item := range typed {
			name, ok := item.(string)
			if !ok {
				return nil, errors.New("gRPC service_names 必须是字符串或字符串数组。")
			}
			names = append(names, name)
		}
	default:
		return nil, errors.New("gRPC service_names 必须是字符串或字符串数组。")
	}
	if len(names) == 0 {
		return nil, errors.New("gRPC service_names 不能为空。")
	}
	for index := range names {
		names[index] = strings.TrimSpace(names[index])
		if names[index] == "" {
			return nil, errors.New("gRPC Service Name 不能为空。")
		}
	}
	return names, nil
}

func validateOptionalJSONObject(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil || decoded == nil {
		return validationError("JSON 配置校验失败。", map[string]string{name: "请输入有效的 JSON 对象。"})
	}
	return nil
}

func validateOptionalJSONArray(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded []interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil || decoded == nil {
		return validationError("JSON 配置校验失败。", map[string]string{name: "请输入有效的 JSON 数组。"})
	}
	return nil
}

func normalizeOptionalJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (h *handlers) validateProtocolParent(endpointID, nodeID uint, parentID *uint) error {
	if parentID == nil || *parentID == 0 {
		return nil
	}
	if endpointID != 0 && *parentID == endpointID {
		return validationError("协议服务校验失败。", map[string]string{"parent_protocol_id": "协议不能将自身设为父协议。"})
	}
	visited := map[uint]struct{}{}
	if endpointID != 0 {
		visited[endpointID] = struct{}{}
	}
	current := *parentID
	for depth := 0; depth < 128 && current != 0; depth++ {
		if _, exists := visited[current]; exists {
			return validationError("协议服务校验失败。", map[string]string{"parent_protocol_id": "父协议关系不能形成循环。"})
		}
		visited[current] = struct{}{}
		var parent model.ProtocolEndpoint
		if err := h.db.Select("id", "node_id", "parent_protocol_id").First(&parent, current).Error; err != nil {
			return validationError("协议服务校验失败。", map[string]string{"parent_protocol_id": "所选父协议不存在。"})
		}
		if parent.NodeID != nodeID {
			return validationError("协议服务校验失败。", map[string]string{"parent_protocol_id": "父协议必须与当前服务属于同一节点。"})
		}
		if parent.ParentProtocolID == nil {
			return nil
		}
		current = *parent.ParentProtocolID
	}
	if current != 0 {
		return validationError("协议服务校验失败。", map[string]string{"parent_protocol_id": "父协议层级过深。"})
	}
	return nil
}

func (h *handlers) validateNodeSSH(node model.Node) error {
	authMethod := normalizeSSHAuthMethod(node.SSHAuthMethod)
	if err := validateSSHFields(node.SSHHost, node.SSHPort, node.SSHUser, authMethod, node.SSHPwd, node.SSHHostKeyFingerprint); err != nil {
		return err
	}
	credential, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return fmt.Errorf("node ssh credential is unavailable: %w", err)
	}
	if authMethod == sshAuthPrivateKey {
		passphrase, err := h.credentialCipher.Decrypt(node.SSHPrivateKeyPassphrase)
		if err != nil {
			return fmt.Errorf("node ssh private key passphrase is unavailable: %w", err)
		}
		if _, err := parseSSHPrivateKey(credential, passphrase); err != nil {
			return err
		}
	}
	privilegePassword, err := h.credentialCipher.Decrypt(node.SSHPrivilegePassword)
	if err != nil {
		return fmt.Errorf("node ssh privilege password is unavailable: %w", err)
	}
	if err := validateSSHPrivilege(node.SSHPrivilegeMode, privilegePassword); err != nil {
		return err
	}
	return nil
}

func (h *handlers) execSSHCommand(node model.Node, command string) (string, time.Duration, error) {
	return h.execSSHCommandWithPrivilege(node, command, false)
}

func (h *handlers) execSSHCommandWithPrivilege(node model.Node, command string, privileged bool) (string, time.Duration, error) {
	start := time.Now()
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return "", time.Since(start), err
	}
	defer conn.Close()

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
	return string(bytes.TrimSpace(out)), time.Since(start), err
}

func (h *handlers) prepareSSHCommand(node model.Node, command string, privileged bool) (string, string, bool, error) {
	if !privileged || normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) == sshPrivilegeNone {
		return command, "", false, nil
	}
	password, err := h.credentialCipher.Decrypt(node.SSHPrivilegePassword)
	if err != nil {
		return "", "", false, fmt.Errorf("decrypt node privilege password: %w", err)
	}
	switch normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) {
	case sshPrivilegeSudo:
		if password == "" {
			return "sudo -n -- sh -c " + shellQuote(command), "", false, nil
		}
		return "sudo -S -p '' -- sh -c " + shellQuote(command), password + "\n", false, nil
	case sshPrivilegeSU:
		if password == "" {
			return "", "", false, errors.New("node privilege password is required for su")
		}
		return "su root -c " + shellQuote(command), password + "\n", true, nil
	default:
		return "", "", false, errors.New("unsupported node privilege mode")
	}
}

func (h *handlers) dialNodeSSH(node model.Node) (*ssh.Client, time.Duration, error) {
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
	addr := fmt.Sprintf("%s:%d", strings.TrimSpace(node.SSHHost), node.SSHPort)
	conf := &ssh.ClientConfig{
		User:            strings.TrimSpace(node.SSHUser),
		Auth:            []ssh.AuthMethod{authMethod},
		Timeout:         12 * time.Second,
		HostKeyCallback: verifiedHostKeyCallback(node.SSHHostKeyFingerprint, &observedFingerprint),
	}
	conn, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return nil, time.Since(start), err
	}
	if err := h.pinSSHHostKey(node.ID, node.SSHHostKeyFingerprint, observedFingerprint); err != nil {
		_ = conn.Close()
		return nil, time.Since(start), err
	}
	return conn, time.Since(start), nil
}

func (h *handlers) pinSSHHostKey(nodeID uint, expectedFingerprint string, observedFingerprint string) error {
	expected := strings.TrimSpace(expectedFingerprint)
	observed := strings.TrimSpace(observedFingerprint)
	if expected != "" {
		var stored string
		if err := h.db.Model(&model.Node{}).Select("ssh_host_key_fingerprint").Where("id = ?", nodeID).Scan(&stored).Error; err != nil {
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
	result := h.db.Model(&model.Node{}).
		Where("id = ? AND (ssh_host_key_fingerprint IS NULL OR ssh_host_key_fingerprint = '')", nodeID).
		Update("ssh_host_key_fingerprint", observed)
	if result.Error != nil {
		return fmt.Errorf("record SSH host key: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var stored string
	if err := h.db.Model(&model.Node{}).Select("ssh_host_key_fingerprint").Where("id = ?", nodeID).Scan(&stored).Error; err != nil {
		return fmt.Errorf("read recorded SSH host key: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(stored)), []byte(observed)) == 1 {
		return nil
	}
	return fmt.Errorf("SSH host key changed while it was being recorded: expected %s, received %s; verify the VPS identity before resetting trust", stored, observed)
}

func normalizeSSHAuthMethod(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return sshAuthPassword
	}
	return normalized
}

func normalizeSSHPrivilegeMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == "direct" {
		return sshPrivilegeNone
	}
	return normalized
}

func validateSSHPrivilege(mode string, password string) error {
	switch normalizeSSHPrivilegeMode(mode) {
	case sshPrivilegeNone:
		return nil
	case sshPrivilegeSudo:
		return nil // An empty password explicitly selects passwordless sudo.
	case sshPrivilegeSU:
		if password == "" {
			return validationError("SSH 配置校验失败。", map[string]string{"ssh_privilege_password": "使用 su 提权时必须提供 root 密码。"})
		}
		return nil
	default:
		return validationError("SSH 配置校验失败。", map[string]string{"ssh_privilege_mode": "请选择有效的系统提权方式。"})
	}
}

func validateSSHFields(host string, port int, user string, authMethod string, credential string, fingerprint string) error {
	fields := make(map[string]string)
	if strings.TrimSpace(host) == "" {
		fields["ssh_host"] = "请输入 SSH 主机。"
	}
	if strings.TrimSpace(user) == "" {
		fields["ssh_user"] = "请输入 SSH 用户。"
	}
	if port <= 0 || port > 65535 {
		fields["ssh_port"] = "端口必须在 1–65535 之间。"
	}
	if authMethod != sshAuthPassword && authMethod != sshAuthPrivateKey {
		fields["ssh_auth_method"] = "请选择密码或私钥认证。"
	}
	if strings.TrimSpace(credential) == "" {
		field := "ssh_password"
		if authMethod == sshAuthPrivateKey {
			field = "ssh_private_key"
		}
		fields[field] = "请输入 SSH 登录凭证。"
	}
	if strings.TrimSpace(fingerprint) != "" {
		if err := validateSSHHostKeyFingerprint(fingerprint); err != nil {
			fields["ssh_host"] = "已保存的主机身份无效，请确认目标后重新信任主机。"
		}
	}
	if len(fields) > 0 {
		return validationError("SSH 配置校验失败。", fields)
	}
	return nil
}

func parseSSHPrivateKey(privateKey string, passphrase string) (ssh.Signer, error) {
	if strings.TrimSpace(privateKey) == "" {
		return nil, validationError("SSH 配置校验失败。", map[string]string{"ssh_private_key": "请输入 SSH 私钥。"})
	}
	var (
		signer ssh.Signer
		err    error
	)
	if passphrase == "" {
		signer, err = ssh.ParsePrivateKey([]byte(privateKey))
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	}
	if err != nil {
		return nil, validationError("SSH 配置校验失败。", map[string]string{"ssh_private_key": "私钥格式或口令无效。"})
	}
	return signer, nil
}

func validateSSHHostKeyFingerprint(fingerprint string) error {
	normalized := strings.TrimSpace(fingerprint)
	if !strings.HasPrefix(normalized, "SHA256:") {
		return errors.New("node ssh_host_key_fingerprint must use SHA256 format")
	}
	encoded := strings.TrimPrefix(normalized, "SHA256:")
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("node ssh_host_key_fingerprint is invalid")
	}
	return nil
}

func verifiedHostKeyCallback(expectedFingerprint string, observedFingerprint *string) ssh.HostKeyCallback {
	expected := strings.TrimSpace(expectedFingerprint)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		actual := ssh.FingerprintSHA256(key)
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

func (h *handlers) issueToken(claims authClaims) (string, int64, error) {
	claims.Expiry = time.Now().Add(24 * time.Hour).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", 0, err
	}
	sig := h.sign(payload)
	token := fmt.Sprintf("%s.%s", base64.RawURLEncoding.EncodeToString(payload), base64.RawURLEncoding.EncodeToString(sig))
	return token, claims.Expiry, nil
}

func (h *handlers) sign(payload []byte) []byte {
	m := hmac.New(sha256.New, []byte(h.jwtSecret))
	_, _ = m.Write(payload)
	return m.Sum(nil)
}

func decodeBody(r *http.Request, out interface{}) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func parseOrderID(path string) (uint, error) {
	normalized := strings.TrimRight(path, "/")
	parts := strings.Split(normalized, "/")
	if len(parts) < 2 {
		return 0, errors.New("invalid order callback path")
	}
	raw := parts[len(parts)-2]
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errors.New("invalid order id")
	}
	return uint(parsed), nil
}

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parsePathID(path string, prefix string) (uint, error) {
	normalized := strings.Trim(path, "/")
	normalizedPrefix := strings.Trim(prefix, "/")
	pathPrefix := normalizedPrefix + "/"
	if !strings.HasPrefix(normalized, pathPrefix) {
		return 0, errors.New("invalid path")
	}

	value := strings.Trim(strings.TrimPrefix(normalized, pathPrefix), "/")
	if strings.Contains(value, "/") {
		value = strings.SplitN(value, "/", 2)[0]
	}
	if value == "" {
		return 0, errors.New("missing id")
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("invalid id")
	}
	return uint(parsed), nil
}

func toPublicUser(user model.User) userPublic {
	return userPublic{
		ID: user.ID, Email: user.Email,
		IsAdmin: user.IsAdmin, Status: user.Status,
	}
}
