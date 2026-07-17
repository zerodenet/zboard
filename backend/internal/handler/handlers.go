package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/version"
)

const (
	orderStatusPending    = "pending"
	orderStatusPaid       = "paid"
	orderStatusFailed     = "failed"
	orderStatusCanceled   = "canceled"
	orderStatusSuccess    = "success"
	subStatusActive       = "active"
	subStatusExpired      = "expired"
	userStatusActive      = "active"
	userStatusSuspended   = "suspended"
	userStatusDeactivated = "deactivated"
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
	Name     string `json:"name"`
	Region   string `json:"region"`
	Address  string `json:"address"`
	Protocol string `json:"protocol"`
	SSHHost  string `json:"ssh_host"`
	SSHPort  int    `json:"ssh_port"`
	SSHUser  string `json:"ssh_user"`
	SSHPwd   string `json:"ssh_password"`
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

type orderActionReq struct {
	Force bool `json:"force"`
}

type trafficReportReq struct {
	UserID    uint   `json:"user_id"`
	NodeID    uint   `json:"node_id"`
	UsedBytes int64  `json:"used_bytes"`
	Meta      string `json:"meta"`
}

type nodeSSHTestReq struct {
	NodeID uint `json:"node_id"`
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
	db        *gorm.DB
	jwtSecret string
}

func NewHandlers(db *gorm.DB, jwtSecret string) (*handlers, error) {
	if err := cfgpkg.ValidateJWTSecret(jwtSecret); err != nil {
		return nil, err
	}
	return &handlers{
		db:        db,
		jwtSecret: jwtSecret,
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
	_, err := h.requireAdmin(w, r)
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
	if err := h.db.Create(&user).Error; err != nil {
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
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !h.isValidUserStatus(status) {
			BadRequest(w, "invalid user status")
			return
		}
		updates["status"] = status
	}
	if req.IsAdmin != nil {
		updates["is_admin"] = *req.IsAdmin
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

	if err := h.db.Model(&target).Updates(updates).Error; err != nil {
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

	node := model.Node{
		Name:     req.Name,
		Region:   req.Region,
		Address:  req.Address,
		Protocol: req.Protocol,
		IsOnline: false,
		SSHHost:  req.SSHHost,
		SSHPort:  req.SSHPort,
		SSHUser:  req.SSHUser,
		SSHPwd:   req.SSHPwd,
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

func (h *handlers) NodeProtocolConfigHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
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
	if strings.TrimSpace(req.Config) == "" {
		BadRequest(w, "config is required")
		return
	}
	if strings.TrimSpace(req.ClientConfig) != "" && !json.Valid([]byte(req.ClientConfig)) {
		BadRequest(w, "client_config must be valid JSON")
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
	if strings.TrimSpace(req.ClientConfig) != "" {
		updates["client_config"] = req.ClientConfig
		node.ClientConfig = req.ClientConfig
	}
	if saveErr := h.db.Model(&node).Updates(updates).Error; saveErr != nil {
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

	if order.Status == orderStatusPaid {
		OK(w, order)
		return
	}

	if !parseBoolQuery(r.URL.Query().Get("force")) && order.Status != orderStatusPending {
		BadRequest(w, "order not payable")
		return
	}

	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			ServerError(w, fmt.Errorf("internal server panic: %v", r))
		}
	}()

	if err := h.setOrderPaid(tx, &order); err != nil {
		tx.Rollback()
		ServerError(w, err)
		return
	}
	if err := tx.Commit().Error; err != nil {
		ServerError(w, err)
		return
	}

	if err := h.db.First(&order, orderID).Error; err != nil {
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

	if order.Status == orderStatusCanceled {
		OK(w, order)
		return
	}
	if !parseBoolQuery(r.URL.Query().Get("force")) && order.Status != orderStatusPending {
		BadRequest(w, "order not cancelable")
		return
	}

	if err := h.db.Model(&order).Updates(map[string]interface{}{
		"status":     orderStatusCanceled,
		"updated_at": time.Now(),
	}).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.First(&order, orderID).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

func (h *handlers) OrderPayCallbackHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
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

	var order model.Order
	if err := h.db.First(&order, orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "order not found")
			return
		}
		ServerError(w, err)
		return
	}

	if status == orderStatusSuccess {
		status = orderStatusPaid
	}
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			ServerError(w, fmt.Errorf("internal server panic: %v", r))
		}
	}()

	if status == orderStatusPaid {
		if err := h.setOrderPaid(tx, &order); err != nil {
			tx.Rollback()
			ServerError(w, err)
			return
		}
		if err := tx.Model(&order).Updates(map[string]interface{}{
			"raw_callback": req.RawCallback,
		}).Error; err != nil {
			tx.Rollback()
			ServerError(w, err)
			return
		}
	} else {
		if err := tx.Model(&order).Updates(map[string]interface{}{
			"status":       status,
			"raw_callback": req.RawCallback,
			"updated_at":   time.Now(),
		}).Error; err != nil {
			tx.Rollback()
			ServerError(w, err)
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		ServerError(w, err)
		return
	}

	if err := h.db.First(&order, orderID).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

func (h *handlers) setOrderPaid(tx *gorm.DB, order *model.Order) error {
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

	now := time.Now()
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

	duration := time.Duration(plan.DurationDay) * 24 * time.Hour
	additionalFlow := plan.TrafficGB * 1024 * 1024 * 1024
	if additionalFlow <= 0 {
		additionalFlow = 0
	}

	var sub model.Subscription
	err := tx.Where("user_id = ? AND plan_id = ? AND status = ?", userID, planID, subStatusActive).Order("end_at desc").First(&sub).Error
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

	if sub.EndAt.Before(now) {
		sub.StartAt = now
		sub.EndAt = now.Add(duration)
		sub.FlowUsed = 0
		sub.FlowTotal = additionalFlow
		sub.Status = subStatusActive
	} else {
		sub.EndAt = sub.EndAt.Add(duration)
		sub.FlowTotal += additionalFlow
	}
	sub.Status = subStatusActive

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, err
	}
	return sub, nil
}

func (h *handlers) SubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var subs []model.Subscription
	query := h.db.Order("id desc")
	if !claims.IsAdmin {
		query = query.Where("user_id = ?", claims.UserID)
	} else if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
		if parsed, parseErr := strconv.ParseUint(target, 10, 64); parseErr == nil {
			query = query.Where("user_id = ?", parsed)
		} else {
			BadRequest(w, "invalid user_id")
			return
		}
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
	result := h.db.Model(&model.SubscriptionToken{}).
		Where("user_id = ? AND revoked_at IS NULL", claims.UserID).
		Update("revoked_at", now)
	if result.Error != nil {
		ServerError(w, result.Error)
		return
	}
	OK(w, map[string]interface{}{"configured": false, "revoked": result.RowsAffected > 0})
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
	subQuery = subQuery.Where("status = ?", subStatusActive).Select("COALESCE(SUM(flow_total - flow_used), 0)")
	if err := subQuery.Scan(&currentRemain).Error; err != nil {
		ServerError(w, err)
		return
	}

	todayStart := time.Now().Truncate(24 * time.Hour)
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
		"as_of":            time.Now().Format(time.RFC3339),
	})
}

func (h *handlers) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(w, r)
	if err != nil {
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
	if err := h.db.Model(&model.Subscription{}).Where("status = ?", subStatusActive).Count(&activeSubscriptions).Error; err != nil {
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
	if err := h.db.Model(&model.Subscription{}).Where("status = ?", subStatusActive).Select("COALESCE(SUM(flow_total), 0)").Scan(&trafficTotal).Error; err != nil {
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

func (h *handlers) TrafficRecordsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	records := make([]model.TrafficRecord, 0)
	query := h.db.Order("id desc")
	if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
		if !claims.IsAdmin {
			BadRequest(w, "only admin can query other users")
			return
		}
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil {
			BadRequest(w, "invalid user_id")
			return
		}
		query = query.Where("user_id = ?", parsed)
	} else if claims.IsAdmin {
		if targetNode := strings.TrimSpace(r.URL.Query().Get("node_id")); targetNode != "" {
			parsed, parseErr := strconv.ParseUint(targetNode, 10, 64)
			if parseErr != nil {
				BadRequest(w, "invalid node_id")
				return
			}
			query = query.Where("node_id = ?", parsed)
		}
	} else {
		query = query.Where("user_id = ?", claims.UserID)
	}

	if err := query.Find(&records).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, records)
}

func (h *handlers) TrafficReportHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}

	var req trafficReportReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}

	if req.UsedBytes <= 0 {
		BadRequest(w, "used_bytes must be > 0")
		return
	}

	targetUser := req.UserID
	if targetUser == 0 {
		targetUser = claims.UserID
	}
	if targetUser != claims.UserID && !claims.IsAdmin {
		Unauthorized(w, "can only report traffic for your own account")
		return
	}

	nodeID := req.NodeID

	if nodeID != 0 {
		if _, err := h.loadNode(nodeID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				BadRequest(w, "node_id not found")
				return
			}
			ServerError(w, err)
			return
		}
	}

	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			ServerError(w, fmt.Errorf("internal server panic: %v", r))
		}
	}()

	now := time.Now()
	var sub model.Subscription
	if err := tx.Where("user_id = ? AND status = ? AND end_at > ?", targetUser, subStatusActive, now).Order("end_at desc").First(&sub).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "no active subscription")
			return
		}
		ServerError(w, err)
		return
	}

	used := req.UsedBytes
	remainBefore := sub.FlowTotal - sub.FlowUsed
	if remainBefore <= 0 {
		sub.Status = subStatusExpired
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":    sub.Status,
			"flow_used": sub.FlowUsed,
		}).Error; err != nil {
			tx.Rollback()
			ServerError(w, err)
			return
		}
		if err := tx.Commit().Error; err != nil {
			ServerError(w, err)
			return
		}
		BadRequest(w, "subscription quota exhausted")
		return
	}
	if used > remainBefore {
		used = remainBefore
	}

	sub.FlowUsed = sub.FlowUsed + used
	if sub.FlowUsed >= sub.FlowTotal {
		sub.Status = subStatusExpired
	}

	if err := tx.Save(&sub).Error; err != nil {
		tx.Rollback()
		ServerError(w, err)
		return
	}

	record := model.TrafficRecord{
		UserID:    targetUser,
		NodeID:    nodeID,
		UsedBytes: used,
		At:        now,
		Meta:      req.Meta,
	}
	if err := tx.Create(&record).Error; err != nil {
		tx.Rollback()
		ServerError(w, err)
		return
	}
	if err := tx.Commit().Error; err != nil {
		ServerError(w, err)
		return
	}

	OK(w, map[string]interface{}{
		"subscription_id":  sub.ID,
		"node_id":          nodeID,
		"used_bytes":       used,
		"flow_used":        sub.FlowUsed,
		"flow_total":       sub.FlowTotal,
		"flow_remaining":   sub.FlowTotal - sub.FlowUsed,
		"subscription_end": sub.EndAt.Format(time.RFC3339),
	})
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

func (h *handlers) validateNodeSSH(node model.Node) error {
	if strings.TrimSpace(node.SSHHost) == "" {
		return errors.New("node ssh_host is required")
	}
	if strings.TrimSpace(node.SSHUser) == "" {
		return errors.New("node ssh_user is required")
	}
	if node.SSHPort <= 0 {
		return errors.New("node ssh_port is invalid")
	}
	if strings.TrimSpace(node.SSHPwd) == "" {
		return errors.New("node ssh_password is required")
	}
	return nil
}

func (h *handlers) execSSHCommand(node model.Node, command string) (string, time.Duration, error) {
	start := time.Now()
	addr := fmt.Sprintf("%s:%d", strings.TrimSpace(node.SSHHost), node.SSHPort)
	conf := &ssh.ClientConfig{
		User:            strings.TrimSpace(node.SSHUser),
		Auth:            []ssh.AuthMethod{ssh.Password(node.SSHPwd)},
		Timeout:         12 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
