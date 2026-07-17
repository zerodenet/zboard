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
	"net"
	"net/http"
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
)

var (
	errNodeReportCredentialChanged = errors.New("node report credential changed")
	errNodeReportNonceReplayed     = errors.New("node report nonce replayed")
	errSubscriptionNotFound        = errors.New("no active subscription")
	errSubscriptionQuotaExhausted  = errors.New("subscription quota exhausted")
	errOrderNotPayable             = errors.New("order not payable")
	errOrderNotCancelable          = errors.New("order not cancelable")
	errOrderTransitionRejected     = errors.New("order status transition rejected")
)

var supportedProtocols = map[string]struct{}{
	"vmess":       {},
	"vless":       {},
	"trojan":      {},
	"shadowsocks": {},
	"hysteria":    {},
}

type authClaims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"u"`
	IsAdmin  bool   `json:"a"`
	Expiry   int64  `json:"exp"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type userPublic struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	Status   string `json:"status"`
}

type nodeCreateReq struct {
	Name                  string `json:"name"`
	Region                string `json:"region"`
	Address               string `json:"address"`
	Protocol              string `json:"protocol"`
	SSHHost               string `json:"ssh_host"`
	SSHPort               int    `json:"ssh_port"`
	SSHUser               string `json:"ssh_user"`
	SSHPwd                string `json:"ssh_password"`
	SSHHostKeyFingerprint string `json:"ssh_host_key_fingerprint"`
}

type planCreateReq struct {
	Name        string `json:"name"`
	PriceCents  int64  `json:"price_cents"`
	TrafficGB   int64  `json:"traffic_gb"`
	DurationDay int    `json:"duration_day"`
	MaxDevice   int    `json:"max_device"`
}

type planUpdateReq struct {
	Name        *string `json:"name"`
	PriceCents  *int64  `json:"price_cents"`
	TrafficGB   *int64  `json:"traffic_gb"`
	DurationDay *int    `json:"duration_day"`
	MaxDevice   *int    `json:"max_device"`
	IsActive    *bool   `json:"is_active"`
}

type orderCreateReq struct {
	PlanID  uint   `json:"plan_id"`
	Channel string `json:"channel"`
}

type orderCallbackReq struct {
	Status      string `json:"status"`
	RawCallback string `json:"raw_callback"`
}

type trafficReportReq struct {
	ReportID  string `json:"report_id"`
	UserID    uint   `json:"user_id"`
	UsedBytes int64  `json:"used_bytes"`
	Meta      string `json:"meta"`
}

type authenticatedNodeReport struct {
	node      model.Node
	timestamp time.Time
	nonce     string
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
	SSHHost               string `json:"ssh_host"`
	SSHPort               int    `json:"ssh_port"`
	SSHUser               string `json:"ssh_user"`
	SSHPwd                string `json:"ssh_password"`
	SSHHostKeyFingerprint string `json:"ssh_host_key_fingerprint"`
}

type nodeProtocolConfigReq struct {
	NodeID       uint   `json:"node_id"`
	Protocol     string `json:"protocol"`
	Config       string `json:"config"`
	ClientConfig string `json:"client_config"`
}

type subscriptionManifestNode struct {
	ID       uint            `json:"id"`
	Name     string          `json:"name"`
	Region   string          `json:"region"`
	Address  string          `json:"address"`
	Protocol string          `json:"protocol"`
	Config   json.RawMessage `json:"config"`
}

type adminUserCreateReq struct {
	Username string `json:"username"`
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

type handlers struct {
	db               *gorm.DB
	jwtSecret        string
	credentialCipher *security.CredentialCipher
}

func NewHandlers(db *gorm.DB, jwtSecret string, credentialCipher *security.CredentialCipher) (*handlers, error) {
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
	type req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	var body req
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if body.Password == "" || (body.Username == "" && body.Email == "") {
		BadRequest(w, "username/email and password required")
		return
	}
	if body.Email == "" {
		body.Email = fmt.Sprintf("%s@local", body.Username)
	}
	if body.Username == "" {
		body.Username = body.Email
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}

	user := model.User{
		Username: body.Username,
		Email:    body.Email,
		Password: string(hash),
		IsAdmin:  false,
		Status:   userStatusActive,
	}

	if err := h.db.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") {
			BadRequest(w, "username or email already exists")
			return
		}
		ServerError(w, err)
		return
	}

	auth := authClaims{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		Expiry:   0,
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
		Account  string `json:"account"`
		Password string `json:"password"`
	}

	var body req
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	account := strings.TrimSpace(body.Account)
	if account == "" || body.Password == "" {
		BadRequest(w, "account and password required")
		return
	}

	var user model.User
	query := h.db.Model(&model.User{}).Where("(username = ? OR email = ?) AND status = ?", account, account, userStatusActive)
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Unauthorized(w, "invalid account or password")
			return
		}
		ServerError(w, err)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)); err != nil {
		Unauthorized(w, "invalid account or password")
		return
	}

	token, expiresAt, err := h.issueToken(authClaims{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
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
		query = query.Where("username LIKE ? OR email LIKE ?", like, like)
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
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.Email == "" || req.Password == "" {
		BadRequest(w, "username, email and password are required")
		return
	}
	if len(req.Password) < 6 {
		BadRequest(w, "password must be at least 6 characters")
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
		Username: req.Username,
		Email:    req.Email,
		Password: string(hash),
		IsAdmin:  req.IsAdmin,
		Status:   status,
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "user.create", fmt.Sprintf("user:%d", user.ID), fmt.Sprintf("status=%s admin=%t", user.Status, user.IsAdmin))
	}); err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") {
			BadRequest(w, "username or email already exists")
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
		newPassword := strings.TrimSpace(*req.Password)
		if len(newPassword) < 6 {
			BadRequest(w, "password must be at least 6 characters")
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
	if _, err := h.authFromRequest(r); err != nil {
		Unauthorized(w, err.Error())
		return
	}

	nodes := make([]model.Node, 0)
	if err := h.db.Order("id desc").Find(&nodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, nodes)
}

func (h *handlers) NodeCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	_ = claims

	var req nodeCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.Address == "" || req.Region == "" || req.Protocol == "" {
		BadRequest(w, "name, region, address and protocol are required")
		return
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if !h.isProtocolSupported(protocol) {
		BadRequest(w, "unsupported protocol")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	req.Protocol = protocol
	req.SSHHost = strings.TrimSpace(req.SSHHost)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.SSHHostKeyFingerprint = strings.TrimSpace(req.SSHHostKeyFingerprint)
	sshConfigured := req.SSHHost != "" || req.SSHUser != "" || req.SSHPwd != "" || req.SSHHostKeyFingerprint != ""
	if sshConfigured {
		if err := validateSSHFields(req.SSHHost, req.SSHPort, req.SSHUser, req.SSHPwd, req.SSHHostKeyFingerprint); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	encryptedPassword, err := h.credentialCipher.Encrypt(req.SSHPwd)
	if err != nil {
		ServerError(w, err)
		return
	}

	node := model.Node{
		Name:                  req.Name,
		Region:                req.Region,
		Address:               req.Address,
		Protocol:              req.Protocol,
		IsOnline:              false,
		SSHHost:               req.SSHHost,
		SSHPort:               req.SSHPort,
		SSHUser:               req.SSHUser,
		SSHPwd:                encryptedPassword,
		SSHHostKeyFingerprint: req.SSHHostKeyFingerprint,
	}
	if err := h.db.Create(&node).Error; err != nil {
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

	output, elapsed, execErr := h.execSSHCommand(node, "echo zboard-node-ok")
	now := time.Now().UTC()
	node.IsOnline = execErr == nil
	node.LastSeenAt = &now
	if saveErr := h.db.Model(&node).Updates(map[string]interface{}{
		"is_online":    node.IsOnline,
		"last_seen_at": node.LastSeenAt,
	}).Error; saveErr != nil {
		ServerError(w, saveErr)
		return
	}

	if execErr != nil {
		BadRequest(w, execErr.Error())
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
	req.SSHHostKeyFingerprint = strings.TrimSpace(req.SSHHostKeyFingerprint)
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	encryptedPassword := node.SSHPwd
	if req.SSHPwd != "" {
		encryptedPassword, err = h.credentialCipher.Encrypt(req.SSHPwd)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	if err := validateSSHFields(req.SSHHost, req.SSHPort, req.SSHUser, encryptedPassword, req.SSHHostKeyFingerprint); err != nil {
		BadRequest(w, err.Error())
		return
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(map[string]interface{}{
			"ssh_host":                 req.SSHHost,
			"ssh_port":                 req.SSHPort,
			"ssh_user":                 req.SSHUser,
			"ssh_pwd":                  encryptedPassword,
			"ssh_host_key_fingerprint": req.SSHHostKeyFingerprint,
			"is_online":                false,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{
			UserID: claims.UserID,
			Actor:  claims.Username,
			Action: "node.ssh_config.update",
			Target: fmt.Sprintf("node:%d", node.ID),
			Detail: "host key fingerprint and encrypted credential updated",
		}).Error
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	node.SSHHost = req.SSHHost
	node.SSHPort = req.SSHPort
	node.SSHUser = req.SSHUser
	node.SSHPwd = encryptedPassword
	node.SSHHostKeyFingerprint = req.SSHHostKeyFingerprint
	node.IsOnline = false
	OK(w, node)
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
			Actor:  claims.Username,
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
			Actor:  claims.Username,
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

func (h *handlers) NodeProtocolConfigHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req nodeProtocolConfigReq
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
	if err := validateNodeProtocolConfigs(req.Config, req.ClientConfig); err != nil {
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
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(req.Config))
	remotePath := fmt.Sprintf("/etc/zerodenet/protocols/%s.json", protocol)
	command := fmt.Sprintf("mkdir -p /etc/zerodenet/protocols && printf '%s' | base64 -d > %s", encoded, remotePath)
	output, elapsed, execErr := h.execSSHCommand(node, command)
	if execErr != nil {
		BadRequest(w, execErr.Error())
		return
	}

	now := time.Now().UTC()
	node.Protocol = protocol
	node.ProtocolConfig = req.Config
	node.IsOnline = true
	node.LastSeenAt = &now
	updates := map[string]interface{}{
		"protocol":        protocol,
		"protocol_config": req.Config,
		"is_online":       true,
		"last_seen_at":    now,
	}
	updates["client_config"] = req.ClientConfig
	node.ClientConfig = req.ClientConfig
	if saveErr := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&node).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.protocol_config.publish", fmt.Sprintf("node:%d", node.ID), protocol)
	}); saveErr != nil {
		ServerError(w, saveErr)
		return
	}

	OK(w, map[string]interface{}{
		"node":       node,
		"output":     strings.TrimSpace(output),
		"latency_ms": elapsed.Milliseconds(),
	})
}

func (h *handlers) PlanListHandler(w http.ResponseWriter, r *http.Request) {
	plans := make([]model.Plan, 0)
	query := h.db.Order("id desc")
	claims, claimErr := h.authFromRequest(r)
	isAdmin := claimErr == nil && claims.IsAdmin

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
	OK(w, plans)
}

func (h *handlers) PlanCreateHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}

	var req planCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.PriceCents < 0 || req.TrafficGB <= 0 || req.DurationDay <= 0 {
		BadRequest(w, "invalid plan data")
		return
	}
	if req.MaxDevice <= 0 {
		req.MaxDevice = 1
	}

	plan := model.Plan{
		Name:        req.Name,
		PriceCents:  req.PriceCents,
		TrafficGB:   req.TrafficGB,
		DurationDay: req.DurationDay,
		MaxDevice:   req.MaxDevice,
		IsActive:    true,
	}
	if err := h.db.Create(&plan).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, plan)
}

func (h *handlers) PlanUpdateHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
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

	updates := make(map[string]interface{})
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			BadRequest(w, "name cannot be empty")
			return
		}
		updates["name"] = name
	}
	if req.PriceCents != nil {
		if *req.PriceCents < 0 {
			BadRequest(w, "price_cents must be >= 0")
			return
		}
		updates["price_cents"] = *req.PriceCents
	}
	if req.TrafficGB != nil {
		if *req.TrafficGB <= 0 {
			BadRequest(w, "traffic_gb must be > 0")
			return
		}
		updates["traffic_gb"] = *req.TrafficGB
	}
	if req.DurationDay != nil {
		if *req.DurationDay <= 0 {
			BadRequest(w, "duration_day must be > 0")
			return
		}
		updates["duration_day"] = *req.DurationDay
	}
	if req.MaxDevice != nil {
		if *req.MaxDevice <= 0 {
			BadRequest(w, "max_device must be > 0")
			return
		}
		updates["max_device"] = *req.MaxDevice
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if len(updates) == 0 {
		BadRequest(w, "no valid update fields")
		return
	}

	var plan model.Plan
	if err := h.db.First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	if err := h.db.Model(&plan).Updates(updates).Error; err != nil {
		ServerError(w, err)
		return
	}

	if err := h.db.First(&plan, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, plan)
}

func (h *handlers) OrderListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var orders []model.Order
	query := h.db.Order("id desc")

	if claims.IsAdmin {
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
	if req.PlanID == 0 {
		BadRequest(w, "plan_id is required")
		return
	}

	var plan model.Plan
	if err := h.db.Where("is_active = 1").First(&plan, req.PlanID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "plan not found")
			return
		}
		ServerError(w, err)
		return
	}

	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = "manual"
	}
	order := model.Order{
		UserID:      claims.UserID,
		PlanID:      req.PlanID,
		TradeNo:     uuid.NewString(),
		AmountCents: plan.PriceCents,
		Currency:    "USD",
		Channel:     channel,
		Status:      orderStatusPending,
	}
	if err := h.db.Create(&order).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

func (h *handlers) OrderPayHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
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

	if !claims.IsAdmin && order.UserID != claims.UserID {
		Forbidden(w, "no permission")
		return
	}

	force := parseBoolQuery(r.URL.Query().Get("force"))
	if force && !claims.IsAdmin {
		Forbidden(w, "force payment requires admin")
		return
	}

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
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
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

	if !claims.IsAdmin && order.UserID != claims.UserID {
		Forbidden(w, "no permission")
		return
	}

	force := parseBoolQuery(r.URL.Query().Get("force"))
	if force && !claims.IsAdmin {
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
		if err := tx.Model(&order).Updates(map[string]interface{}{
			"status":     order.Status,
			"updated_at": order.UpdatedAt,
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

	var plan model.Plan
	if err := tx.First(&plan, order.PlanID).Error; err != nil {
		return err
	}

	subscription, err := h.allocateOrRenewSubscription(tx, plan.ID, order.UserID, now)
	if err != nil {
		return err
	}

	if err := tx.Model(order).Updates(map[string]interface{}{
		"status":          orderStatusPaid,
		"subscription_id": subscription.ID,
		"updated_at":      now,
	}).Error; err != nil {
		return err
	}
	order.Status = orderStatusPaid
	order.SubscriptionID = subscription.ID
	order.UpdatedAt = now
	return nil
}

func (h *handlers) allocateOrRenewSubscription(tx *gorm.DB, planID uint, userID uint, now time.Time) (model.Subscription, error) {
	if tx == nil {
		tx = h.db
	}
	var plan model.Plan
	if err := tx.First(&plan, planID).Error; err != nil {
		return model.Subscription{}, err
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, userID).Error; err != nil {
		return model.Subscription{}, err
	}
	if err := expireSubscriptions(tx, userID, now); err != nil {
		return model.Subscription{}, err
	}

	duration := time.Duration(plan.DurationDay) * 24 * time.Hour
	additionalFlow := plan.TrafficGB * 1024 * 1024 * 1024
	if additionalFlow <= 0 {
		additionalFlow = 0
	}

	var sub model.Subscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND plan_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", userID, planID, subStatusActive, now).
		Order("end_at desc").First(&sub).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Subscription{}, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		sub = model.Subscription{
			UserID:    userID,
			PlanID:    planID,
			StartAt:   now,
			EndAt:     now.Add(duration),
			Status:    subStatusActive,
			FlowTotal: additionalFlow,
			FlowUsed:  0,
		}
		if err := tx.Create(&sub).Error; err != nil {
			return model.Subscription{}, err
		}
		return sub, nil
	}

	sub.EndAt = sub.EndAt.Add(duration)
	sub.FlowTotal += additionalFlow
	sub.Status = subStatusActive

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, err
	}
	return sub, nil
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
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	scopeUserID := claims.UserID
	if claims.IsAdmin {
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
			Actor:  claims.Username,
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

	var nodes []model.Node
	if err := h.db.Where("is_online = ? AND client_config <> ''", true).Order("region asc, id asc").Find(&nodes).Error; err != nil {
		ServerError(w, err)
		return
	}
	manifestNodes := make([]subscriptionManifestNode, 0, len(nodes))
	for _, node := range nodes {
		if !json.Valid([]byte(node.ClientConfig)) {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: node.ID, Name: node.Name, Region: node.Region, Address: node.Address,
			Protocol: node.Protocol, Config: json.RawMessage(node.ClientConfig),
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
		"nodes": manifestNodes,
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
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	userFilter := claims.UserID
	if claims.IsAdmin {
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
		"revenue_cents":        paidRevenue,
		"subscriptions":        subscriptions,
		"subscriptions_active": activeSubscriptions,
		"traffic_pool_bytes":   trafficTotal,
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
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	records := make([]model.TrafficRecord, 0)
	query := h.db.Order("id desc")
	if !claims.IsAdmin {
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
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	userID := claims.UserID
	if claims.IsAdmin {
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
	if req.UsedBytes <= 0 {
		BadRequest(w, "used_bytes must be > 0")
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
		if lockedNode.TrafficSecret == "" || lockedNode.TrafficSecretRevokedAt != nil || lockedNode.TrafficSecret != authenticated.node.TrafficSecret {
			return errNodeReportCredentialChanged
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
			Where("user_id = ? AND status = ? AND end_at > ?", req.UserID, subStatusActive, now).
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
		used := req.UsedBytes
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

		record = model.TrafficRecord{
			UserID:         req.UserID,
			SubscriptionID: sub.ID,
			NodeID:         lockedNode.ID,
			ReportID:       req.ReportID,
			Nonce:          authenticated.nonce,
			UsedBytes:      used,
			At:             authenticated.timestamp,
			Meta:           req.Meta,
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
		"report_id":       record.ReportID,
		"subscription_id": record.SubscriptionID,
		"node_id":         record.NodeID,
		"used_bytes":      record.UsedBytes,
		"duplicate":       duplicate,
	}
	if !duplicate {
		response["flow_used"] = sub.FlowUsed
		response["flow_total"] = sub.FlowTotal
		response["flow_remaining"] = sub.FlowTotal - sub.FlowUsed
		response["subscription_end"] = sub.EndAt.Format(time.RFC3339)
	}
	OK(w, response)
}

func newNodeReportSecret() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(random)
	return secret, secret[:12], nil
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
	if payload.UserID == 0 || payload.Username == "" {
		return authClaims{}, errors.New("invalid token payload")
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", payload.UserID, userStatusActive).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authClaims{}, errors.New("user not found")
		}
		return authClaims{}, errors.New("token validation failed")
	}
	payload.Username = user.Username
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
		Actor:  claims.Username,
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
	return node, nil
}

func (h *handlers) isProtocolSupported(proto string) bool {
	_, ok := supportedProtocols[strings.ToLower(proto)]
	return ok
}

func validateNodeProtocolConfigs(serverConfig, clientConfig string) error {
	if strings.TrimSpace(serverConfig) == "" || !json.Valid([]byte(serverConfig)) {
		return errors.New("config must be valid JSON")
	}
	if strings.TrimSpace(clientConfig) == "" || !json.Valid([]byte(clientConfig)) {
		return errors.New("client_config must be valid JSON")
	}
	return nil
}

func (h *handlers) validateNodeSSH(node model.Node) error {
	if err := validateSSHFields(node.SSHHost, node.SSHPort, node.SSHUser, node.SSHPwd, node.SSHHostKeyFingerprint); err != nil {
		return err
	}
	if _, err := h.credentialCipher.Decrypt(node.SSHPwd); err != nil {
		return fmt.Errorf("node ssh credential is unavailable: %w", err)
	}
	return nil
}

func (h *handlers) execSSHCommand(node model.Node, command string) (string, time.Duration, error) {
	start := time.Now()
	password, err := h.credentialCipher.Decrypt(node.SSHPwd)
	if err != nil {
		return "", time.Since(start), fmt.Errorf("decrypt node ssh credential: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", strings.TrimSpace(node.SSHHost), node.SSHPort)
	conf := &ssh.ClientConfig{
		User:            strings.TrimSpace(node.SSHUser),
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		Timeout:         12 * time.Second,
		HostKeyCallback: pinnedHostKeyCallback(node.SSHHostKeyFingerprint),
	}
	conn, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return "", time.Since(start), err
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return "", time.Since(start), err
	}
	defer session.Close()

	out, err := session.CombinedOutput(command)
	return string(bytes.TrimSpace(out)), time.Since(start), err
}

func validateSSHFields(host string, port int, user string, password string, fingerprint string) error {
	if strings.TrimSpace(host) == "" {
		return errors.New("node ssh_host is required")
	}
	if strings.TrimSpace(user) == "" {
		return errors.New("node ssh_user is required")
	}
	if port <= 0 || port > 65535 {
		return errors.New("node ssh_port is invalid")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("node ssh_password is required")
	}
	return validateSSHHostKeyFingerprint(fingerprint)
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

func pinnedHostKeyCallback(expectedFingerprint string) ssh.HostKeyCallback {
	expected := strings.TrimSpace(expectedFingerprint)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		actual := ssh.FingerprintSHA256(key)
		if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
			return errors.New("ssh host key fingerprint mismatch")
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
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  user.IsAdmin,
		Status:   user.Status,
	}
}
