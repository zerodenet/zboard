package handler

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	certificateStatusPending  = "pending"
	certificateStatusIssuing  = "issuing"
	certificateStatusActive   = "active"
	certificateStatusRenewing = "renewing"
	certificateStatusFailed   = "failed"
	certificateStatusExpired  = "expired"

	certificateEnvironmentProduction  = "production"
	certificateEnvironmentStaging     = "staging"
	certificateChallengeHTTP01        = "http-01"
	certificateChallengeHTTP01Webroot = "http-01-webroot"
	certificateChallengeDNS01         = "dns-01"

	certificateOperationIssue = "issue"
	certificateOperationRenew = "renew"

	certificateRenewalScanInterval = 6 * time.Hour
	certificateRetryInterval       = 6 * time.Hour
	certificateOperationTimeout    = 8 * time.Minute
)

var (
	errCertificateOperationRunning = errors.New("certificate operation is already running")
	errCertificateRevisionConflict = errors.New("certificate revision conflict")
	domainLabelPattern             = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type certificateWriteRequest struct {
	NodeID            uint     `json:"node_id"`
	ProviderAccountID uint     `json:"provider_account_id"`
	Name              string   `json:"name"`
	Domains           []string `json:"domains"`
	ContactEmail      string   `json:"contact_email"`
	Environment       string   `json:"environment"`
	AutoRenew         *bool    `json:"auto_renew"`
	RenewBeforeDays   int      `json:"renew_before_days"`
	ChallengeType     string   `json:"challenge_type"`
	WebrootPath       string   `json:"webroot_path"`
}

type certificateRenewalUpdateRequest struct {
	AutoRenew        bool   `json:"auto_renew"`
	RenewBeforeDays  int    `json:"renew_before_days"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

type certificateUpdateRequest struct {
	Name             string `json:"name"`
	ContactEmail     string `json:"contact_email"`
	WebrootPath      string `json:"webroot_path"`
	AutoRenew        bool   `json:"auto_renew"`
	RenewBeforeDays  int    `json:"renew_before_days"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

type certificateListItem struct {
	ID                   uint                        `json:"id"`
	NodeID               uint                        `json:"node_id"`
	ProviderAccountID    *uint                       `json:"provider_account_id,omitempty"`
	NodeName             string                      `json:"node_name"`
	Name                 string                      `json:"name"`
	Domains              []string                    `json:"domains"`
	ContactEmail         string                      `json:"contact_email"`
	Environment          string                      `json:"environment"`
	ChallengeType        string                      `json:"challenge_type"`
	WebrootPath          string                      `json:"webroot_path"`
	Status               string                      `json:"status"`
	CertPath             string                      `json:"cert_path"`
	KeyPath              string                      `json:"key_path"`
	SerialNumber         string                      `json:"serial_number"`
	FingerprintSHA256    string                      `json:"fingerprint_sha256"`
	NotBefore            *time.Time                  `json:"not_before,omitempty"`
	NotAfter             *time.Time                  `json:"not_after,omitempty"`
	LastIssuedAt         *time.Time                  `json:"last_issued_at,omitempty"`
	LastRenewalAttemptAt *time.Time                  `json:"last_renewal_attempt_at,omitempty"`
	NextRenewalAt        *time.Time                  `json:"next_renewal_at,omitempty"`
	AutoRenew            bool                        `json:"auto_renew"`
	RenewBeforeDays      int                         `json:"renew_before_days"`
	LastError            string                      `json:"last_error"`
	Revision             uint64                      `json:"revision"`
	UsageCount           int64                       `json:"usage_count"`
	LatestOperation      *model.CertificateOperation `json:"latest_operation,omitempty"`
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
}

type issuedCertificateMetadata struct {
	SerialNumber      string
	FingerprintSHA256 string
	NotBefore         time.Time
	NotAfter          time.Time
}

func normalizeCertificateDomains(values []string) ([]string, map[string]string) {
	fields := make(map[string]string)
	if len(values) == 0 {
		fields["domains"] = "请至少填写一个证书域名。"
		return nil, fields
	}
	if len(values) > 10 {
		fields["domains"] = "单张证书最多支持 10 个域名。"
		return nil, fields
	}
	seen := make(map[string]struct{}, len(values))
	domains := make([]string, 0, len(values))
	for _, raw := range values {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if !validCertificateDomain(domain) {
			fields["domains"] = "域名必须是可公开验证的 ASCII/Punycode 主机名；HTTP-01 不支持通配符或 IP 地址。"
			return nil, fields
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	if len(domains) == 0 {
		fields["domains"] = "请至少填写一个证书域名。"
	}
	return domains, fields
}

func validCertificateDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.Contains(domain, "*") || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || !domainLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func validCertificateContactEmail(value string) bool {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254
}

func validNodeWebroot(value string) bool {
	cleaned := path.Clean(value)
	return strings.HasPrefix(cleaned, "/") && cleaned != "/" && cleaned == value
}

func decodeCertificateDomains(raw string) []string {
	var domains []string
	if err := json.Unmarshal([]byte(raw), &domains); err != nil {
		return []string{}
	}
	return domains
}

func protocolSupportsManagedCertificate(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "trojan", "hysteria2":
		return true
	default:
		return false
	}
}

func (h *handlers) loadUsableManagedCertificate(id, nodeID uint, protocol string, now time.Time) (model.ManagedCertificate, error) {
	if !protocolSupportsManagedCertificate(protocol) {
		return model.ManagedCertificate{}, errors.New("当前协议不使用 TLS 证书。")
	}
	var certificate model.ManagedCertificate
	if err := h.db.First(&certificate, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return certificate, errors.New("所选证书不存在。")
		}
		return certificate, err
	}
	if certificate.NodeID != nodeID {
		return certificate, errors.New("证书与协议服务必须属于同一节点。")
	}
	if certificate.NotAfter == nil || !certificate.NotAfter.After(now) ||
		(certificate.Status != certificateStatusActive && certificate.Status != certificateStatusFailed) {
		return certificate, errors.New("证书尚未成功签发或已经过期。")
	}
	return certificate, nil
}

func (h *handlers) loadManagedCertificateIDsForEndpoints(endpointIDs []uint) (map[uint]*uint, error) {
	result := make(map[uint]*uint, len(endpointIDs))
	if len(endpointIDs) == 0 {
		return result, nil
	}
	var links []model.CertificateProtocolEndpoint
	if err := h.db.Where("protocol_endpoint_id IN ?", endpointIDs).Find(&links).Error; err != nil {
		return nil, err
	}
	for _, link := range links {
		id := link.ManagedCertificateID
		result[link.ProtocolEndpointID] = &id
	}
	return result, nil
}

func (h *handlers) loadManagedCertificatesForEndpoints(endpoints []model.ProtocolEndpoint) (map[uint]model.ManagedCertificate, error) {
	result := make(map[uint]model.ManagedCertificate)
	if len(endpoints) == 0 {
		return result, nil
	}
	endpointIDs := make([]uint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpointIDs = append(endpointIDs, endpoint.ID)
	}
	type certificateEndpointRow struct {
		ProtocolEndpointID uint
		model.ManagedCertificate
	}
	var rows []certificateEndpointRow
	if err := h.db.Table("certificate_protocol_endpoints").
		Select("certificate_protocol_endpoints.protocol_endpoint_id, managed_certificates.*").
		Joins("JOIN managed_certificates ON managed_certificates.id = certificate_protocol_endpoints.managed_certificate_id").
		Where("certificate_protocol_endpoints.protocol_endpoint_id IN ?", endpointIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ProtocolEndpointID] = row.ManagedCertificate
	}
	return result, nil
}

func applyManagedCertificateToProtocol(protocol map[string]interface{}, protocolType string, certificate model.ManagedCertificate, now time.Time) error {
	if certificate.NotAfter == nil || !certificate.NotAfter.After(now) {
		return errors.New("certificate is expired")
	}
	if certificate.Status != certificateStatusActive && certificate.Status != certificateStatusFailed {
		return fmt.Errorf("certificate status is %s", certificate.Status)
	}
	if strings.TrimSpace(certificate.CertPath) == "" || strings.TrimSpace(certificate.KeyPath) == "" {
		return errors.New("certificate file paths are unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(protocolType)) {
	case "vless":
		tls, ok := protocol["tls"].(map[string]interface{})
		if !ok || tls == nil {
			return errors.New("VLESS endpoint is not configured for TLS")
		}
		tls["cert_path"] = certificate.CertPath
		tls["key_path"] = certificate.KeyPath
	case "vmess", "trojan":
		tls, _ := protocol["tls"].(map[string]interface{})
		if tls == nil {
			tls = make(map[string]interface{})
			protocol["tls"] = tls
		}
		tls["cert_path"] = certificate.CertPath
		tls["key_path"] = certificate.KeyPath
	case "hysteria2":
		protocol["cert_path"] = certificate.CertPath
		protocol["key_path"] = certificate.KeyPath
	default:
		return fmt.Errorf("%s does not support managed certificates", protocolType)
	}
	return nil
}

func effectiveCertificateStatus(certificate model.ManagedCertificate, now time.Time) string {
	if certificate.NotAfter != nil && !certificate.NotAfter.After(now) &&
		certificate.Status != certificateStatusIssuing && certificate.Status != certificateStatusRenewing {
		return certificateStatusExpired
	}
	return certificate.Status
}

func newCertificateListItem(certificate model.ManagedCertificate, nodeName string, usageCount int64, operation *model.CertificateOperation, now time.Time) certificateListItem {
	return certificateListItem{
		ID: certificate.ID, NodeID: certificate.NodeID, ProviderAccountID: certificate.ProviderAccountID, NodeName: nodeName, Name: certificate.Name,
		Domains: decodeCertificateDomains(certificate.Domains), ContactEmail: certificate.ContactEmail,
		Environment: certificate.Environment, ChallengeType: certificate.ChallengeType, WebrootPath: certificate.WebrootPath,
		Status: effectiveCertificateStatus(certificate, now), CertPath: certificate.CertPath, KeyPath: certificate.KeyPath,
		SerialNumber: certificate.SerialNumber, FingerprintSHA256: certificate.FingerprintSHA256,
		NotBefore: certificate.NotBefore, NotAfter: certificate.NotAfter, LastIssuedAt: certificate.LastIssuedAt,
		LastRenewalAttemptAt: certificate.LastRenewalAttemptAt, NextRenewalAt: certificate.NextRenewalAt,
		AutoRenew: certificate.AutoRenew, RenewBeforeDays: certificate.RenewBeforeDays,
		LastError: certificate.LastError, Revision: certificate.Revision, UsageCount: usageCount,
		LatestOperation: operation, CreatedAt: certificate.CreatedAt, UpdatedAt: certificate.UpdatedAt,
	}
}

func (h *handlers) ManagedCertificateListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.ManagedCertificate{})
	if nodeID := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeID != "" {
		parsed, parseErr := strconv.ParseUint(nodeID, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "node_id must be a positive integer")
			return
		}
		query = query.Where("node_id = ?", parsed)
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		like := "%" + search + "%"
		query = query.Where("name LIKE ? OR domains LIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	var certificates []model.ManagedCertificate
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&certificates).Error; err != nil {
		ServerError(w, err)
		return
	}
	items, err := h.decorateManagedCertificates(certificates, time.Now().UTC())
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *handlers) decorateManagedCertificates(certificates []model.ManagedCertificate, now time.Time) ([]certificateListItem, error) {
	if len(certificates) == 0 {
		return []certificateListItem{}, nil
	}
	ids := make([]uint, 0, len(certificates))
	nodeIDs := make([]uint, 0, len(certificates))
	for _, certificate := range certificates {
		ids = append(ids, certificate.ID)
		nodeIDs = append(nodeIDs, certificate.NodeID)
	}
	var nodes []model.Node
	if err := h.db.Select("id", "name").Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return nil, err
	}
	nodeNames := make(map[uint]string, len(nodes))
	for _, node := range nodes {
		nodeNames[node.ID] = node.Name
	}
	type usageRow struct {
		ManagedCertificateID uint
		Count                int64
	}
	var usageRows []usageRow
	if err := h.db.Model(&model.CertificateProtocolEndpoint{}).
		Select("managed_certificate_id, COUNT(*) AS count").
		Where("managed_certificate_id IN ?", ids).
		Group("managed_certificate_id").Scan(&usageRows).Error; err != nil {
		return nil, err
	}
	usageCounts := make(map[uint]int64, len(usageRows))
	for _, row := range usageRows {
		usageCounts[row.ManagedCertificateID] = row.Count
	}
	var operations []model.CertificateOperation
	if err := h.db.Where("managed_certificate_id IN ?", ids).
		Order("managed_certificate_id asc, id desc").Find(&operations).Error; err != nil {
		return nil, err
	}
	latestOperations := make(map[uint]*model.CertificateOperation, len(operations))
	for index := range operations {
		operation := &operations[index]
		if latestOperations[operation.ManagedCertificateID] == nil {
			latestOperations[operation.ManagedCertificateID] = operation
		}
	}
	items := make([]certificateListItem, 0, len(certificates))
	for _, certificate := range certificates {
		items = append(items, newCertificateListItem(certificate, nodeNames[certificate.NodeID], usageCounts[certificate.ID], latestOperations[certificate.ID], now))
	}
	return items, nil
}

func (h *handlers) ManagedCertificateGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var certificate model.ManagedCertificate
	if err := h.db.First(&certificate, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	items, err := h.decorateManagedCertificates([]model.ManagedCertificate{certificate}, time.Now().UTC())
	if err != nil {
		ServerError(w, err)
		return
	}
	var endpointIDs []uint
	if err := h.db.Model(&model.CertificateProtocolEndpoint{}).
		Where("managed_certificate_id = ?", id).Order("protocol_endpoint_id asc").
		Pluck("protocol_endpoint_id", &endpointIDs).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"certificate": items[0], "protocol_endpoint_ids": endpointIDs})
}

func (h *handlers) ManagedCertificateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var certificate model.ManagedCertificate
	if err := h.db.First(&certificate, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	var usageCount, runningCount int64
	if err := h.db.Model(&model.CertificateProtocolEndpoint{}).Where("managed_certificate_id = ?", id).Count(&usageCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.CertificateOperation{}).
		Where("managed_certificate_id = ? AND status = ?", id, "running").
		Count(&runningCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if usageCount > 0 || runningCount > 0 {
		writeJSON(w, http.StatusConflict, "删除证书前请先解除协议服务引用，并等待正在运行的签发或续期任务结束。",
			map[string]interface{}{"blockers": map[string]int64{"protocol_endpoints": usageCount, "running_operations": runningCount}})
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := createAuditLog(tx, claims, "certificate.delete", fmt.Sprintf("certificate:%d", certificate.ID),
			fmt.Sprintf("node=%d name=%s", certificate.NodeID, certificate.Name)); err != nil {
			return err
		}
		return tx.Delete(&certificate).Error
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": certificate.ID, "deleted": true, "remote_files_retained": true})
}

func (h *handlers) ManagedCertificateUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request certificateUpdateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.ContactEmail = strings.TrimSpace(request.ContactEmail)
	request.WebrootPath = strings.TrimSpace(request.WebrootPath)
	fields := map[string]string{}
	if request.ExpectedRevision == 0 {
		fields["expected_revision"] = "请刷新后再编辑该证书。"
	}
	if request.Name == "" || len([]byte(request.Name)) > 80 {
		fields["name"] = "证书名称需包含 1 到 80 个 UTF-8 字节。"
	}
	if !validCertificateContactEmail(request.ContactEmail) {
		fields["contact_email"] = "请输入有效的 ACME 联系邮箱。"
	}
	if request.RenewBeforeDays < 1 || request.RenewBeforeDays > 60 {
		fields["renew_before_days"] = "提前续期天数必须在 1–60 之间。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "证书信息校验失败。", fields)
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var certificate model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, id).Error; err != nil {
			return err
		}
		if certificate.Revision != request.ExpectedRevision {
			return errCertificateRevisionConflict
		}
		if certificate.Status == certificateStatusIssuing || certificate.Status == certificateStatusRenewing {
			return errCertificateOperationRunning
		}
		if certificate.ChallengeType == certificateChallengeHTTP01Webroot {
			if !validNodeWebroot(request.WebrootPath) {
				return validationError("证书信息校验失败。", map[string]string{"webroot_path": "Webroot 必须是节点上的规范绝对目录，且不能是根目录。"})
			}
		} else if request.WebrootPath != "" {
			return validationError("证书信息校验失败。", map[string]string{"webroot_path": "DNS-01 不使用 Webroot 路径。"})
		}
		updates := map[string]interface{}{
			"name": request.Name, "contact_email": request.ContactEmail, "webroot_path": request.WebrootPath,
			"auto_renew": request.AutoRenew, "renew_before_days": request.RenewBeforeDays,
			"revision": certificate.Revision + 1,
		}
		if certificate.NotAfter != nil {
			updates["next_renewal_at"] = certificate.NotAfter.Add(-time.Duration(request.RenewBeforeDays) * 24 * time.Hour)
		}
		if err := tx.Model(&certificate).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "certificate.update", fmt.Sprintf("certificate:%d", certificate.ID),
			fmt.Sprintf("name=%s auto_renew=%t renew_before_days=%d", request.Name, request.AutoRenew, request.RenewBeforeDays))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errCertificateRevisionConflict) {
		writeJSON(w, http.StatusConflict, "证书已被其他会话更新，请重新加载。", nil)
		return
	}
	if errors.Is(err, errCertificateOperationRunning) {
		writeJSON(w, http.StatusConflict, "证书正在签发或续期，请等待操作完成后再编辑。", nil)
		return
	}
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, validation)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "updated": true})
}

func (h *handlers) ManagedCertificateCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request certificateWriteRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.ContactEmail = strings.TrimSpace(request.ContactEmail)
	request.Environment = strings.ToLower(strings.TrimSpace(request.Environment))
	request.ChallengeType = strings.ToLower(strings.TrimSpace(request.ChallengeType))
	request.WebrootPath = strings.TrimSpace(request.WebrootPath)
	if request.Environment == "" {
		request.Environment = certificateEnvironmentProduction
	}
	if request.ChallengeType == "" {
		request.ChallengeType = certificateChallengeDNS01
	}
	if request.RenewBeforeDays == 0 {
		request.RenewBeforeDays = 30
	}
	domains, fields := normalizeCertificateDomains(request.Domains)
	if request.NodeID == 0 {
		fields["node_id"] = "请选择证书所在节点。"
	}
	if request.Name == "" || len([]byte(request.Name)) > 80 {
		fields["name"] = "证书名称需包含 1 到 80 个 UTF-8 字节。"
	}
	if !validCertificateContactEmail(request.ContactEmail) {
		fields["contact_email"] = "请输入有效的 ACME 联系邮箱。"
	}
	if request.Environment != certificateEnvironmentProduction && request.Environment != certificateEnvironmentStaging {
		fields["environment"] = "请选择生产或测试签发环境。"
	}
	if request.RenewBeforeDays < 1 || request.RenewBeforeDays > 60 {
		fields["renew_before_days"] = "提前续期天数必须在 1–60 之间。"
	}
	if request.ChallengeType != certificateChallengeDNS01 && request.ChallengeType != certificateChallengeHTTP01Webroot {
		fields["challenge_type"] = "请选择 Cloudflare DNS-01 或 HTTP-01 Webroot。"
	}
	if request.ChallengeType == certificateChallengeHTTP01Webroot {
		if !validNodeWebroot(request.WebrootPath) {
			fields["webroot_path"] = "Webroot 必须是节点上的规范绝对目录，且不能是根目录。"
		}
	} else if request.WebrootPath != "" {
		fields["webroot_path"] = "DNS-01 不使用 Webroot 路径。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "证书信息校验失败。", fields)
		return
	}
	var node model.Node
	if err := h.db.First(&node, request.NodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequestFields(w, "证书信息校验失败。", map[string]string{"node_id": "所选节点不存在。"})
			return
		}
		ServerError(w, err)
		return
	}
	var providerAccountID *uint
	if request.ChallengeType == certificateChallengeDNS01 {
		var account model.ProviderAccount
		if request.ProviderAccountID == 0 || h.db.First(&account, request.ProviderAccountID).Error != nil ||
			account.ProviderKey != providerCloudflare || account.Status != "active" {
			BadRequestFields(w, "证书信息校验失败。", map[string]string{"provider_account_id": "请选择已验证的 Cloudflare DNS 账户。"})
			return
		}
		providerAccountID = &account.ID
	} else if request.ProviderAccountID != 0 {
		BadRequestFields(w, "证书信息校验失败。", map[string]string{"provider_account_id": "HTTP-01 Webroot 不使用 DNS 供应商账户。"})
		return
	}
	domainsJSON, _ := json.Marshal(domains)
	autoRenew := true
	if request.AutoRenew != nil {
		autoRenew = *request.AutoRenew
	}
	certificate := model.ManagedCertificate{
		NodeID: request.NodeID, ProviderAccountID: providerAccountID, Name: request.Name, Domains: string(domainsJSON),
		ContactEmail: request.ContactEmail, Environment: request.Environment,
		ChallengeType: request.ChallengeType, WebrootPath: request.WebrootPath, Status: certificateStatusPending,
		AutoRenew: autoRenew, RenewBeforeDays: request.RenewBeforeDays, Revision: 1,
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&certificate).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "certificate.create", fmt.Sprintf("certificate:%d", certificate.ID),
			fmt.Sprintf("node=%d environment=%s challenge=%s domains=%d", certificate.NodeID, certificate.Environment, certificate.ChallengeType, len(domains)))
	}); err != nil {
		ServerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, "certificate created", newCertificateListItem(certificate, node.Name, 0, nil, time.Now().UTC()))
}

func (h *handlers) ManagedCertificateRenewalUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request certificateRenewalUpdateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if request.ExpectedRevision == 0 {
		writeJSON(w, http.StatusPreconditionRequired, "更新证书续期策略前需要提供当前版本号。", nil)
		return
	}
	if request.RenewBeforeDays < 1 || request.RenewBeforeDays > 60 {
		BadRequestFields(w, "证书续期策略校验失败。", map[string]string{"renew_before_days": "提前续期天数必须在 1–60 之间。"})
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var certificate model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, id).Error; err != nil {
			return err
		}
		if certificate.Revision != request.ExpectedRevision {
			return errCertificateRevisionConflict
		}
		updates := map[string]interface{}{
			"auto_renew": request.AutoRenew, "renew_before_days": request.RenewBeforeDays,
			"revision": certificate.Revision + 1,
		}
		if certificate.NotAfter != nil {
			next := certificate.NotAfter.Add(-time.Duration(request.RenewBeforeDays) * 24 * time.Hour)
			updates["next_renewal_at"] = next
		}
		if err := tx.Model(&certificate).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "certificate.renewal_policy.update", fmt.Sprintf("certificate:%d", certificate.ID),
			fmt.Sprintf("auto_renew=%t renew_before_days=%d", request.AutoRenew, request.RenewBeforeDays))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errCertificateRevisionConflict) {
		writeJSON(w, http.StatusConflict, "证书续期策略已被其他会话更新，请重新加载。", nil)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "updated": true})
}

func (h *handlers) ManagedCertificateIssueHandler(w http.ResponseWriter, r *http.Request) {
	h.startManagedCertificateOperationHandler(w, r, certificateOperationIssue)
}

func (h *handlers) ManagedCertificateRenewHandler(w http.ResponseWriter, r *http.Request) {
	h.startManagedCertificateOperationHandler(w, r, certificateOperationRenew)
}

func (h *handlers) startManagedCertificateOperationHandler(w http.ResponseWriter, r *http.Request, operationType string) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	requestedBy := claims.UserID
	operation, err := h.startManagedCertificateOperation(id, operationType, &requestedBy)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errCertificateOperationRunning) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, "certificate operation started", operation)
}

func (h *handlers) startManagedCertificateOperation(certificateID uint, operationType string, requestedBy *uint) (model.CertificateOperation, error) {
	var operation model.CertificateOperation
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var certificate model.ManagedCertificate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, certificateID).Error; err != nil {
			return err
		}
		if certificate.Status == certificateStatusIssuing || certificate.Status == certificateStatusRenewing {
			return errCertificateOperationRunning
		}
		if operationType == certificateOperationRenew && certificate.NotAfter == nil {
			return errors.New("certificate has not been issued yet")
		}
		now := time.Now().UTC()
		status := certificateStatusIssuing
		if operationType == certificateOperationRenew {
			status = certificateStatusRenewing
		}
		operation = model.CertificateOperation{
			ManagedCertificateID: certificate.ID, NodeID: certificate.NodeID,
			OperationType: operationType, Status: "running", Phase: "queued",
			RequestedBy: requestedBy, StartedAt: &now,
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{"status": status, "last_error": ""}
		if operationType == certificateOperationRenew {
			updates["last_renewal_attempt_at"] = now
		}
		return tx.Model(&certificate).Updates(updates).Error
	})
	if err != nil {
		return operation, err
	}
	go h.executeManagedCertificateOperation(operation.ID)
	return operation, nil
}

func (h *handlers) executeManagedCertificateOperation(operationID uint) {
	var operation model.CertificateOperation
	if err := h.db.First(&operation, operationID).Error; err != nil {
		return
	}
	var certificate model.ManagedCertificate
	if err := h.db.First(&certificate, operation.ManagedCertificateID).Error; err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "loading", err, false)
		return
	}
	var node model.Node
	if err := h.db.First(&node, certificate.NodeID).Error; err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "loading", err, false)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "connecting", err, false)
		return
	}
	domains := decodeCertificateDomains(certificate.Domains)
	secretInput := ""
	switch certificate.ChallengeType {
	case certificateChallengeDNS01:
		if certificate.ProviderAccountID == nil {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 certificate has no provider account"), false)
			return
		}
		var account model.ProviderAccount
		if err := h.db.First(&account, *certificate.ProviderAccountID).Error; err != nil ||
			account.ProviderKey != providerCloudflare || account.Status != "active" {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 provider account is unavailable"), false)
			return
		}
		token, err := h.credentialCipher.Decrypt(account.CredentialCiphertext)
		if err != nil {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 provider credential is unavailable"), false)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		for _, domain := range domains {
			if _, err := findCloudflareZone(ctx, token, domain); err != nil {
				cancel()
				h.finishCertificateOperationFailure(operation, certificate, "preflight", fmt.Errorf("DNS-01 cannot manage %s: %w", domain, err), false)
				return
			}
		}
		cancel()
		secretInput = token + "\n"
	case certificateChallengeHTTP01Webroot:
		if err := preflightHTTP01Domains(domains); err != nil {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", err, false)
			return
		}
	case certificateChallengeHTTP01:
		// Existing certificates retain their legacy standalone renewal contract.
	default:
		h.finishCertificateOperationFailure(operation, certificate, "preflight", fmt.Errorf("unsupported certificate challenge %q", certificate.ChallengeType), false)
		return
	}
	_ = h.db.Model(&operation).Update("phase", "requesting").Error
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "connecting", err, false)
		return
	}
	defer conn.Close()
	timeout := time.AfterFunc(certificateOperationTimeout, func() { _ = conn.Close() })
	defer timeout.Stop()
	script := buildCertbotCertificateScript(certificate, domains, operation.OperationType == certificateOperationRenew, uuid.NewString())
	output, err := h.runNodeSSHSessionWithInput(conn, node, script, true, secretInput)
	if err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "requesting",
			fmt.Errorf("ACME certificate request failed: %w: %s", err, truncateCertificateError(output)), false)
		return
	}
	metadata, err := parseIssuedCertificateMetadata(output, domains, time.Now().UTC())
	if err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "validating", err, false)
		return
	}
	now := time.Now().UTC()
	nextRenewal := metadata.NotAfter.Add(-time.Duration(certificate.RenewBeforeDays) * 24 * time.Hour)
	if nextRenewal.Before(now.Add(24 * time.Hour)) {
		nextRenewal = now.Add(24 * time.Hour)
	}
	certPath, keyPath := managedCertificatePaths(certificate.ID)
	if err := h.db.Model(&certificate).Updates(map[string]interface{}{
		"status": certificateStatusActive, "cert_path": certPath, "key_path": keyPath,
		"serial_number": metadata.SerialNumber, "fingerprint_sha256": metadata.FingerprintSHA256,
		"not_before": metadata.NotBefore, "not_after": metadata.NotAfter,
		"last_issued_at": now, "next_renewal_at": nextRenewal, "last_error": "",
	}).Error; err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "persisting", err, false)
		return
	}

	_ = h.db.Model(&operation).Update("phase", "publishing").Error
	var binding model.CertificateProtocolEndpoint
	bindingErr := h.db.Where("managed_certificate_id = ?", certificate.ID).Order("id asc").First(&binding).Error
	if bindingErr != nil && !errors.Is(bindingErr, gorm.ErrRecordNotFound) {
		h.finishCertificateOperationFailure(operation, certificate, "publishing", bindingErr, true)
		return
	}
	if bindingErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
		_, _, publishErr := h.publishNodeConfigForNode(ctx, certificate.NodeID, binding.ProtocolEndpointID, requestedByValue(operation.RequestedBy))
		cancel()
		if publishErr != nil {
			h.finishCertificateOperationFailure(operation, certificate, "publishing",
				fmt.Errorf("certificate is active but Zero configuration publish failed: %w", publishErr), true)
			return
		}
	}
	finished := time.Now().UTC()
	summary := fmt.Sprintf("%s certificate valid until %s", operation.OperationType, metadata.NotAfter.Format(time.RFC3339))
	_ = h.db.Model(&operation).Updates(map[string]interface{}{
		"status": "succeeded", "phase": "completed", "result_summary": summary,
		"error": "", "finished_at": finished,
	}).Error
}

func requestedByValue(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}

func (h *handlers) finishCertificateOperationFailure(operation model.CertificateOperation, certificate model.ManagedCertificate, phase string, cause error, certificateUsable bool) {
	now := time.Now().UTC()
	message := truncateCertificateError(cause.Error())
	status := certificateStatusFailed
	if certificateUsable {
		status = certificateStatusActive
	} else if certificate.NotAfter != nil && !certificate.NotAfter.After(now) {
		status = certificateStatusExpired
	}
	nextRetry := now.Add(certificateRetryInterval)
	_ = h.db.Transaction(func(tx *gorm.DB) error {
		if certificate.ID != 0 {
			if err := tx.Model(&model.ManagedCertificate{}).Where("id = ?", certificate.ID).Updates(map[string]interface{}{
				"status": status, "last_error": message, "next_renewal_at": nextRetry,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.CertificateOperation{}).Where("id = ?", operation.ID).Updates(map[string]interface{}{
			"status": "failed", "phase": phase, "error": message, "finished_at": now,
		}).Error
	})
}

func managedCertificatePaths(certificateID uint) (string, string) {
	base := fmt.Sprintf("/etc/zboard/certificates/%d/current", certificateID)
	return base + "/fullchain.pem", base + "/privkey.pem"
}

var (
	http01LookupIPAddrs = func(ctx context.Context, resolver *net.Resolver, domain string) ([]net.IPAddr, error) {
		return resolver.LookupIPAddr(ctx, domain)
	}
	http01DialTimeout = net.DialTimeout
)

func preflightHTTP01Domains(domains []string) error {
	for _, domain := range domains {
		var addresses []net.IPAddr
		var lookupErr error
		for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
			resolver := &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					dialer := net.Dialer{Timeout: 2 * time.Second}
					return dialer.DialContext(ctx, network, server)
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			addresses, lookupErr = http01LookupIPAddrs(ctx, resolver, domain)
			cancel()
			if lookupErr == nil && len(addresses) > 0 {
				break
			}
		}
		if lookupErr != nil || len(addresses) == 0 {
			return fmt.Errorf("HTTP-01 preflight could not resolve public A/AAAA records for %s", domain)
		}
		seen := make(map[string]struct{}, len(addresses))
		unobservable := 0
		for _, address := range addresses {
			ip := address.IP.String()
			if _, exists := seen[ip]; exists {
				continue
			}
			seen[ip] = struct{}{}
			network := "IPv6 AAAA"
			if address.IP.To4() != nil {
				network = "IPv4 A"
			}
			conn, err := http01DialTimeout("tcp", net.JoinHostPort(ip, "80"), 3*time.Second)
			if err != nil {
				if localNetworkUnreachable(err) {
					unobservable++
					continue
				}
				return fmt.Errorf("HTTP-01 preflight: %s %s for %s cannot accept TCP port 80: %w", network, ip, domain, err)
			}
			_ = conn.Close()
		}
		_ = unobservable // An absent local route is a control-plane limitation, not proof that the node is unreachable.
	}
	return nil
}

func localNetworkUnreachable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "network is unreachable") || strings.Contains(message, "no route to host") ||
		strings.Contains(message, "network unreachable")
}

func buildCertbotCertificateScript(certificate model.ManagedCertificate, domains []string, forceRenewal bool, stagingID string) string {
	certName := fmt.Sprintf("zboard-%d", certificate.ID)
	baseDir := fmt.Sprintf("/etc/zboard/certificates/%d", certificate.ID)
	stageDir := baseDir + "/.staging-" + stagingID
	args := []string{
		"certonly", "--non-interactive", "--agree-tos",
		"--email", certificate.ContactEmail, "--cert-name", certName,
		"--rsa-key-size", "2048",
	}
	switch certificate.ChallengeType {
	case certificateChallengeDNS01:
		args = append(args,
			"--dns-cloudflare",
			"--dns-cloudflare-credentials", stageDir+"/cloudflare.ini",
			"--dns-cloudflare-propagation-seconds", "30",
		)
	case certificateChallengeHTTP01Webroot:
		args = append(args, "--webroot", "--webroot-path", certificate.WebrootPath, "--preferred-challenges", "http")
	default:
		args = append(args, "--standalone", "--preferred-challenges", "http", "--http-01-port", "80")
	}
	if certificate.Environment == certificateEnvironmentStaging {
		args = append(args, "--server", "https://acme-staging-v02.api.letsencrypt.org/directory")
	}
	if forceRenewal {
		args = append(args, "--force-renewal")
	}
	for _, domain := range domains {
		args = append(args, "-d", domain)
	}
	quotedArgs := make([]string, 0, len(args))
	for _, argument := range args {
		quotedArgs = append(quotedArgs, shellQuote(argument))
	}
	quotedDomains := make([]string, 0, len(domains))
	for _, domain := range domains {
		quotedDomains = append(quotedDomains, shellQuote(domain))
	}
	http01PreflightToken := "zboard-http01-preflight-" + stagingID
	return fmt.Sprintf(`set -eu
test "$(id -u)" = "0"
install_certbot() {
  if command -v certbot >/dev/null 2>&1; then certbot_bin="$(command -v certbot)"; return 0; fi
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y certbot
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y certbot
  elif command -v yum >/dev/null 2>&1; then
    yum install -y certbot
  elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache certbot
  else
    printf 'ZBOARD_CERT_ERROR=no supported package manager can install certbot\n' >&2
    return 1
  fi
  command -v certbot >/dev/null 2>&1
  certbot_bin="$(command -v certbot)"
}
install_certbot
command -v openssl >/dev/null 2>&1
stage_dir=%s
install -d -m 0700 "$stage_dir"
http01_challenge_file=""
cleanup_challenge() {
  rm -f "$stage_dir/cloudflare.ini"
  if [ -n "$http01_challenge_file" ]; then rm -f "$http01_challenge_file"; fi
}
trap cleanup_challenge EXIT HUP INT TERM
if [ %s = http-01-webroot ]; then
  webroot=%s
  challenge_token=%s
  challenge_dir="$webroot/.well-known/acme-challenge"
  http01_challenge_file="$challenge_dir/$challenge_token"
  install -d -m 0755 "$challenge_dir"
  printf '%%s' "$challenge_token" > "$http01_challenge_file"
  chmod 0644 "$http01_challenge_file"
  fetch_http01_challenge() {
    if command -v curl >/dev/null 2>&1; then
      curl --fail --silent --show-error --insecure --location --proto '=http,https' --proto-redir '=http,https' --connect-timeout 5 --max-time 20 "$1"
    elif command -v wget >/dev/null 2>&1; then
      wget -qO- --no-check-certificate --timeout=20 "$1"
    else
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight requires curl or wget on the node\n' >&2
      return 1
    fi
  }
  for domain in %s; do
    challenge_url="http://$domain/.well-known/acme-challenge/$challenge_token"
    if ! challenge_body="$(fetch_http01_challenge "$challenge_url")"; then
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight could not fetch %%s; verify that %%s serves %%s\n' "$challenge_url" "$domain" "$webroot" >&2
      exit 1
    fi
    if [ "$challenge_body" != "$challenge_token" ]; then
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight returned unexpected content for %%s; verify that %%s maps to %%s\n' "$domain" "$challenge_url" "$webroot" >&2
      exit 1
    fi
  done
  rm -f "$http01_challenge_file"
  http01_challenge_file=""
fi
if [ %s = dns-01 ]; then
  IFS= read -r cloudflare_token
  test -n "$cloudflare_token"
  plugin_installed=0
  python_ready=0
  if "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    plugin_installed=1
  fi
  if [ "$plugin_installed" = 0 ]; then
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get update >/dev/null 2>&1 || true
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache certbot-dns-cloudflare; then plugin_installed=1; fi
    fi
  fi
  if [ "$plugin_installed" = 1 ] && ! "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    plugin_installed=0
  fi
  if [ "$plugin_installed" = 0 ]; then
    if command -v python3 >/dev/null 2>&1; then
      python_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3; then python_ready=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3; then python_ready=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3; then python_ready=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache python3; then python_ready=1; fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] && [ "$python_ready" = 1 ]; then
    certbot_venv=/opt/zboard-certbot
    venv_ready=0
    if python3 -m venv "$certbot_venv" >/dev/null 2>&1; then
      venv_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-venv && python3 -m venv "$certbot_venv"; then
        venv_ready=1
      elif python_version="$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')" &&
        DEBIAN_FRONTEND=noninteractive apt-get install -y "python${python_version}-venv" &&
        python3 -m venv "$certbot_venv"; then
        venv_ready=1
      fi
    fi
    if [ "$venv_ready" = 1 ]; then
      if "$certbot_venv/bin/pip" install --disable-pip-version-check --upgrade certbot certbot-dns-cloudflare; then
        certbot_bin="$certbot_venv/bin/certbot"
        plugin_installed=1
      fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] && [ "$python_ready" = 1 ]; then
    pip_ready=0
    if python3 -m pip --version >/dev/null 2>&1; then
      pip_ready=1
    elif python3 -m ensurepip --upgrade >/dev/null 2>&1 && python3 -m pip --version >/dev/null 2>&1; then
      pip_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache py3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    fi
    if [ "$pip_ready" = 1 ]; then
      certbot_target=/opt/zboard-certbot-packages
      certbot_wrapper=/opt/zboard-certbot-run
      install -d -m 0755 "$certbot_target"
      if python3 -m pip install --disable-pip-version-check --upgrade --target "$certbot_target" certbot certbot-dns-cloudflare; then
        cat > "$certbot_wrapper" <<'ZBOARD_CERTBOT_WRAPPER'
#!/bin/sh
PYTHONPATH=/opt/zboard-certbot-packages exec python3 -m certbot "$@"
ZBOARD_CERTBOT_WRAPPER
        chmod 0755 "$certbot_wrapper"
        certbot_bin="$certbot_wrapper"
        plugin_installed=1
      fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] || ! "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    printf 'ZBOARD_CERT_ERROR=unable to install Certbot Cloudflare DNS plugin; system package, Python venv, and pip target fallbacks failed\n' >&2
    exit 1
  fi
  printf 'dns_cloudflare_api_token = %%s\n' "$cloudflare_token" > "$stage_dir/cloudflare.ini"
  chmod 0600 "$stage_dir/cloudflare.ini"
fi
"$certbot_bin" %s
source_dir=%s
base_dir=%s
test -s "$source_dir/fullchain.pem"
test -s "$source_dir/privkey.pem"
rm -f "$stage_dir/cloudflare.ini"
install -d -m 0700 "$stage_dir" "$base_dir/generations"
install -m 0644 "$source_dir/fullchain.pem" "$stage_dir/fullchain.pem"
install -m 0600 "$source_dir/privkey.pem" "$stage_dir/privkey.pem"
openssl x509 -in "$stage_dir/fullchain.pem" -pubkey -noout > "$stage_dir/cert.pub"
openssl pkey -in "$stage_dir/privkey.pem" -pubout > "$stage_dir/key.pub"
cmp "$stage_dir/cert.pub" "$stage_dir/key.pub"
rm -f "$stage_dir/cert.pub" "$stage_dir/key.pub"
serial="$(openssl x509 -in "$stage_dir/fullchain.pem" -serial -noout | cut -d= -f2 | tr -cd 'A-Fa-f0-9')"
test -n "$serial"
generation="$base_dir/generations/$serial"
install -d -m 0700 "$generation"
install -m 0644 "$stage_dir/fullchain.pem" "$generation/fullchain.pem"
install -m 0600 "$stage_dir/privkey.pem" "$generation/privkey.pem"
ln -sfn "$generation" "$base_dir/current.next"
mv -Tf "$base_dir/current.next" "$base_dir/current"
rm -rf "$stage_dir"
printf 'ZBOARD_CERT_DER_BASE64=%%s\n' "$(openssl x509 -in "$base_dir/current/fullchain.pem" -outform DER | base64 | tr -d '\r\n')"
`, shellQuote(stageDir), shellQuote(certificate.ChallengeType), shellQuote(certificate.WebrootPath), shellQuote(http01PreflightToken),
		strings.Join(quotedDomains, " "), shellQuote(certificate.ChallengeType), strings.Join(quotedArgs, " "),
		shellQuote("/etc/letsencrypt/live/"+certName), shellQuote(baseDir))
}

func parseIssuedCertificateMetadata(output string, domains []string, now time.Time) (issuedCertificateMetadata, error) {
	const marker = "ZBOARD_CERT_DER_BASE64="
	index := strings.LastIndex(output, marker)
	if index < 0 {
		return issuedCertificateMetadata{}, errors.New("remote certificate result did not include validated certificate metadata")
	}
	encoded := strings.TrimSpace(strings.SplitN(output[index+len(marker):], "\n", 2)[0])
	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return issuedCertificateMetadata{}, fmt.Errorf("decode issued certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return issuedCertificateMetadata{}, fmt.Errorf("parse issued certificate: %w", err)
	}
	for _, domain := range domains {
		if err := certificate.VerifyHostname(domain); err != nil {
			return issuedCertificateMetadata{}, fmt.Errorf("issued certificate does not cover %s: %w", domain, err)
		}
	}
	if certificate.NotAfter.Before(now.Add(24 * time.Hour)) {
		return issuedCertificateMetadata{}, errors.New("issued certificate expires too soon")
	}
	sum := sha256.Sum256(certificate.Raw)
	return issuedCertificateMetadata{
		SerialNumber:      strings.ToUpper(certificate.SerialNumber.Text(16)),
		FingerprintSHA256: fmt.Sprintf("%x", sum[:]),
		NotBefore:         certificate.NotBefore.UTC(), NotAfter: certificate.NotAfter.UTC(),
	}, nil
}

func truncateCertificateError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[:4000]
	}
	return value
}

func (h *handlers) StartCertificateRenewalWorker() {
	go func() {
		h.scanCertificateRenewals(time.Now().UTC())
		ticker := time.NewTicker(certificateRenewalScanInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			h.scanCertificateRenewals(now.UTC())
		}
	}()
}

func (h *handlers) scanCertificateRenewals(now time.Time) {
	if h.backgroundWorkPaused() {
		return
	}
	_ = h.db.Model(&model.ManagedCertificate{}).
		Where("not_after IS NOT NULL AND not_after <= ? AND status NOT IN ?", now, []string{certificateStatusIssuing, certificateStatusRenewing, certificateStatusExpired}).
		Updates(map[string]interface{}{"status": certificateStatusExpired, "last_error": "certificate has expired"}).Error
	var certificates []model.ManagedCertificate
	if err := h.db.Where(
		"auto_renew = ? AND not_after IS NOT NULL AND not_after > ? AND next_renewal_at IS NOT NULL AND next_renewal_at <= ? AND status IN ?",
		true, now, now, []string{certificateStatusActive, certificateStatusFailed},
	).Order("next_renewal_at asc, id asc").Limit(20).Find(&certificates).Error; err != nil {
		return
	}
	for _, certificate := range certificates {
		_, _ = h.startManagedCertificateOperation(certificate.ID, certificateOperationRenew, nil)
	}
}
