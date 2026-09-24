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
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	sshadapter "github.com/zerodenet/zboard/backend/internal/adapters/ssh"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
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

var perpetualSubscriptionEnd = entitlements.PerpetualEnd

var (
	errNodeReportCredentialChanged = metering.ErrNodeReportCredentialChanged
	errNodeReportNonceReplayed     = metering.ErrNodeReportNonceReplayed
	errSubscriptionNotFound        = metering.ErrSubscriptionNotFound
	errSubscriptionQuotaExhausted  = errors.New("subscription quota exhausted")
	errOrderNotPayable             = errors.New("order not payable")
	errOrderNotCancelable          = errors.New("order not cancelable")
	errOrderTransitionRejected     = errors.New("order status transition rejected")
	errProtocolEndpointUnavailable = metering.ErrProtocolEndpointUnavailable
	errNoBillableTraffic           = metering.ErrNoBillableTraffic
)

var supportedProtocols = map[string]struct{}{
	"vmess":       {},
	"vless":       {},
	"trojan":      {},
	"shadowsocks": {},
	"hysteria2":   {},
	"mieru":       {},
}

type authClaims = identity.SessionClaims

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

type adminSubscriptionListItem = entitlements.SubscriptionSummary

func effectiveSubscriptionStatus(sub model.Subscription, now time.Time) string {
	return entitlements.EffectiveStatus(entitlements.Subscription(sub), now)
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

type planUpdateReq = commerce.PlanUpdateRequest

type nodeGroupCreateReq struct {
	Name                string `json:"name"`
	Code                string `json:"code"`
	Description         string `json:"description"`
	IsEnabled           *bool  `json:"is_enabled"`
	ProtocolEndpointIDs []uint `json:"protocol_endpoint_ids"`
	NetworkEntryIDs     []uint `json:"network_entry_ids"`
}

type nodeGroupUpdateReq struct {
	Name                *string `json:"name"`
	Code                *string `json:"code"`
	Description         *string `json:"description"`
	IsEnabled           *bool   `json:"is_enabled"`
	ProtocolEndpointIDs *[]uint `json:"protocol_endpoint_ids"`
	NetworkEntryIDs     *[]uint `json:"network_entry_ids"`
	ExpectedRevision    *uint64 `json:"expected_revision"`
}

type planSKUReq = commerce.LegacySKURequest

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

type trafficReconciliationItem = metering.ReconciliationItem
type trafficReconciliationAggregates = metering.ReconciliationAggregates

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

type trafficRecordAggregates = metering.RecordAggregates

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
	EgressConfig               string                                      `json:"egress_config"`
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
	NetworkEntryID      uint            `json:"network_entry_id,omitempty"`
	NetworkEntryNetwork string          `json:"network_entry_network,omitempty"`
	ID                  uint            `json:"id"`
	NodeID              uint            `json:"node_id"`
	SubscriptionID      uint            `json:"subscription_id,omitempty"`
	CredentialID        string          `json:"credential_id,omitempty"`
	Name                string          `json:"name"`
	Region              string          `json:"region"`
	Address             string          `json:"address"`
	Port                int             `json:"port"`
	PublicPort          int             `json:"public_port"`
	Protocol            string          `json:"protocol"`
	MultiplierMilli     int64           `json:"multiplier_milli"`
	Config              json.RawMessage `json:"config"`
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
	services                *application.Services
	jobRuntimeOnce          sync.Once
	jobRuntime              *backgroundJobs
	identityProviders       identityRuntime
	externalAuth            externalAuthState
	pluginManager           *plugins.Manager
	db                      *gorm.DB
	jwtSecret               string
	credentialCipher        *security.CredentialCipher
	zeroArtifactDir         string
	zeroNativeAccess        bool
	zeroMieruAccess         bool
	zeroLocalVersion        string
	sshTerminal             *sshTerminalRuntime
	nodePublishLocks        sync.Map
	zeroEventAuthCache      sync.Map
	zeroEventAuthFailures   sync.Map
	trafficStatisticsCache  trafficSnapshotCache[trafficUsageStatistics]
	trafficIncrementalStats *meteringstore.IncrementalCache
	trafficTrendsCache      trafficSnapshotCache[trafficTrendSnapshot]
	expiryReconcileMu       sync.Mutex
	lastExpiryReconcile     time.Time
	deletionMu              sync.Mutex
}

func NewHandlers(services *application.Services, db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher, zeroArtifactDir, zeroKernelContract, zeroLocalVersion string) (*handlers, error) {
	if err := cfgpkg.ValidateJWTSecret(jwtSecret); err != nil {
		return nil, err
	}
	if credentialCipher == nil {
		return nil, errors.New("credential cipher is required")
	}
	if services == nil || !services.Matches(db, jwtSecret) {
		return nil, errors.New("matching application services are required")
	}
	normalizedKernelContract := strings.ToLower(strings.TrimSpace(zeroKernelContract))
	localVersion := strings.TrimSpace(zeroLocalVersion)
	nativeContract := normalizedKernelContract == cfgpkg.ZeroKernelNativeLocal || normalizedKernelContract == cfgpkg.ZeroKernelNativeMieru
	return &handlers{
		services:         services,
		db:               db,
		jwtSecret:        jwtSecret,
		credentialCipher: credentialCipher,
		zeroArtifactDir:  strings.TrimSpace(zeroArtifactDir),
		zeroNativeAccess: nativeContract,
		zeroMieruAccess:  nativeContract,
		zeroLocalVersion: localVersion,
		sshTerminal:      newSSHTerminalRuntime(),
	}, nil
}

func (h *handlers) HealthHandler(w http.ResponseWriter, r *http.Request) {
	OK(w, map[string]interface{}{
		"service": "zboard",
		"ready":   true,
	})
}

func (h *handlers) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.services.DatabaseReady(r.Context()); err != nil {
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
	state, err := h.services.InstallationState(r.Context(), false)
	if err != nil {
		ServerError(w, err)
		return
	}
	data := map[string]interface{}{"installed": state.Installed, "version": version.FullVersion()}
	if state.Installed {
		data["site_name"] = state.SiteName
		data["site_url"] = state.SiteURL
		data["allow_registration"] = state.AllowRegistration
	}
	OK(w, data)
}
func (h *handlers) SetupInstallHandler(w http.ResponseWriter, r *http.Request) {
	var body setupRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.installPlatform(w, r, platform.InstallationInput{SiteName: body.SiteName, SiteURL: body.SiteURL, AllowRegistration: body.AllowRegistration, AdminEmail: body.AdminEmail, AdminPassword: body.AdminPassword}, false)
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
	view, err := h.services.SiteSettings.Update(r.Context(), claims.UserID, platform.SiteSettingsInput{SiteName: body.SiteName, SiteURL: body.SiteURL, AllowRegistration: body.AllowRegistration})
	var invalid *platform.SiteSettingsValidation
	if errors.As(err, &invalid) {
		BadRequestError(w, validationError(invalid.Error(), invalid.Fields))
		return
	}
	if errors.Is(err, platform.ErrSettingsPermission) {
		Forbidden(w, "settings administrator authorization changed")
		return
	}
	if errors.Is(err, platform.ErrMaintenanceBusy) {
		ServiceUnavailable(w, "database migration locks settings changes")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, view)
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
		installation, err := h.services.InstallationState(r.Context(), false)
		if err != nil {
			ServerError(w, err)
			return
		}
		if !installation.Installed {
			writeJSON(w, http.StatusPreconditionRequired, "zboard installation is required", map[string]string{"setup_url": "/setup"})
			return
		}
		state, err := h.loadMaintenanceState(false)
		if err != nil {
			ServerError(w, err)
			return
		}
		writeLocked := state.MigrationInProgress || state.MigrationCutoverPending
		unsafeMethod := (r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions) || r.URL.Path == externalAuthPath+"/callback"
		cutoverConfirmation := state.MigrationCutoverPending && r.Method == http.MethodPut && r.URL.Path == "/api/v1/admin/maintenance"
		if writeLocked && unsafeMethod && r.URL.Path != "/api/v1/auth/login" && !cutoverConfirmation {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusServiceUnavailable, "database migration locks all writes until cutover is confirmed", map[string]interface{}{"maintenance": state})
			return
		}
		if state.Enabled && !maintenanceRouteAllowedWithoutAdmin(r.URL.Path) {
			claims, authErr := h.authFromRequest(r)
			if authErr != nil || !claims.IsAdmin {
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusServiceUnavailable, state.Title, map[string]interface{}{"maintenance": state})
				return
			}
		}
		next(w, r)
	}
}

func validateSetupRequest(body *setupRequest) error {
	in, err := platform.NormalizeInstallation(platform.InstallationInput{SiteName: body.SiteName, SiteURL: body.SiteURL, AdminEmail: body.AdminEmail, AdminPassword: body.AdminPassword, AllowRegistration: body.AllowRegistration})
	if err != nil {
		var invalid *platform.InstallationValidation
		if errors.As(err, &invalid) {
			return validationError(invalid.Error(), invalid.Fields)
		}
		return err
	}
	body.SiteName, body.SiteURL, body.AdminEmail = in.SiteName, in.SiteURL, in.AdminEmail
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
	return identity.NormalizeEmail(value)
}

func validEmail(value string) bool { return identity.ValidEmail(value) }

func validPassword(value string) bool {
	return identity.ValidPassword(value)
}

func (h *handlers) SystemInfoHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.services.DatabaseReady(r.Context()); err != nil {
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

func (h *handlers) AdminUsersListHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	query := identity.AccountDirectoryQuery{}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !h.isValidUserStatus(status) {
			BadRequest(w, "invalid user status")
			return
		}
		query.Status = status
	}

	if isAdmin := strings.TrimSpace(r.URL.Query().Get("is_admin")); isAdmin != "" {
		flag, parseErr := strconv.ParseBool(isAdmin)
		if parseErr != nil {
			BadRequest(w, "invalid is_admin")
			return
		}
		query.IsAdmin = &flag
	}

	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		if len(q) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		query.Search = q
	}

	query.Paged = wantsPagedList(r)
	offset, limit := 0, 50
	if query.Paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		query.Offset, query.Limit = offset, limit
	}
	query.Sort = strings.TrimSpace(r.URL.Query().Get("sort"))
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "asc") {
		query.Direction = "asc"
	}
	result, err := h.services.Identity.Directory.List(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	if query.Paged {
		items := make([]adminUserListItem, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, adminUserListItem{userPublic: userPublic(item.PublicAccount), ActiveSubscriptionCount: item.ActiveSubscriptionCount, TotalSubscriptionCount: item.TotalSubscriptionCount, PendingOrderCount: item.PendingOrderCount, TotalOrderCount: item.TotalOrderCount, CreatedAt: item.CreatedAt})
		}
		OK(w, pagedData(items, result.Total, offset, limit))
		return
	}
	publicUsers := make([]userPublic, 0, len(result.Items))
	for _, item := range result.Items {
		publicUsers = append(publicUsers, userPublic(item.PublicAccount))
	}
	OK(w, publicUsers)
}

func (h *handlers) isValidUserStatus(status string) bool { return identity.ValidStatus(status) }

func (h *handlers) isValidOrderStatus(status string) bool {
	_, ok := commerce.OrderStatuses(status, false)
	return ok
}
func (h *handlers) orderListStatusValues(status string, adminScope bool) ([]string, bool) {
	return commerce.OrderStatuses(status, adminScope)
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
	return commerce.OrderTransitionAllowed(current, target, force)
}

func (h *handlers) NodeListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	if r.URL.Query().Get("paged") == "true" {
		h.nodePage(w, r)
		return
	}

	cutoff := time.Now().UTC().Add(-nodeOnlineWindow)
	page, err := h.services.NetworkInventory.Nodes(r.Context(), networkcap.NodeInventoryQuery{OnlineCutoff: cutoff})
	if err != nil {
		ServerError(w, err)
		return
	}
	nodes := make([]model.Node, 0, len(page.Items))
	for _, item := range page.Items {
		nodes = append(nodes, nodeInventoryModel(item))
	}
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
	query := networkcap.NodeInventoryQuery{Paged: true, Offset: offset, Limit: limit}
	if rawID := strings.TrimSpace(r.URL.Query().Get("node_id")); rawID != "" {
		nodeID, parseErr := strconv.ParseUint(rawID, 10, 64)
		if parseErr != nil || nodeID == 0 {
			BadRequest(w, "invalid node_id")
			return
		}
		query.ID = uint(nodeID)
	}
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		query.Search = search
	}
	if region := strings.TrimSpace(r.URL.Query().Get("region")); region != "" {
		query.Region = region
	}
	if lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle_status")); lifecycle != "" {
		switch lifecycle {
		case "active", "maintenance", "retired", resourceStatusDeleting:
			query.LifecycleStatus = lifecycle
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
		query.Enabled = &enabled
	}
	cutoff := time.Now().UTC().Add(-nodeOnlineWindow)
	query.OnlineCutoff = cutoff
	if online := strings.TrimSpace(r.URL.Query().Get("connector_online")); online != "" {
		switch online {
		case "true":
			value := true
			query.ConnectorOnline = &value
		case "false":
			value := false
			query.ConnectorOnline = &value
		default:
			BadRequest(w, "invalid connector_online")
			return
		}
	}
	if kernelStatus := strings.TrimSpace(r.URL.Query().Get("kernel_status")); kernelStatus != "" {
		query.KernelStatus = kernelStatus
	}
	query.Sort = strings.TrimSpace(r.URL.Query().Get("sort"))
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "asc") {
		query.Direction = "asc"
	}
	page, err := h.services.NetworkInventory.Nodes(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]nodeListItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, newNodeListItem(nodeInventoryModel(item), item.EnabledProtocolCount, cutoff))
	}
	OK(w, pagedData(items, page.Total, offset, limit))
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
	item, err := h.services.NetworkInventory.Node(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, networkcap.ErrInventoryNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, newNodeDetailItem(nodeInventoryModel(item), item.EnabledProtocolCount, time.Now().UTC().Add(-nodeOnlineWindow)))
}

func nodeInventoryModel(item networkcap.NodeInventoryItem) model.Node {
	r := item.Node
	node := model.Node{ID: r.ID, Name: r.Name, Region: r.Region, Address: r.Address, NodeCredentialPrefix: r.NodeCredentialPrefix, NodeCredentialRevokedAt: r.NodeCredentialRevokedAt, CommunicationProtocol: r.CommunicationProtocol, Status: r.Status, LifecycleStatus: r.LifecycleStatus, Config: r.Config, IsEnabled: r.IsEnabled, Remark: r.Remark, IsOnline: r.IsOnline, LastSeenAt: r.LastSeenAt, LastSyncAt: r.LastSyncAt, Version: r.Version, SSHHost: r.SSHHost, SSHPort: r.SSHPort, SSHUser: r.SSHUser, SSHAuthMethod: r.SSHAuthMethod, SSHPrivilegeMode: r.SSHPrivilegeMode, SSHPrivilegeConfigured: r.SSHPrivilegeConfigured, SSHHostKeyFingerprint: r.SSHHostKeyFingerprint, SSHVerifiedAt: r.SSHVerifiedAt, ConnectorLastSeenAt: r.ConnectorLastSeenAt, ConnectorOnline: r.ConnectorOnline, UptimeSeconds: r.UptimeSeconds, ActiveFlows: r.ActiveFlows, BytesUp: r.BytesUp, BytesDown: r.BytesDown, TrafficSecretPrefix: r.TrafficSecretPrefix, TrafficSecretRevokedAt: r.TrafficSecretRevokedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if item.KernelState != nil {
		k := item.KernelState
		node.KernelState = &model.NodeKernelState{NodeID: k.NodeID, Status: k.Status, Phase: k.Phase, RecommendedAction: k.RecommendedAction, PlatformOS: k.PlatformOS, Architecture: k.Architecture, Libc: k.Libc, DesiredVersion: k.DesiredVersion, InstalledVersion: k.InstalledVersion, DesiredSHA256: k.DesiredSHA256, InstalledSHA256: k.InstalledSHA256, DesiredConfigSHA256: k.DesiredConfigSHA256, AppliedConfigSHA256: k.AppliedConfigSHA256, ServiceStatus: k.ServiceStatus, ControlStatus: k.ControlStatus, LastError: k.LastError, ActiveOperationID: k.ActiveOperationID, LastDetectedAt: k.LastDetectedAt, LastHealthyAt: k.LastHealthyAt, CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt}
	}
	return node
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
	node, err := h.services.NodeAdministration.Create(r.Context(), claims.UserID, networkcap.NodeCreateChange{
		Name: req.Name, Region: req.Region, Address: req.Address,
		NodeCredentialCiphertext: encryptedNodeCredential, NodeCredentialPrefix: nodeCredentialPrefix,
		CommunicationProtocol: req.CommunicationProtocol, Config: normalizeOptionalJSON(req.Config, "{}"),
		IsEnabled: isEnabled, Remark: req.Remark, SSHHost: req.SSHHost, SSHPort: req.SSHPort,
		SSHUser: req.SSHUser, SSHAuthMethod: req.SSHAuthMethod, SSHPwdCiphertext: encryptedCredential,
		SSHPassphraseCiphertext: encryptedPassphrase, SSHPrivilegeMode: req.SSHPrivilegeMode,
		SSHPrivilegeCiphertext: encryptedPrivilegePassword,
	})
	if err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
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
	node, err := h.services.NodeAdministration.Update(r.Context(), claims.UserID, networkcap.NodeUpdateRequest{
		ID: nodeID, Name: req.Name, Region: req.Region, Address: req.Address, Remark: req.Remark,
		LifecycleStatus: req.LifecycleStatus, IsEnabled: req.IsEnabled,
	})
	if err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, node)
}

func (h *handlers) NodeSSHTestHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
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
	updated, saveErr := h.services.NodeAdministration.RecordSSHVerification(r.Context(), claims.UserID, node.ID, now, execErr == nil)
	if saveErr != nil {
		if writeNodeAdministrationError(w, saveErr) {
			return
		}
		ServerError(w, saveErr)
		return
	}
	if execErr != nil {
		BadRequest(w, execErr.Error())
		return
	}

	OK(w, map[string]interface{}{
		"node":       updated,
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

	updated, err := h.services.NodeAdministration.SaveSSH(r.Context(), claims.UserID, node.ID, networkcap.NodeSSHConfigurationChange{
		SSHHost: req.SSHHost, SSHPort: req.SSHPort, SSHUser: req.SSHUser, SSHAuthMethod: authMethod,
		SSHPwdCiphertext: encryptedCredential, SSHPassphraseCiphertext: encryptedPassphrase,
		SSHPrivilegeMode: privilegeMode, SSHPrivilegeCiphertext: encryptedPrivilegePassword, ResetHostKey: targetChanged,
	})
	if err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, updated)
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
	if err := h.services.NodeAdministration.ResetSSHHostKey(r.Context(), claims.UserID, nodeID); err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"node_id": nodeID, "host_key_trust_reset": true})
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
	if err := h.services.NodeAdministration.RotateCredential(r.Context(), claims.UserID, nodeID, networkcap.NodeCredentialConnector, networkcap.NodeCredentialChange{Ciphertext: encryptedAPIKey, Prefix: prefix}); err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
		ServerError(w, err)
		return
	}
	h.invalidateZeroEventCredential(nodeID)
	OK(w, map[string]interface{}{
		"node_id":        strconv.FormatUint(uint64(nodeID), 10),
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
	if err := h.services.NodeAdministration.RevokeCredential(r.Context(), claims.UserID, nodeID, networkcap.NodeCredentialConnector); err != nil {
		if writeNodeAdministrationError(w, err) {
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
	activity := networkcap.NodeActivityUpdate{
		At: now, Online: true, ConnectorSeen: true, UptimeSeconds: &heartbeat.UptimeSeconds,
		ActiveFlows: &heartbeat.ActiveFlows, BytesUp: &heartbeat.BytesUp, BytesDown: &heartbeat.BytesDown,
	}
	if heartbeat.BuildID != "" {
		activity.Version = &heartbeat.BuildID
	}
	if err := h.services.NodeActivity.Record(r.Context(), node.ID, node.NodeCredential, activity); err != nil {
		if !errors.Is(err, networkcap.ErrNodeActivityCredential) {
			ServerError(w, err)
			return
		}
		Unauthorized(w, "invalid node connector authentication")
		return
	}
	h.StartCredentialExpiryWorker()
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

	err = h.services.NodeAdministration.RotateCredential(r.Context(), claims.UserID, nodeID, networkcap.NodeCredentialTraffic, networkcap.NodeCredentialChange{Ciphertext: encryptedSecret, Prefix: prefix})
	if err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"node_id":       nodeID,
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
	err = h.services.NodeAdministration.RevokeCredential(r.Context(), claims.UserID, nodeID, networkcap.NodeCredentialTraffic)
	if err != nil {
		if writeNodeAdministrationError(w, err) {
			return
		}
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
	removed, err := h.services.ProtocolEndpointRemoval.Remove(r.Context(), claims.UserID, endpointID)
	if errors.Is(err, networkcap.ErrResourceNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, networkcap.ErrResourcePermission) {
		Forbidden(w, "administrator access required")
		return
	}
	if errors.Is(err, networkcap.ErrProtocolEndpointRemovalConflict) {
		writeJSON(w, http.StatusConflict, "该协议服务仍有发布任务运行，请等待任务结束后再删除。", nil)
		return
	}
	if errors.Is(err, networkcap.ErrProtocolEndpointResourceDeleting) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, removed)
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
	endpointMutations := h.services.ProtocolEndpointMutations(h.credentialCipher)
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "vmess"
	}
	if !h.isProtocolSupported(protocol) {
		BadRequestFields(w, "协议服务校验失败。", map[string]string{"protocol": "请选择受支持的协议类型。"})
		return
	}
	var existing networkcap.ProtocolEndpointRecord
	var existingMutationSnapshot *networkcap.ProtocolEndpointMutationSnapshot
	if endpointID != 0 {
		snapshot, loadErr := endpointMutations.Load(r.Context(), claims.UserID, endpointID)
		if errors.Is(loadErr, networkcap.ErrProtocolEndpointNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(loadErr, networkcap.ErrProtocolEndpointMutationPermission) {
			Forbidden(w, "管理员权限已失效。")
			return
		}
		if loadErr != nil {
			ServerError(w, loadErr)
			return
		}
		existingMutationSnapshot = &snapshot
		existing = snapshot.Endpoint
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
		existingServerConfig := ""
		if existingMutationSnapshot != nil {
			existingServerConfig = existingMutationSnapshot.Endpoint.ServerConfig
		}
		req.Config, req.ClientConfig, err = prepareMieruEndpointConfigsWithExisting(req.Config, req.ClientConfig, existingServerConfig)
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
	if _, req.EgressConfig, err = networkcap.NormalizeProtocolEndpointEgressConfig(req.EgressConfig); err != nil {
		var validation *networkcap.ProtocolEndpointMutationValidation
		if errors.As(err, &validation) {
			BadRequestFields(w, validation.Message, validation.Fields)
		} else {
			ServerError(w, err)
		}
		return
	}

	if supported, reason := h.protocolKernelSupport(protocol); !supported {
		// Existing records on an unsupported selected kernel remain recoverable:
		// administrators may disable them while other writes wait for support.
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

	validationFinishedAt := time.Now()
	capabilityMembershipChanges := make([]networkcap.ProtocolEndpointMembershipChange, 0, len(membershipChanges))
	for _, change := range membershipChanges {
		capabilityMembershipChanges = append(capabilityMembershipChanges, networkcap.ProtocolEndpointMembershipChange{NodeGroupID: change.NodeGroupID, ExpectedRevision: change.ExpectedRevision, Member: change.Member})
	}
	result, transactionErr := endpointMutations.Save(r.Context(), claims.UserID, existingMutationSnapshot, networkcap.ProtocolEndpointMutationRequest{
		ID: endpointID, NodeID: req.NodeID, Name: req.Name, Protocol: protocol, Address: req.Address,
		Port: req.Port, PublicPort: req.PublicPort, Cipher: req.Cipher, ParentProtocolID: req.ParentProtocolID,
		ManagedCertificateID: req.ManagedCertificateID, MultiplierMilli: req.MultiplierMilli, IsActive: req.IsActive,
		ServerConfig: req.Config, EgressConfig: req.EgressConfig, ClientConfig: req.ClientConfig, OptionalConfig: req.OptionalConfig, Tags: req.Tags,
		MembershipChanges: capabilityMembershipChanges, CredentialProtocols: h.storedSubscriptionCredentialProtocols(),
	})
	transactionFinishedAt := time.Now()
	if err := transactionErr; err != nil {
		var conflict *networkcap.ProtocolEndpointMembershipConflictError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, "节点组已被其他管理员更新，请重新加载协议服务后再保存。", map[string]interface{}{"conflicts": conflict.Conflicts})
			return
		}
		var validation *networkcap.ProtocolEndpointMutationValidation
		if errors.As(err, &validation) {
			BadRequestFields(w, validation.Message, validation.Fields)
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointConflict) {
			writeJSON(w, http.StatusConflict, "协议服务已被其他管理员更新，请重新加载后再保存。", nil)
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointMutationPermission) {
			Forbidden(w, "管理员权限已失效。")
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointResourceDeleting) {
			writeJSON(w, http.StatusConflict, "资源已进入删除流程，请等待删除完成或重试保存。", nil)
			return
		}
		ServerError(w, err)
		return
	}
	if result.MembershipMutation != nil && len(result.MembershipMutation.ReconcileTasks) > 0 {
		h.StartAdminTaskWorker()
	}
	taskEnqueueFinishedAt := time.Now()
	responseFinishedAt := time.Now()
	timing := newProtocolEndpointMutationTiming(requestStartedAt, validationFinishedAt, transactionFinishedAt, taskEnqueueFinishedAt, responseFinishedAt)
	w.Header().Set("Server-Timing", fmt.Sprintf("validation;dur=%d, transaction;dur=%d, task_enqueue;dur=%d, response_preparation;dur=%d", timing.ValidationMS, timing.TransactionMS, timing.TaskEnqueueMS, timing.ResponsePreparationMS))
	OK(w, protocolEndpointMutationResponse{
		ProtocolEndpoint:              result.ProtocolEndpoint,
		protocolEndpointChangeEffects: result.ProtocolEndpointChangeEffects,
		NodeGroupMemberships:          result.Memberships,
		NodeGroupMembership:           result.MembershipMutation,
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
	EgressConfig            string                                `json:"egress_config,omitempty"`
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
	EgressProtocol          string                      `json:"egress_protocol,omitempty"`
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
		Protocol: endpoint.Protocol, EgressProtocol: endpoint.EgressProtocol, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
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
	item, err := h.services.NetworkInventory.ProtocolEndpoint(r.Context(), endpointID, time.Now().UTC())
	if err != nil {
		if errors.Is(err, networkcap.ErrInventoryNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	endpoint := protocolEndpointRecordModel(item.Endpoint)
	serverConfig, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
	if err != nil {
		ServerError(w, fmt.Errorf("decrypt protocol endpoint config: %w", err))
		return
	}
	egressConfig := ""
	if strings.TrimSpace(endpoint.EgressConfig) != "" {
		egressConfig, err = h.credentialCipher.Decrypt(endpoint.EgressConfig)
		if err != nil {
			ServerError(w, fmt.Errorf("decrypt protocol endpoint egress config: %w", err))
			return
		}
	}
	if strings.EqualFold(endpoint.Protocol, "mieru") {
		serverConfig, endpoint.ClientConfig = redactMieruEndpointAdminConfigs(serverConfig, endpoint.ClientConfig)
	} else if serverConfig, endpoint.ClientConfig, err = normalizeManagedProtocolTemplates(endpoint.Protocol, serverConfig, endpoint.ClientConfig); err != nil {
		ServerError(w, fmt.Errorf("normalize protocol endpoint template: %w", err))
		return
	}
	usage := protocolUsageModel(item.Usage)
	node := nodeAdministrationModel(item.Node)
	kernelSupported, kernelUnsupportedReason := h.protocolKernelSupportForNode(endpoint.Protocol, node)
	memberships := make([]protocolEndpointNodeGroupMembership, 0, len(item.Memberships))
	for _, membership := range item.Memberships {
		memberships = append(memberships, protocolEndpointNodeGroupMembership{NodeGroupID: membership.NodeGroupID, Name: membership.Name, Code: membership.Code, Description: membership.Description, IsEnabled: membership.IsEnabled, Revision: membership.Revision, SortOrder: membership.SortOrder})
	}
	detail := protocolEndpointAdminDetail{
		ProtocolEndpoint: endpoint, Config: serverConfig, EgressConfig: egressConfig, ManagedCertificateID: item.ManagedCertificateID,
		Usage: usage, KernelSupported: kernelSupported, KernelUnsupportedReason: kernelUnsupportedReason,
		NodeGroupMemberships: memberships,
	}
	if item.LatestDeployment != nil {
		deployment := protocolDeploymentModel(*item.LatestDeployment)
		detail.LatestDeployment = &deployment
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
	query := networkcap.ProtocolDeploymentQuery{Offset: offset, Limit: limit}
	for key, column := range map[string]string{"node_id": "node_id", "protocol_endpoint_id": "protocol_endpoint_id"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			parsed, parseErr := strconv.ParseUint(value, 10, 64)
			if parseErr != nil || parsed == 0 {
				BadRequest(w, "invalid "+key)
				return
			}
			if column == "node_id" {
				query.NodeID = uint(parsed)
			} else {
				query.ProtocolEndpointID = uint(parsed)
			}
		}
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if status != "running" && status != "succeeded" && status != "failed" {
			BadRequest(w, "invalid status")
			return
		}
		query.Status = status
	}
	page, err := h.services.NetworkInventory.ProtocolDeployments(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.ProtocolDeployment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, protocolDeploymentModel(item))
	}
	OK(w, pagedData(items, page.Total, offset, limit))
}

func (h *handlers) applyProtocolEndpointFilters(values url.Values) (networkcap.ProtocolEndpointInventoryQuery, error) {
	query := networkcap.ProtocolEndpointInventoryQuery{}
	if rawIDs := strings.TrimSpace(values.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		ids := make([]uint, 0, len(parts))
		for _, part := range parts {
			parsed, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
			if err != nil || parsed == 0 {
				return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("invalid ids")
			}
			ids = append(ids, uint(parsed))
		}
		if len(ids) > 100 {
			return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("ids cannot contain more than 100 values")
		}
		query.IDs = ids
	}
	if nodeID := strings.TrimSpace(values.Get("node_id")); nodeID != "" {
		parsed, err := strconv.ParseUint(nodeID, 10, 64)
		if err != nil || parsed == 0 {
			return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("invalid node_id")
		}
		query.NodeID = uint(parsed)
	}
	if search := strings.ToLower(strings.TrimSpace(values.Get("q"))); search != "" {
		if len([]byte(search)) > 100 {
			return networkcap.ProtocolEndpointInventoryQuery{}, validationError("协议端点筛选条件校验失败。", map[string]string{"q": "搜索内容不能超过 100 个 UTF-8 字节。"})
		}
		query.Search = search
	}
	if protocol := strings.ToLower(strings.TrimSpace(values.Get("protocol"))); protocol != "" {
		if !h.isProtocolSupported(protocol) {
			return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("invalid protocol")
		}
		query.Protocol = protocol
	}
	if rawActive := strings.TrimSpace(values.Get("active")); rawActive != "" {
		active, err := strconv.ParseBool(rawActive)
		if err != nil {
			return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("invalid active")
		}
		query.Active = &active
	}
	if deploymentStatus := strings.TrimSpace(values.Get("deployment_status")); deploymentStatus != "" {
		if deploymentStatus != "running" && deploymentStatus != "succeeded" && deploymentStatus != "failed" && deploymentStatus != "never" {
			return networkcap.ProtocolEndpointInventoryQuery{}, errors.New("invalid deployment_status")
		}
		query.DeploymentStatus = deploymentStatus
	}
	return query, nil
}

func (h *handlers) ProtocolEndpointSelectionHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	query, err := h.applyProtocolEndpointFilters(r.URL.Query())
	if err != nil {
		BadRequestError(w, err)
		return
	}
	ids, total, err := h.services.NetworkInventory.SelectProtocolEndpointIDs(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	if total > maxEndpointSelection {
		BadRequestFields(w, "协议端点筛选结果过多。", map[string]string{
			"q": fmt.Sprintf("当前筛选匹配 %d 个端点，批量快照上限为 %d 个；请缩小搜索范围。", total, maxEndpointSelection),
		})
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
	requestStartedAt := time.Now()
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
	query, err := h.applyProtocolEndpointFilters(r.URL.Query())
	if err != nil {
		BadRequestError(w, err)
		return
	}
	query.Sort = strings.TrimSpace(r.URL.Query().Get("sort"))
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("direction")), "desc") {
		query.Direction = "desc"
	}
	if paged {
		query.Paged, query.Offset, query.Limit = true, offset, limit
		query.IncludeStatusFacets = r.URL.Query().Get("include_facets") == "true"
	}
	query.Now = time.Now().UTC()
	page, err := h.services.NetworkInventory.ProtocolEndpoints(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]protocolEndpointListItem, 0, len(page.Items))
	for _, row := range page.Items {
		endpoint := protocolEndpointRecordModel(row.Endpoint)
		node := nodeAdministrationModel(row.Node)
		kernelSupported, kernelUnsupportedReason := h.protocolKernelSupportForNode(endpoint.Protocol, node)
		var deployment *model.ProtocolDeployment
		if row.LatestDeployment != nil {
			value := protocolDeploymentModel(*row.LatestDeployment)
			deployment = &value
		}
		items = append(items, newProtocolEndpointListItem(endpoint, node.Name, row.ManagedCertificateID, deployment, protocolUsageModel(row.Usage), kernelSupported, kernelUnsupportedReason))
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("protocol_inventory;dur=%d", time.Since(requestStartedAt).Milliseconds()))
	if paged {
		data := pagedData(items, page.Total, offset, limit)
		if query.IncludeStatusFacets {
			data["facets"] = page.Facets
		}
		OK(w, data)
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
	ids := protocolEndpointIDs(endpoints)
	result := make(map[uint]protocolEndpointUsage, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	usage, err := h.services.NetworkInventory.Usage(context.Background(), ids, now)
	if err != nil {
		return nil, err
	}
	for id, row := range usage {
		result[id] = protocolUsageModel(row)
	}
	return result, nil
}

func (h *handlers) loadLatestProtocolDeployments(endpoints []model.ProtocolEndpoint) (map[uint]*model.ProtocolDeployment, error) {
	ids := protocolEndpointIDs(endpoints)
	result := make(map[uint]*model.ProtocolDeployment, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	page, err := h.services.NetworkInventory.ProtocolEndpoints(context.Background(), networkcap.ProtocolEndpointInventoryQuery{IDs: ids, Now: time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	for _, row := range page.Items {
		if row.LatestDeployment != nil {
			value := protocolDeploymentModel(*row.LatestDeployment)
			result[row.Endpoint.ID] = &value
		}
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
	rows, err := h.services.NetworkInventory.RuntimeNodes(context.Background(), nodeIDs)
	if err != nil {
		return nil, err
	}
	for _, nodeID := range nodeIDs {
		if row, ok := rows[nodeID]; ok {
			result[nodeID] = row.Node.Name
		}
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
	rows, err := h.services.NetworkInventory.RuntimeNodes(context.Background(), nodeIDs)
	if err != nil {
		return nil, err
	}
	for _, nodeID := range nodeIDs {
		if row, ok := rows[nodeID]; ok {
			result[nodeID] = nodeRuntimeModel(row)
		}
	}
	return result, nil
}

func protocolEndpointRecordModel(r networkcap.ProtocolEndpointRecord) model.ProtocolEndpoint {
	return model.ProtocolEndpoint{ID: r.ID, NodeID: r.NodeID, Name: r.Name, RuntimeKey: r.RuntimeKey, Protocol: r.Protocol, Address: r.Address, Port: r.Port, PublicPort: r.PublicPort, Cipher: r.Cipher, ParentProtocolID: r.ParentProtocolID, MultiplierMilli: r.MultiplierMilli, ManagedPrincipalReady: r.ManagedPrincipalReady, MieruPrincipalReady: r.MieruPrincipalReady, ServerConfig: r.ServerCiphertext, EgressProtocol: r.EgressProtocol, EgressConfig: r.EgressCiphertext, ClientConfig: r.ClientConfig, OptionalConfig: r.OptionalConfig, Tags: r.Tags, IsActive: r.IsActive, SortOrder: r.SortOrder, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func protocolDeploymentModel(r networkcap.ProtocolDeploymentRecord) model.ProtocolDeployment {
	return model.ProtocolDeployment{ID: r.ID, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, ConfigRevision: r.ConfigRevision, DesiredConfigSHA256: r.DesiredConfigSHA256, AppliedConfigSHA256: r.AppliedConfigSHA256, Status: r.Status, RequestedBy: r.RequestedBy, Error: r.Error, Output: r.Output, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func protocolUsageModel(r networkcap.ProtocolUsageRecord) protocolEndpointUsage {
	return protocolEndpointUsage{ActiveFlows: r.ActiveFlows, ActiveUsers: r.ActiveUsers, ActiveCredentials: r.ActiveCredentials, LastUsedAt: r.LastUsedAt, UsedBytesToday: r.UsedBytesToday, UsedBytesTotal: r.UsedBytesTotal}
}
func nodeAdministrationModel(r networkcap.NodeAdministrationRecord) model.Node {
	return nodeInventoryModel(networkcap.NodeInventoryItem{Node: r})
}
func nodeRuntimeModel(r networkcap.NodeRuntimeRecord) model.Node {
	node := nodeInventoryModel(networkcap.NodeInventoryItem{Node: r.Node, KernelState: r.KernelState})
	node.NodeCredential = r.NodeCredentialCiphertext
	node.SSHPwd = r.SSHPwdCiphertext
	node.SSHPrivateKeyPassphrase = r.SSHPrivateKeyPassphraseCiphertext
	node.SSHPrivilegePassword = r.SSHPrivilegePasswordCiphertext
	node.TrafficSecret = r.TrafficSecretCiphertext
	return node
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
	query := networkcap.NodeGroupInventoryQuery{Paged: paged, Offset: offset, Limit: limit}
	if rawGroupID := strings.TrimSpace(r.URL.Query().Get("group_id")); rawGroupID != "" {
		groupID, parseErr := strconv.ParseUint(rawGroupID, 10, 64)
		if parseErr != nil || groupID == 0 {
			BadRequest(w, "invalid group_id")
			return
		}
		query.ID = uint(groupID)
	}
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		query.Search = search
	}
	if rawEnabled := strings.TrimSpace(r.URL.Query().Get("enabled")); rawEnabled != "" {
		enabled, parseErr := strconv.ParseBool(rawEnabled)
		if parseErr != nil {
			BadRequest(w, "invalid enabled")
			return
		}
		query.Enabled = &enabled
	}
	page, err := h.services.NetworkInventory.NodeGroups(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	if paged {
		items := make([]nodeGroupSummaryItem, 0, len(page.Items))
		for _, row := range page.Items {
			group := row.Group
			items = append(items, nodeGroupSummaryItem{
				ID:                    group.ID,
				Name:                  group.Name,
				Code:                  group.Code,
				Description:           group.Description,
				IsEnabled:             group.IsEnabled,
				Revision:              group.Revision,
				ProtocolEndpointCount: row.ProtocolEndpointCount,
				NetworkEntryCount:     len(group.NetworkEntryIDs),
				PlanCount:             group.PlanCount,
				CreatedAt:             group.CreatedAt,
				UpdatedAt:             group.UpdatedAt,
			})
		}
		OK(w, pagedData(items, page.Total, offset, limit))
		return
	}
	groups := make([]networkcap.NodeGroupRecord, 0, len(page.Items))
	for _, row := range page.Items {
		groups = append(groups, row.Group)
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
	NetworkEntryCount     int       `json:"network_entry_count"`
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
	group, err := h.services.NetworkInventory.NodeGroup(r.Context(), id)
	if err != nil {
		if errors.Is(err, networkcap.ErrInventoryNotFound) {
			NotFound(w)
			return
		}
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
	result, err := h.services.NodeGroupMutations.Create(r.Context(), claims.UserID, networkcap.NodeGroupCreateRequest{
		Name: req.Name, Code: req.Code, Description: req.Description, IsEnabled: req.IsEnabled,
		ProtocolEndpointIDs: req.ProtocolEndpointIDs, NetworkEntryIDs: req.NetworkEntryIDs,
		CredentialProtocols: h.storedSubscriptionCredentialProtocols(),
	})
	if err != nil {
		writeNodeGroupMutationError(w, err)
		return
	}
	if result.ReconcileTask != nil {
		h.StartAdminTaskWorker()
	}
	OK(w, nodeGroupMutationResponse{NodeGroupRecord: result.NodeGroup, ReconcileTask: result.ReconcileTask})
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
	result, err := h.services.NodeGroupMutations.Update(r.Context(), claims.UserID, networkcap.NodeGroupUpdateRequest{
		ID: id, ExpectedRevision: req.ExpectedRevision, Name: req.Name, Code: req.Code,
		Description: req.Description, IsEnabled: req.IsEnabled,
		ProtocolEndpointIDs: req.ProtocolEndpointIDs, NetworkEntryIDs: req.NetworkEntryIDs,
		CredentialProtocols: h.storedSubscriptionCredentialProtocols(),
	})
	if err != nil {
		writeNodeGroupMutationError(w, err)
		return
	}
	if result.ReconcileTask != nil {
		h.StartAdminTaskWorker()
	}
	OK(w, nodeGroupMutationResponse{NodeGroupRecord: result.NodeGroup, ReconcileTask: result.ReconcileTask})
}

type normalizedPlanPolicy = commerce.PlanPolicy

func normalizePlanPolicy(req planCreateReq, _ planSKUReq) (normalizedPlanPolicy, error) {
	value, err := commerce.NormalizePlanPolicy(commerce.PlanCreateRequest{TrafficBytes: req.TrafficBytes, SpeedLimitMbps: req.SpeedLimitMbps, MaxActiveSubscriptions: req.MaxActiveSubscriptions, DeviceLimit: req.DeviceLimit, FamilyLimit: req.FamilyLimit, ResetPolicy: req.ResetPolicy, TrafficCalcMode: req.TrafficCalcMode, IsRenewable: req.IsRenewable})
	return value, commerceValidationError(err)
}

func validTrafficCalcMode(mode int16) bool {
	return mode == trafficCalcBoth || mode == trafficCalcUpload || mode == trafficCalcDownload
}

func buildPlanSKU(planID uint, req planSKUReq) (model.PlanSKU, error) {
	sku, err := commerce.BuildLegacySKU(planID, req)
	return model.PlanSKU(sku), commerceValidationError(err)
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

type renewalFulfillment struct{ extendPeriod, addQuota, makePermanent bool }

func renewalFulfillmentForOrder(order model.Order) (renewalFulfillment, error) {
	out, err := entitlements.RenewalForGrant(grantRequestForOrder(order))
	return renewalFulfillment{out.ExtendPeriod, out.AddQuota, out.MakePermanent}, err
}

func createQuotaEvent(tx *gorm.DB, sub model.Subscription, eventType string, delta, before, after int64, referenceType, referenceID string) error {
	return application.RecordEntitlementQuotaEvent(tx, sub, eventType, delta, before, after, referenceType, referenceID)
}

func nextTrafficReset(base time.Time, policy int16) *time.Time {
	return entitlements.NextTrafficReset(base, policy)
}
func nextTrafficResetAfter(anchor time.Time, policy int16, after time.Time) *time.Time {
	return entitlements.NextTrafficResetAfter(anchor, policy, after)
}
func effectiveResetPolicy(unit string, policy int16) int16 {
	return entitlements.EffectiveResetPolicy(unit, policy)
}
func isPerpetualSubscriptionEnd(value time.Time) bool { return entitlements.IsPerpetualEnd(value) }
func addBillingPeriod(base time.Time, unit string, value int) (time.Time, error) {
	return entitlements.AddBillingPeriod(base, unit, value)
}
func addCalendarMonths(base time.Time, months int) time.Time {
	return entitlements.AddCalendarMonths(base, months)
}

func expireSubscriptions(db *gorm.DB, userID uint, now time.Time) error {
	if db == nil {
		return errors.New("database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error { return expireSubscriptionsInTx(tx, userID, now.UTC()) })
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
	configuredTarget, siteURL, _ := h.services.SubscriptionPresentation.Camouflage(r.Context())
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

func newSubscriptionToken() (string, string, string, error) { return entitlements.NewAccessToken() }
func hashSubscriptionToken(raw string) string               { return entitlements.HashAccessToken(raw) }

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
	result, err := h.services.UsageSummary().Read(r.Context(), claims.UserID, metering.UsageSummaryQuery{Administrative: adminScope, UserID: userFilter})
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	OK(w, result)
}

func (h *handlers) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	totals, err := h.services.Dashboard.Totals(r.Context(), time.Now().UTC())
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, totals)
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

	query := observability.AuditLogQuery{Actor: strings.TrimSpace(r.URL.Query().Get("actor")), Action: strings.TrimSpace(r.URL.Query().Get("action")), Target: strings.TrimSpace(r.URL.Query().Get("target")), From: window.From, To: window.To, Offset: offset, Limit: limit}
	if cursor != nil {
		query.CursorAt, query.CursorID, query.CursorDirection = cursor.At, cursor.ID, cursor.Direction
	}
	page, err := h.services.AuditDirectory.List(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	logs := make([]model.AuditLog, 0, len(page.Items))
	for _, item := range page.Items {
		logs = append(logs, auditLogModel(item))
	}
	if cursor == nil && offset > 0 {
		OK(w, pagedData(auditLogSummaries(logs), page.Total, offset, limit))
		return
	}
	var nextCursor, previousCursor *string
	if len(logs) > 0 {
		nextCursor, previousCursor, err = historyPageCursorValues(
			historyKey{At: logs[0].CreatedAt, ID: logs[0].ID},
			historyKey{At: logs[len(logs)-1].CreatedAt, ID: logs[len(logs)-1].ID},
			cursor,
			page.HasMore,
		)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, cursorPagedData(auditLogSummaries(logs), page.Total, limit, nextCursor, previousCursor))
}

func auditLogModel(item observability.AuditLogRecord) model.AuditLog {
	return model.AuditLog{ID: item.ID, UserID: item.UserID, Actor: item.Actor, Action: item.Action, Target: item.Target, Detail: item.Detail, CreatedAt: item.CreatedAt}
}

func (h *handlers) trafficRecordsHandler(w http.ResponseWriter, r *http.Request) {
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

	paged := wantsPagedList(r)
	q := metering.RecordsQuery{Administrative: adminScope, UserID: uint(userFilter), NodeID: uint(nodeFilter), SubscriptionID: uint(subscriptionFilter), ProtocolEndpointID: uint(protocolEndpointFilter), Paged: paged, Limit: 50}
	var cursor *historyCursor
	if paged {
		q.Offset, q.Limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		window, err := parseHistoryWindow(r.URL.Query(), 7)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		q.From, q.To = window.From, window.To
		cursor, err = decodeHistoryCursor(r.URL.Query().Get("cursor"), nil)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if cursor != nil {
			q.Cursor = &metering.RecordCursor{At: cursor.At, ID: cursor.ID, Direction: cursor.Direction}
		}
	}
	result, err := h.services.Records().Read(r.Context(), claims.UserID, q)
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	if !paged {
		OK(w, result.Records)
		return
	}
	records := make([]model.TrafficRecord, 0, len(result.Records))
	for _, record := range result.Records {
		records = append(records, model.TrafficRecord(record))
	}
	if cursor == nil && q.Offset > 0 {
		data := pagedData(trafficRecordSummaries(records), result.Total, q.Offset, q.Limit)
		data["aggregates"] = result.Aggregates
		OK(w, data)
		return
	}
	var nextCursor, previousCursor *string
	if len(records) > 0 {
		nextCursor, previousCursor, err = historyPageCursorValues(historyKey{At: records[0].At, ID: records[0].ID}, historyKey{At: records[len(records)-1].At, ID: records[len(records)-1].ID}, cursor, result.HasMore)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	data := cursorPagedData(trafficRecordSummaries(records), result.Total, q.Limit, nextCursor, previousCursor)
	data["aggregates"] = result.Aggregates
	OK(w, data)
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

	result, err := h.services.NodeReports().Record(r.Context(), metering.AuthenticatedNodeReport{NodeID: authenticated.node.ID, ExpectedCredential: authenticated.node.TrafficSecret, Timestamp: authenticated.timestamp, Nonce: authenticated.nonce, Version: nodeVersion, ReportID: req.ReportID, UserID: req.UserID, ProtocolEndpointID: req.ProtocolEndpointID, RawBytes: req.RawBytes, UploadBytes: req.UploadBytes, DownloadBytes: req.DownloadBytes, Meta: req.Meta})

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
	if result.QuotaExhausted {
		BadRequest(w, errSubscriptionQuotaExhausted.Error())
		return
	}

	response := map[string]interface{}{
		"report_id":                 result.Record.ReportID,
		"subscription_id":           result.Record.SubscriptionID,
		"node_id":                   result.Record.NodeID,
		"protocol_endpoint_id":      result.Record.ProtocolEndpointID,
		"raw_bytes":                 result.Record.RawBytes,
		"upload_bytes":              result.Record.UploadBytes,
		"download_bytes":            result.Record.DownloadBytes,
		"traffic_calc_mode":         result.Record.TrafficCalcMode,
		"protocol_multiplier_milli": result.Record.ProtocolMultiplierMilli,
		"used_bytes":                result.Record.UsedBytes,
		"duplicate":                 result.Duplicate,
	}
	if !result.Duplicate {
		response["flow_used"] = result.FlowUsed
		response["flow_total"] = result.FlowTotal
		response["flow_remaining"] = result.FlowTotal - result.FlowUsed
		response["subscription_end"] = result.SubscriptionEnd.Format(time.RFC3339)
	}
	OK(w, response)
}

func billedTrafficBytes(rawBytes, multiplierMilli int64) int64 {
	billed, _ := billedTrafficBytesChecked(rawBytes, multiplierMilli)
	return billed
}

func trafficBytesForMode(up, down int64, mode int16) int64 {
	return metering.TrafficBytesForMode(up, down, mode)
}
func billedTrafficBytesChecked(raw, multiplier int64) (int64, error) {
	return metering.BilledTrafficBytes(raw, multiplier)
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
	record, err := h.services.NetworkInventory.RuntimeNode(r.Context(), nodeID)
	if err != nil || !record.Node.IsEnabled {
		return model.Node{}, errors.New("invalid node credential")
	}
	node := nodeRuntimeModel(record)
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
	return h.loadNodeContext(context.Background(), nodeID)
}

func (h *handlers) loadNodeContext(ctx context.Context, nodeID uint) (model.Node, error) {
	record, err := h.services.NetworkInventory.RuntimeNode(ctx, nodeID)
	if err != nil {
		if errors.Is(err, networkcap.ErrInventoryNotFound) {
			return model.Node{}, gorm.ErrRecordNotFound
		}
		return model.Node{}, err
	}
	return nodeRuntimeModel(record), nil
}

func (h *handlers) isProtocolSupported(proto string) bool {
	return networkcap.IsRuntimeProtocolSupported(proto)
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

func (h *handlers) protocolKernelSupportForNode(proto string, _ model.Node) (bool, string) {
	// Release numbering can reset; the target kernel validates the generated config.
	return h.protocolKernelSupport(proto)
}

func (h *handlers) protocolKernelCapabilities() map[string]protocolKernelCapability {
	capabilities := make(map[string]protocolKernelCapability, len(supportedProtocols))
	for protocol := range supportedProtocols {
		supported, reason := h.protocolKernelSupport(protocol)
		capability := protocolKernelCapability{Supported: supported, Reason: reason}
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
		return validationError("协议配置校验失败。", map[string]string{"config": "VLESS Reality 仅支持原始 TCP。"})
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
	signer, err := sshadapter.ParsePrivateKey(privateKey, passphrase)
	if err != nil {
		return nil, validationError("SSH 配置校验失败。", map[string]string{"ssh_private_key": "私钥格式或口令无效。"})
	}
	return signer, nil
}

func validateSSHHostKeyFingerprint(fingerprint string) error {
	return sshadapter.ValidateHostKeyFingerprint(fingerprint)
}

func verifiedHostKeyCallback(expectedFingerprint string, observedFingerprint *string) ssh.HostKeyCallback {
	return sshadapter.VerifiedHostKeyCallback(expectedFingerprint, observedFingerprint)
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
