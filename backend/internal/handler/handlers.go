package handler

import (
	"bytes"
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
	orderStatusPending     = "pending"
	orderStatusPaid        = "paid"
	orderStatusFailed      = "failed"
	orderStatusCanceled    = "canceled"
	orderStatusSuccess     = "success"
	subStatusActive        = "active"
	subStatusExpired       = "expired"
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
)

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
	NodeID           uint   `json:"node_id"`
	Name             string `json:"name"`
	Protocol         string `json:"protocol"`
	Address          string `json:"address"`
	Port             int    `json:"port"`
	PublicPort       int    `json:"public_port"`
	Cipher           int16  `json:"cipher"`
	ParentProtocolID *uint  `json:"parent_protocol_id"`
	MultiplierMilli  int64  `json:"multiplier_milli"`
	IsActive         *bool  `json:"is_active"`
	SortOrder        int    `json:"sort_order"`
	Config           string `json:"config"`
	ClientConfig     string `json:"client_config"`
	OptionalConfig   string `json:"optional_config"`
	Tags             string `json:"tags"`
}

type subscriptionManifestNode struct {
	ID              uint            `json:"id"`
	NodeID          uint            `json:"node_id"`
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
	db               *gorm.DB
	jwtSecret        string
	credentialCipher *security.CredentialCipher
	zeroArtifactDir  string
	sshTerminal      *sshTerminalRuntime
}

func NewHandlers(db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher, zeroArtifactDir string) (*handlers, error) {
	if err := cfgpkg.ValidateJWTSecret(jwtSecret); err != nil {
		return nil, err
	}
	if credentialCipher == nil {
		return nil, errors.New("credential cipher is required")
	}
	return &handlers{
		db:               db,
		jwtSecret:        jwtSecret,
		credentialCipher: credentialCipher,
		zeroArtifactDir:  strings.TrimSpace(zeroArtifactDir),
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
	OK(w, map[string]interface{}{
		"version": version.FullVersion(),
		"name":    "zboard",
	})
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
		BadRequest(w, err.Error())
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
		BadRequest(w, err.Error())
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
	if err := normalizeAndValidateSiteSettings(&body.SiteName, &body.SiteURL); err != nil {
		return err
	}
	body.AdminEmail = normalizeEmail(body.AdminEmail)
	if !validEmail(body.AdminEmail) {
		return errors.New("admin_email is invalid")
	}
	if len(body.AdminPassword) < 12 || len(body.AdminPassword) > 72 {
		return errors.New("admin_password must contain 12 to 72 bytes")
	}
	return nil
}

func normalizeAndValidateSiteSettings(siteName, siteURL *string) error {
	*siteName = strings.TrimSpace(*siteName)
	*siteURL = strings.TrimRight(strings.TrimSpace(*siteURL), "/")
	if *siteName == "" || len(*siteName) > 80 {
		return errors.New("site_name must contain 1 to 80 bytes")
	}
	parsedURL, err := url.ParseRequestURI(*siteURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return errors.New("site_url must be an absolute http or https URL")
	}
	if parsedURL.User != nil || parsedURL.Fragment != "" {
		return errors.New("site_url must not contain credentials or a fragment")
	}
	return nil
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
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	var body req
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	body.Email = normalizeEmail(body.Email)
	if !validEmail(body.Email) || !validPassword(body.Password) {
		BadRequest(w, "a valid email and a 12 to 72 byte password are required")
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

	if err := h.db.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") {
			BadRequest(w, "email already exists")
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
	if email == "" || body.Password == "" {
		BadRequest(w, "email and password required")
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
	query := h.db.Order("id desc")

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
		like := fmt.Sprintf("%%%s%%", q)
		query = query.Where("email LIKE ?", like)
	}

	if err := query.Find(&users).Error; err != nil {
		ServerError(w, err)
		return
	}

	publicUsers := make([]userPublic, 0, len(users))
	for _, user := range users {
		publicUsers = append(publicUsers, toPublicUser(user))
	}
	OK(w, publicUsers)
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
	if !validEmail(req.Email) || !validPassword(req.Password) {
		BadRequest(w, "a valid email and a 12 to 72 byte password are required")
		return
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = userStatusActive
	}
	if !h.isValidUserStatus(status) {
		BadRequest(w, "invalid status")
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
			BadRequest(w, "email already exists")
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
			BadRequest(w, "invalid user status")
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
			BadRequest(w, "password must contain 12 to 72 bytes")
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
		BadRequest(w, "node name is required")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.CommunicationProtocol == 0 {
		req.CommunicationProtocol = 1
	}
	if err := validateOptionalJSONObject("config", req.Config); err != nil {
		BadRequest(w, err.Error())
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
			BadRequest(w, err.Error())
			return
		}
		if req.SSHAuthMethod == sshAuthPrivateKey {
			if _, err := parseSSHPrivateKey(credential, req.SSHPrivateKeyPassphrase); err != nil {
				BadRequest(w, err.Error())
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
		BadRequest(w, err.Error())
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
			BadRequest(w, "node_credential must be at least 12 characters")
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
			BadRequest(w, "node name cannot be empty")
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
			BadRequest(w, "lifecycle_status must be active, maintenance or retired")
			return
		}
		updates["lifecycle_status"] = status
		if status != "active" {
			updates["is_enabled"] = false
		}
	}
	if req.IsEnabled != nil {
		if lifecycle, ok := updates["lifecycle_status"].(string); ok && lifecycle != "active" && *req.IsEnabled {
			BadRequest(w, "a maintenance or retired node cannot be enabled")
			return
		}
		if node.LifecycleStatus != "" && node.LifecycleStatus != "active" && *req.IsEnabled && req.LifecycleStatus == nil {
			BadRequest(w, "set lifecycle_status to active before enabling this node")
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
		BadRequest(w, err.Error())
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
			BadRequest(w, "a new credential is required when changing ssh_auth_method")
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
			BadRequest(w, err.Error())
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
		BadRequest(w, err.Error())
		return
	}
	targetChanged := !strings.EqualFold(req.SSHHost, strings.TrimSpace(node.SSHHost)) || req.SSHPort != node.SSHPort
	targetFingerprint := node.SSHHostKeyFingerprint
	if targetChanged {
		targetFingerprint = ""
	}
	if err := validateSSHFields(req.SSHHost, req.SSHPort, req.SSHUser, authMethod, encryptedCredential, targetFingerprint); err != nil {
		BadRequest(w, err.Error())
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
			UserID: claims.UserID,
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
			UserID: claims.UserID,
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
			UserID: claims.UserID,
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

func (h *handlers) saveProtocolEndpoint(w http.ResponseWriter, r *http.Request, endpointID uint) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req protocolEndpointWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "vmess"
	}
	if !h.isProtocolSupported(protocol) {
		BadRequest(w, "unsupported protocol")
		return
	}
	if req.NodeID == 0 {
		BadRequest(w, "node_id is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	if req.Name == "" || req.Address == "" || req.Port <= 0 || req.Port > 65535 {
		BadRequest(w, "name, address and a valid port are required")
		return
	}
	if req.PublicPort == 0 {
		req.PublicPort = req.Port
	}
	if req.PublicPort <= 0 || req.PublicPort > 65535 {
		BadRequest(w, "public_port must be between 1 and 65535")
		return
	}
	if req.MultiplierMilli <= 0 || req.MultiplierMilli > 100000 {
		BadRequest(w, "multiplier_milli must be between 1 and 100000 (1000 means 1x)")
		return
	}
	if err := validateNodeProtocolConfigs(protocol, req.Config, req.ClientConfig); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := validateOptionalJSONObject("optional_config", req.OptionalConfig); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := validateOptionalJSONArray("tags", req.Tags); err != nil {
		BadRequest(w, err.Error())
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
	if err := h.validateProtocolParent(endpointID, node.ID, req.ParentProtocolID); err != nil {
		BadRequest(w, err.Error())
		return
	}
	runtimeKey := uuid.NewString()
	isActive := false
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	var existing model.ProtocolEndpoint
	if endpointID != 0 {
		if err := h.db.First(&existing, endpointID).Error; err != nil {
			NotFound(w)
			return
		}
		if existing.NodeID != node.ID {
			BadRequest(w, "protocol endpoint does not belong to node")
			return
		}
		runtimeKey = existing.RuntimeKey
		if req.IsActive == nil {
			isActive = existing.IsActive
		}
		if !isActive {
			var activePlanCount int64
			if err := h.db.Table("node_group_endpoints").
				Joins("JOIN plans ON plans.node_group_id = node_group_endpoints.node_group_id").
				Where("node_group_endpoints.protocol_endpoint_id = ? AND plans.is_active = ?", existing.ID, true).
				Count(&activePlanCount).Error; err != nil || activePlanCount > 0 {
				BadRequest(w, "unbind this endpoint from active plans before disabling it")
				return
			}
		}
	}

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
	endpoint.ServerConfig = encryptedServerConfig
	endpoint.ClientConfig = req.ClientConfig
	endpoint.OptionalConfig = normalizeOptionalJSON(req.OptionalConfig, "{}")
	endpoint.Tags = normalizeOptionalJSON(req.Tags, "[]")
	endpoint.IsActive = isActive
	endpoint.SortOrder = req.SortOrder

	action := "protocol_endpoint.create"
	if endpointID != 0 {
		action = "protocol_endpoint.update"
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if endpointID == 0 {
			if err := tx.Create(&endpoint).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, action, fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), fmt.Sprintf("node=%d protocol=%s multiplier_milli=%d", node.ID, protocol, endpoint.MultiplierMilli))
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, endpoint)
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
	var endpoint model.ProtocolEndpoint
	if err := h.db.First(&endpoint, endpointID).Error; err != nil {
		NotFound(w)
		return
	}
	node, err := h.loadNode(endpoint.NodeID)
	if err != nil {
		NotFound(w)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	serverConfig, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
	if err != nil {
		ServerError(w, fmt.Errorf("decrypt protocol endpoint config: %w", err))
		return
	}

	startedAt := time.Now().UTC()
	deployment := model.ProtocolDeployment{
		ProtocolEndpointID: endpoint.ID, NodeID: node.ID,
		ConfigRevision: uint64(startedAt.UnixNano()), Status: "running",
		RequestedBy: claims.UserID, StartedAt: &startedAt,
	}
	if err := h.db.Create(&deployment).Error; err != nil {
		ServerError(w, err)
		return
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(serverConfig))
	remotePath := fmt.Sprintf("/etc/zerodenet/protocols/%s.json", endpoint.RuntimeKey)
	temporaryPath := remotePath + ".tmp"
	command := fmt.Sprintf("mkdir -p /etc/zerodenet/protocols && printf '%s' | base64 -d > %s && chmod 600 %s && mv %s %s", encoded, temporaryPath, temporaryPath, temporaryPath, remotePath)
	output, elapsed, execErr := h.execSSHCommandWithPrivilege(node, command, true)
	if execErr != nil {
		finishedAt := time.Now().UTC()
		_ = h.db.Model(&deployment).Updates(map[string]interface{}{
			"status": "failed", "output": strings.TrimSpace(output), "error": execErr.Error(), "finished_at": finishedAt,
		}).Error
		BadRequest(w, "protocol deployment failed: "+execErr.Error())
		return
	}

	now := time.Now().UTC()
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		nodeUpdates := map[string]interface{}{"last_sync_at": now, "ssh_verified_at": now}
		if err := tx.Model(&node).Updates(nodeUpdates).Error; err != nil {
			return err
		}
		if err := tx.Model(&deployment).Updates(map[string]interface{}{
			"status": "succeeded", "output": strings.TrimSpace(output), "error": "", "finished_at": now,
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "protocol_endpoint.deploy", fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), fmt.Sprintf("node=%d deployment=%d", node.ID, deployment.ID))
	}); err != nil {
		ServerError(w, err)
		return
	}
	deployment.Status = "succeeded"
	deployment.Output = strings.TrimSpace(output)
	deployment.FinishedAt = &now
	OK(w, map[string]interface{}{
		"protocol_endpoint": endpoint,
		"deployment":        deployment,
		"output":            strings.TrimSpace(output),
		"latency_ms":        elapsed.Milliseconds(),
	})
}

type protocolEndpointAdminDetail struct {
	model.ProtocolEndpoint
	Config           string                    `json:"config"`
	LatestDeployment *model.ProtocolDeployment `json:"latest_deployment,omitempty"`
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
	detail := protocolEndpointAdminDetail{ProtocolEndpoint: endpoint, Config: serverConfig}
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
	OK(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *handlers) ProtocolEndpointListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	endpoints := make([]model.ProtocolEndpoint, 0)
	query := h.db.Order("sort_order asc, id asc")
	if nodeID := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}
	if err := query.Find(&endpoints).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, endpoints)
}

func (h *handlers) NodeGroupListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	groups := make([]model.NodeGroup, 0)
	if err := h.db.Order("name asc, id asc").Find(&groups).Error; err != nil {
		ServerError(w, err)
		return
	}
	for index := range groups {
		_ = h.db.Model(&model.NodeGroupEndpoint{}).
			Where("node_group_id = ?", groups[index].ID).
			Order("sort_order asc, id asc").
			Pluck("protocol_endpoint_id", &groups[index].ProtocolEndpointIDs).Error
	}
	OK(w, groups)
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
	if req.Name == "" || req.Code == "" {
		BadRequest(w, "name and code are required")
		return
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}
	group := model.NodeGroup{Name: req.Name, Code: req.Code, Description: strings.TrimSpace(req.Description), IsEnabled: isEnabled}
	endpointIDs := uniqueUintIDs(req.ProtocolEndpointIDs)
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		if err := replaceNodeGroupEndpoints(tx, group.ID, endpointIDs); err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node_group.create", fmt.Sprintf("node_group:%d", group.ID), fmt.Sprintf("endpoints=%v", endpointIDs))
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}
	group.ProtocolEndpointIDs = endpointIDs
	OK(w, group)
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
		NotFound(w)
		return
	}
	updates := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			BadRequest(w, "name cannot be empty")
			return
		}
		updates["name"] = name
	}
	if req.Code != nil {
		code := strings.ToLower(strings.TrimSpace(*req.Code))
		if code == "" {
			BadRequest(w, "code cannot be empty")
			return
		}
		updates["code"] = code
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.IsEnabled != nil {
		if !*req.IsEnabled {
			var activePlans int64
			if err := h.db.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&activePlans).Error; err != nil {
				ServerError(w, err)
				return
			}
			if activePlans > 0 {
				BadRequest(w, "disable plans that use this node group first")
				return
			}
		}
		updates["is_enabled"] = *req.IsEnabled
	}
	if len(updates) == 0 && req.ProtocolEndpointIDs == nil {
		BadRequest(w, "no valid update fields")
		return
	}
	if req.ProtocolEndpointIDs != nil && len(uniqueUintIDs(*req.ProtocolEndpointIDs)) == 0 {
		var activePlans int64
		if err := h.db.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&activePlans).Error; err != nil {
			ServerError(w, err)
			return
		}
		if activePlans > 0 {
			BadRequest(w, "an node group used by active plans must retain at least one protocol endpoint")
			return
		}
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&group).Updates(updates).Error; err != nil {
				return err
			}
		}
		if req.ProtocolEndpointIDs != nil {
			if err := replaceNodeGroupEndpoints(tx, group.ID, uniqueUintIDs(*req.ProtocolEndpointIDs)); err != nil {
				return err
			}
		}
		return createAuditLog(tx, claims, "node_group.update", fmt.Sprintf("node_group:%d", group.ID), "updated")
	})
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.db.First(&group, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	_ = h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Order("sort_order asc, id asc").Pluck("protocol_endpoint_id", &group.ProtocolEndpointIDs).Error
	OK(w, group)
}

func replaceNodeGroupEndpoints(tx *gorm.DB, nodeGroupID uint, endpointIDs []uint) error {
	for _, endpointID := range endpointIDs {
		var count int64
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("id = ? AND is_active = ?", endpointID, true).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("active protocol endpoint %d not found", endpointID)
		}
	}
	if err := tx.Where("node_group_id = ?", nodeGroupID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		return err
	}
	for index, endpointID := range endpointIDs {
		if err := tx.Create(&model.NodeGroupEndpoint{NodeGroupID: nodeGroupID, ProtocolEndpointID: endpointID, SortOrder: index}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *handlers) PlanListHandler(w http.ResponseWriter, r *http.Request) {
	plans := make([]model.Plan, 0)
	claims, claimErr := h.authFromRequest(r)
	isAdmin := claimErr == nil && claims.IsAdmin
	query := h.db.Preload("SKUs", func(db *gorm.DB) *gorm.DB {
		if !isAdmin {
			db = db.Where("is_active = ?", true)
		}
		return db.Order("sort_order asc, id asc")
	}).Order("sort_order asc, id desc")

	if !isAdmin {
		query = query.Where("is_active = 1")
	} else if parseBoolQuery(r.URL.Query().Get("include_inactive")) {
		// admin can view inactive plans for management.
	} else {
		query = query.Where("is_active = 1")
	}

	if err := query.Find(&plans).Error; err != nil {
		ServerError(w, err)
		return
	}
	for i := range plans {
		var group model.NodeGroup
		if err := h.db.Select("id", "name", "code", "is_enabled").First(&group, plans[i].NodeGroupID).Error; err == nil {
			plans[i].NodeGroup = &group
		}
	}
	OK(w, plans)
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
	if req.Name == "" || req.Slug == "" || len(req.SKUs) == 0 {
		BadRequest(w, "name, slug and at least one sku are required")
		return
	}
	if req.NodeGroupID == 0 {
		BadRequest(w, "node_group_id is required")
		return
	}

	policy, err := normalizePlanPolicy(req, req.SKUs[0])
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	plan := model.Plan{
		Name: req.Name, Slug: req.Slug, Summary: strings.TrimSpace(req.Summary),
		Description: strings.TrimSpace(req.Description), IsActive: req.IsActive, SortOrder: req.SortOrder,
		TrafficBytes: policy.TrafficBytes, SpeedLimitMbps: policy.SpeedLimitMbps,
		MaxActiveSubscriptions: policy.MaxActiveSubscriptions, IsRenewable: policy.IsRenewable,
		DeviceLimit: policy.DeviceLimit, FamilyLimit: policy.FamilyLimit,
		ResetPolicy: policy.ResetPolicy, TrafficCalcMode: policy.TrafficCalcMode,
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var group model.NodeGroup
		if err := tx.First(&group, req.NodeGroupID).Error; err != nil {
			return errors.New("node group not found")
		}
		if plan.IsActive && !group.IsEnabled {
			return errors.New("an active plan requires an enabled node group")
		}
		plan.NodeGroupID = group.ID
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		for _, skuReq := range req.SKUs {
			sku, err := buildPlanSKU(plan.ID, skuReq)
			if err != nil {
				return err
			}
			if err := tx.Create(&sku).Error; err != nil {
				return err
			}
		}
		if req.IsActive {
			var activeSKUCount int64
			if err := tx.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", plan.ID, true).Count(&activeSKUCount).Error; err != nil || activeSKUCount == 0 {
				return errors.New("an active plan must have at least one active sku")
			}
		}
		if plan.IsActive {
			var endpointCount int64
			if err := tx.Model(&model.NodeGroupEndpoint{}).
				Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
				Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", plan.NodeGroupID, true).
				Count(&endpointCount).Error; err != nil || endpointCount == 0 {
				return errors.New("an active plan requires a node group with at least one active protocol endpoint")
			}
		}
		return createAuditLog(tx, claims, "plan.create", fmt.Sprintf("plan:%d", plan.ID), fmt.Sprintf("skus=%d node_group=%d", len(req.SKUs), plan.NodeGroupID))
	})
	if err != nil {
		BadRequest(w, err.Error())
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

	updates := make(map[string]interface{})
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			BadRequest(w, "name cannot be empty")
			return
		}
		updates["name"] = name
	}
	if req.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*req.Slug))
		if slug == "" {
			BadRequest(w, "slug cannot be empty")
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
			BadRequest(w, "node_group_id must be positive")
			return
		}
		var group model.NodeGroup
		if err := h.db.First(&group, *req.NodeGroupID).Error; err != nil {
			BadRequest(w, "node group not found")
			return
		}
		updates["node_group_id"] = *req.NodeGroupID
	}
	if req.TrafficBytes != nil {
		if *req.TrafficBytes <= 0 {
			BadRequest(w, "traffic_bytes must be positive")
			return
		}
		updates["traffic_bytes"] = *req.TrafficBytes
	}
	if req.SpeedLimitMbps != nil {
		if *req.SpeedLimitMbps < 0 {
			BadRequest(w, "speed_limit_mbps cannot be negative")
			return
		}
		updates["speed_limit_mbps"] = *req.SpeedLimitMbps
	}
	if req.MaxActiveSubscriptions != nil {
		if *req.MaxActiveSubscriptions < 0 {
			BadRequest(w, "max_active_subscriptions cannot be negative")
			return
		}
		updates["max_active_subscriptions"] = *req.MaxActiveSubscriptions
	}
	if req.IsRenewable != nil {
		updates["is_renewable"] = *req.IsRenewable
	}
	if req.DeviceLimit != nil {
		if *req.DeviceLimit <= 0 {
			BadRequest(w, "device_limit must be positive")
			return
		}
		updates["device_limit"] = *req.DeviceLimit
	}
	if req.FamilyLimit != nil {
		if *req.FamilyLimit < 0 {
			BadRequest(w, "family_limit cannot be negative")
			return
		}
		updates["family_limit"] = *req.FamilyLimit
	}
	if req.ResetPolicy != nil {
		if *req.ResetPolicy < 0 || *req.ResetPolicy > 5 {
			BadRequest(w, "reset_policy must be between 0 and 5")
			return
		}
		updates["reset_policy"] = *req.ResetPolicy
	}
	if req.TrafficCalcMode != nil {
		if !validTrafficCalcMode(*req.TrafficCalcMode) {
			BadRequest(w, "traffic_calc_mode must be 0, 1 or 2")
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
			BadRequest(w, "an active plan requires an enabled node group")
			return
		}
		var endpointCount int64
		if err := h.db.Model(&model.NodeGroupEndpoint{}).
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
			Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", targetNodeGroupID, true).
			Count(&endpointCount).Error; err != nil || endpointCount == 0 {
			BadRequest(w, "an active plan requires a node group with at least one active protocol endpoint")
			return
		}
		var activeSKUCount int64
		if err := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", id, true).Count(&activeSKUCount).Error; err != nil || activeSKUCount == 0 {
			BadRequest(w, "an active plan must have at least one active sku")
			return
		}
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&plan).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plan.update", fmt.Sprintf("plan:%d", plan.ID), fmt.Sprintf("fields=%d node_group=%d", len(updates), targetNodeGroupID))
	}); err != nil {
		BadRequest(w, err.Error())
		return
	}

	if err := h.db.First(&plan, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, plan)
}

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
	policy := normalizedPlanPolicy{
		TrafficBytes: req.TrafficBytes, SpeedLimitMbps: req.SpeedLimitMbps,
		MaxActiveSubscriptions: req.MaxActiveSubscriptions, DeviceLimit: req.DeviceLimit,
		FamilyLimit: req.FamilyLimit, ResetPolicy: req.ResetPolicy,
		TrafficCalcMode: req.TrafficCalcMode,
		IsRenewable:     true,
	}
	if policy.TrafficBytes == 0 {
		policy.TrafficBytes = firstSKU.TrafficBytes
	}
	if policy.SpeedLimitMbps == 0 {
		policy.SpeedLimitMbps = firstSKU.SpeedLimitMbps
	}
	if policy.DeviceLimit == 0 {
		policy.DeviceLimit = firstSKU.DeviceLimit
	}
	if req.IsRenewable != nil {
		policy.IsRenewable = *req.IsRenewable
	}
	if policy.TrafficBytes <= 0 || policy.DeviceLimit <= 0 || policy.SpeedLimitMbps < 0 || policy.MaxActiveSubscriptions < 0 || policy.FamilyLimit < 0 {
		return normalizedPlanPolicy{}, errors.New("plan traffic, device, speed, capacity or family policy is invalid")
	}
	if policy.ResetPolicy < 0 || policy.ResetPolicy > 5 {
		return normalizedPlanPolicy{}, errors.New("reset_policy must be between 0 and 5")
	}
	if !validTrafficCalcMode(policy.TrafficCalcMode) {
		return normalizedPlanPolicy{}, errors.New("traffic_calc_mode must be 0, 1 or 2")
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
	if req.Code == "" || req.Name == "" || req.Currency == "" {
		return model.PlanSKU{}, errors.New("sku code, name and currency are required")
	}
	switch req.SKUType {
	case "new", "renewal", "upgrade", "traffic_pack":
	default:
		return model.PlanSKU{}, errors.New("sku_type must be new, renewal, upgrade or traffic_pack")
	}
	switch req.BillingUnit {
	case "day", "month", "year", "once":
		if req.BillingValue <= 0 {
			return model.PlanSKU{}, errors.New("billing_value must be positive")
		}
	default:
		return model.PlanSKU{}, errors.New("billing_unit must be day, month, year or once")
	}
	if req.BillingUnit == "once" && req.SKUType != "traffic_pack" {
		return model.PlanSKU{}, errors.New("once billing is only valid for traffic_pack skus")
	}
	if req.PriceCents < 0 || req.TrafficBytes <= 0 || req.DeviceLimit <= 0 || req.SpeedLimitMbps < 0 {
		return model.PlanSKU{}, errors.New("sku price, traffic, device or speed specification is invalid")
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
		BadRequest(w, err.Error())
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
		BadRequest(w, err.Error())
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
				BadRequest(w, "an active plan must retain at least one active sku")
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
	query := h.db.Order("id desc")

	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			parsed, parseErr := strconv.ParseUint(target, 10, 64)
			if parseErr != nil {
				BadRequest(w, "invalid user_id")
				return
			}
			query = query.Where("user_id = ?", parsed)
		}
	} else {
		query = query.Where("user_id = ?", claims.UserID)
	}

	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !h.isValidOrderStatus(status) {
			BadRequest(w, "invalid status")
			return
		}
		query = query.Where("status = ?", status)
	}

	if err := query.Find(&orders).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, orders)
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
	order := model.Order{
		UserID: claims.UserID, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: uuid.NewString(), OrderType: orderType, TargetSubscriptionID: targetSubscriptionID,
		AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents, Currency: sku.Currency,
		Channel: channel, Status: orderStatusPending,
		PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit,
		BillingValue: sku.BillingValue, TrafficBytes: sku.TrafficBytes,
		DeviceLimit: sku.DeviceLimit, SpeedLimitMbps: sku.SpeedLimitMbps,
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
		nextResetAt := nextTrafficReset(now, plan.ResetPolicy)
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
			ResetPolicy: plan.ResetPolicy, NextResetAt: nextResetAt,
			TrafficCalcMode: plan.TrafficCalcMode,
			Config:          "{}",
		}
		if err := tx.Create(&sub).Error; err != nil {
			return model.Subscription{}, err
		}
		if err := createQuotaEvent(tx, sub, "purchase", order.TrafficBytes, 0, sub.FlowTotal, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
			return model.Subscription{}, err
		}
		return sub, nil
	}

	if order.OrderType != "traffic_pack" {
		if !plan.IsRenewable && order.OrderType == "renewal" {
			return model.Subscription{}, errors.New("plan does not support renewal")
		}
		sub.EndAt, err = addBillingPeriod(sub.EndAt, order.BillingUnit, order.BillingValue)
		if err != nil {
			return model.Subscription{}, err
		}
	}
	before := sub.FlowTotal - sub.FlowUsed
	sub.FlowTotal += order.TrafficBytes
	sub.Status = subStatusActive
	if order.OrderType == "upgrade" {
		sub.PlanID = plan.ID
		sub.PlanSKUID = sku.ID
		sub.NodeGroupID = plan.NodeGroupID
		sub.SpeedLimitMbps = order.SpeedLimitMbps
		sub.DeviceLimit = order.DeviceLimit
		sub.FamilyLimit = plan.FamilyLimit
		sub.ResetPolicy = plan.ResetPolicy
		sub.NextResetAt = nextTrafficReset(now, plan.ResetPolicy)
		sub.TrafficCalcMode = plan.TrafficCalcMode
	}
	if plan.IsRenewable {
		sub.RenewalPriceMinor = sku.PriceCents
	} else {
		sub.RenewalPriceMinor = 0
	}

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, err
	}
	if err := createQuotaEvent(tx, sub, order.OrderType, order.TrafficBytes, before, before+order.TrafficBytes, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
		return model.Subscription{}, err
	}
	return sub, nil
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
		return time.Time{}, errors.New("once billing does not create a subscription period")
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
	return query.Update("status", subStatusExpired).Error
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
			if parsed, parseErr := strconv.ParseUint(target, 10, 64); parseErr == nil {
				scopeUserID = uint(parsed)
			} else {
				BadRequest(w, "invalid user_id")
				return
			}
		} else {
			scopeUserID = 0
		}
	}
	if err := expireSubscriptions(h.db, scopeUserID, time.Now().UTC()); err != nil {
		ServerError(w, err)
		return
	}

	var subs []model.Subscription
	query := h.db.Order("id desc")
	if scopeUserID != 0 {
		query = query.Where("user_id = ?", scopeUserID)
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&subs).Error; err != nil {
		ServerError(w, err)
		return
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

	OK(w, map[string]interface{}{
		"configured":   token.RevokedAt == nil,
		"token_prefix": token.TokenPrefix,
		"last_used_at": token.LastUsedAt,
		"revoked_at":   token.RevokedAt,
		"updated_at":   token.UpdatedAt,
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

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var token model.SubscriptionToken
		findErr := tx.Where("user_id = ?", claims.UserID).First(&token).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		token.UserID = claims.UserID
		token.TokenHash = tokenHash
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
			UserID: claims.UserID,
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
		"notice":           "token is shown once; rotating invalidates the previous URL",
	})
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
		NotFound(w)
		return
	}

	var access model.SubscriptionToken
	if err := h.db.Where("token_hash = ? AND revoked_at IS NULL", hashSubscriptionToken(rawToken)).First(&access).Error; err != nil {
		NotFound(w)
		return
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", access.UserID, userStatusActive).First(&user).Error; err != nil {
		NotFound(w)
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
	).Order("end_at asc").Find(&subscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}
	if len(subscriptions) == 0 {
		Forbidden(w, "subscription is inactive, expired, or out of traffic")
		return
	}

	nodeGroupIDs := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		nodeGroupIDs = append(nodeGroupIDs, subscription.NodeGroupID)
	}
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
	manifestNodes := make([]subscriptionManifestNode, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !json.Valid([]byte(endpoint.ClientConfig)) {
			continue
		}
		var node model.Node
		if err := h.db.Select("id", "region").First(&node, endpoint.NodeID).Error; err != nil {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Region: node.Region,
			Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort, Protocol: endpoint.Protocol,
			MultiplierMilli: endpoint.MultiplierMilli, Config: json.RawMessage(endpoint.ClientConfig),
		})
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
	OK(w, map[string]interface{}{
		"version":      "zboard.subscription/v1",
		"generated_at": now.Format(time.RFC3339),
		"subscription": map[string]interface{}{
			"expires_at":     expiresAt.Format(time.RFC3339),
			"flow_total":     total,
			"flow_used":      used,
			"flow_remaining": total - used,
		},
		"protocol_endpoints": manifestNodes,
	})
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
	if err := expireSubscriptions(h.db, userFilter, now); err != nil {
		ServerError(w, err)
		return
	}

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
	if err := expireSubscriptions(h.db, 0, now); err != nil {
		ServerError(w, err)
		return
	}

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
	if err := h.db.Model(&model.Ticket{}).Where("status IN ?", []string{ticketStatusOpen, ticketStatusPendingAdmin}).Count(&pendingTickets).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.Task{}).Where("status = ?", taskStatusFailed).Count(&failedTasks).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.ProtocolDeployment{}).Where("status = ?", "failed").Count(&failedDeployments).Error; err != nil {
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
		"users":                users,
		"users_active":         activeUsers,
		"nodes":                nodes,
		"plans":                plans,
		"orders":               orders,
		"orders_paid":          paidOrders,
		"orders_pending":       pendingOrders,
		"orders_failed":        failedOrders,
		"revenue_cents":        paidRevenue,
		"subscriptions":        subscriptions,
		"subscriptions_active": activeSubscriptions,
		"traffic_pool_bytes":   trafficTotal,
		"nodes_offline":        offlineNodes,
		"tickets_pending":      pendingTickets,
		"tasks_failed":         failedTasks,
		"deployments_failed":   failedDeployments,
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

	query := h.db.Model(&model.AuditLog{})
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
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"items":  logs,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
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

	records := make([]model.TrafficRecord, 0)
	query := h.db.Order("id desc")
	if !adminScope {
		query = query.Where("user_id = ?", claims.UserID)
	} else if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid user_id")
			return
		}
		query = query.Where("user_id = ?", parsed)
	}
	if target := strings.TrimSpace(r.URL.Query().Get("node_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid node_id")
			return
		}
		query = query.Where("node_id = ?", parsed)
	}
	if target := strings.TrimSpace(r.URL.Query().Get("protocol_endpoint_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid protocol_endpoint_id")
			return
		}
		query = query.Where("protocol_endpoint_id = ?", parsed)
	}
	if target := strings.TrimSpace(r.URL.Query().Get("subscription_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid subscription_id")
			return
		}
		query = query.Where("subscription_id = ?", parsed)
	}

	if err := query.Find(&records).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, records)
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
	if err := expireSubscriptions(h.db, userID, time.Now().UTC()); err != nil {
		ServerError(w, err)
		return
	}

	subscriptions := make([]model.Subscription, 0)
	query := h.db.Order("id desc")
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if target := strings.TrimSpace(r.URL.Query().Get("subscription_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid subscription_id")
			return
		}
		query = query.Where("id = ?", parsed)
	}
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
			Status:         subscription.Status,
			FlowUsed:       subscription.FlowUsed,
			RecordedBytes:  recorded,
			Difference:     difference,
			Result:         trafficReconciliationResult(difference),
		})
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
		UserID: claims.UserID,
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

func validateNodeProtocolConfigs(protocol, serverConfig, clientConfig string) error {
	var serverObject map[string]interface{}
	if strings.TrimSpace(serverConfig) == "" || json.Unmarshal([]byte(serverConfig), &serverObject) != nil || serverObject == nil {
		return errors.New("config must be a JSON object")
	}
	serverType, ok := serverObject["type"].(string)
	if !ok || !strings.EqualFold(strings.TrimSpace(serverType), strings.TrimSpace(protocol)) {
		return errors.New("config.type must match protocol")
	}
	var clientObject map[string]interface{}
	if strings.TrimSpace(clientConfig) == "" || json.Unmarshal([]byte(clientConfig), &clientObject) != nil || clientObject == nil {
		return errors.New("client_config must be a JSON object")
	}
	return nil
}

func validateOptionalJSONObject(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil || decoded == nil {
		return fmt.Errorf("%s must be a JSON object", name)
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
		return fmt.Errorf("%s must be a JSON array", name)
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
		return errors.New("protocol cannot be its own parent")
	}
	visited := map[uint]struct{}{}
	if endpointID != 0 {
		visited[endpointID] = struct{}{}
	}
	current := *parentID
	for depth := 0; depth < 128 && current != 0; depth++ {
		if _, exists := visited[current]; exists {
			return errors.New("protocol parent relationship contains a cycle")
		}
		visited[current] = struct{}{}
		var parent model.ProtocolEndpoint
		if err := h.db.Select("id", "node_id", "parent_protocol_id").First(&parent, current).Error; err != nil {
			return errors.New("parent protocol not found")
		}
		if parent.NodeID != nodeID {
			return errors.New("parent protocol must belong to the same node")
		}
		if parent.ParentProtocolID == nil {
			return nil
		}
		current = *parent.ParentProtocolID
	}
	if current != 0 {
		return errors.New("protocol parent chain is too deep")
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
			return errors.New("node ssh_privilege_password is required for su")
		}
		return nil
	default:
		return errors.New("node ssh_privilege_mode must be none, sudo, or su")
	}
}

func validateSSHFields(host string, port int, user string, authMethod string, credential string, fingerprint string) error {
	if strings.TrimSpace(host) == "" {
		return errors.New("node ssh_host is required")
	}
	if strings.TrimSpace(user) == "" {
		return errors.New("node ssh_user is required")
	}
	if port <= 0 || port > 65535 {
		return errors.New("node ssh_port is invalid")
	}
	if authMethod != sshAuthPassword && authMethod != sshAuthPrivateKey {
		return errors.New("node ssh_auth_method must be password or private_key")
	}
	if strings.TrimSpace(credential) == "" {
		return errors.New("node ssh credential is required")
	}
	if strings.TrimSpace(fingerprint) != "" {
		return validateSSHHostKeyFingerprint(fingerprint)
	}
	return nil
}

func parseSSHPrivateKey(privateKey string, passphrase string) (ssh.Signer, error) {
	if strings.TrimSpace(privateKey) == "" {
		return nil, errors.New("node ssh_private_key is required")
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
		return nil, fmt.Errorf("node ssh_private_key is invalid: %w", err)
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
