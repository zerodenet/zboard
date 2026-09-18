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
	capabilitynetwork "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
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
	errCertificateOperationRunning = capabilitynetwork.ErrCertificateOperationActive
	errCertificateRevisionConflict = capabilitynetwork.ErrCertificateConflict
	domainLabelPattern             = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

func managedCertificateModel(row capabilitynetwork.CertificateRecord) model.ManagedCertificate {
	return model.ManagedCertificate{
		ID: row.ID, NodeID: row.NodeID, ProviderAccountID: row.ProviderAccountID, Name: row.Name,
		Domains: row.Domains, ContactEmail: row.ContactEmail, Environment: row.Environment,
		ChallengeType: row.ChallengeType, WebrootPath: row.WebrootPath, Status: row.Status,
		CertPath: row.CertPath, KeyPath: row.KeyPath, SerialNumber: row.SerialNumber,
		FingerprintSHA256: row.FingerprintSHA256, NotBefore: row.NotBefore, NotAfter: row.NotAfter,
		LastIssuedAt: row.LastIssuedAt, LastRenewalAttemptAt: row.LastRenewalAttemptAt,
		NextRenewalAt: row.NextRenewalAt, AutoRenew: row.AutoRenew, RenewBeforeDays: row.RenewBeforeDays,
		LastError: row.LastError, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func certificateOperationModel(row capabilitynetwork.CertificateOperationRecord) model.CertificateOperation {
	return model.CertificateOperation{
		ID: row.ID, ManagedCertificateID: row.ManagedCertificateID, NodeID: row.NodeID,
		OperationType: row.OperationType, Status: row.Status, Phase: row.Phase,
		RequestedBy: row.RequestedBy, ResultSummary: row.ResultSummary, Error: row.Error,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

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

func (h *handlers) loadManagedCertificateIDsForEndpoints(ctx context.Context, endpointIDs []uint) (map[uint]*uint, error) {
	bindings, err := h.services.CertificateInventory.Bindings(ctx, endpointIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uint]*uint, len(bindings))
	for endpointID, certificateID := range bindings {
		id := certificateID
		result[endpointID] = &id
	}
	return result, nil
}

func effectiveCertificateStatus(certificate model.ManagedCertificate, now time.Time) string {
	if certificate.NotAfter != nil && !certificate.NotAfter.After(now) &&
		certificate.Status != resourceStatusDeleting && certificate.Status != certificateStatusIssuing && certificate.Status != certificateStatusRenewing {
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
	input := capabilitynetwork.CertificateInventoryQuery{Offset: offset, Limit: limit}
	if nodeID := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeID != "" {
		parsed, parseErr := strconv.ParseUint(nodeID, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "node_id must be a positive integer")
			return
		}
		input.NodeID = uint(parsed)
	}
	input.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	input.Search = strings.TrimSpace(r.URL.Query().Get("q"))
	page, err := h.services.CertificateInventory.List(r.Context(), input)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]certificateListItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, certificateInventoryListItem(item, time.Now().UTC()))
	}
	OK(w, map[string]interface{}{"items": items, "total": page.Total, "offset": offset, "limit": limit})
}

func certificateInventoryListItem(item capabilitynetwork.CertificateInventoryItem, now time.Time) certificateListItem {
	var operation *model.CertificateOperation
	if item.LatestOperation != nil {
		value := certificateOperationModel(*item.LatestOperation)
		operation = &value
	}
	return newCertificateListItem(managedCertificateModel(item.Certificate), item.NodeName, item.UsageCount, operation, now)
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
	detail, err := h.services.CertificateInventory.Detail(r.Context(), id)
	if err != nil {
		if errors.Is(err, capabilitynetwork.ErrCertificateNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"certificate": certificateInventoryListItem(detail.Certificate, time.Now().UTC()), "protocol_endpoint_ids": detail.ProtocolEndpointIDs})
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
	detail, err := h.services.CertificateInventory.Detail(r.Context(), id)
	if err != nil {
		if errors.Is(err, capabilitynetwork.ErrCertificateNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if detail.Certificate.Certificate.ChallengeType == certificateChallengeHTTP01Webroot {
		if !validNodeWebroot(request.WebrootPath) {
			BadRequestFields(w, "证书信息校验失败。", map[string]string{"webroot_path": "Webroot 必须是节点上的规范绝对目录，且不能是根目录。"})
			return
		}
	} else if request.WebrootPath != "" {
		BadRequestFields(w, "证书信息校验失败。", map[string]string{"webroot_path": "DNS-01 不使用 Webroot 路径。"})
		return
	}
	err = h.services.CertificateLifecycle.Update(r.Context(), claims.UserID, id, capabilitynetwork.CertificateUpdate{
		Name: request.Name, ContactEmail: request.ContactEmail, WebrootPath: request.WebrootPath,
		AutoRenew: request.AutoRenew, RenewBeforeDays: request.RenewBeforeDays, ExpectedRevision: request.ExpectedRevision,
	})
	if errors.Is(err, capabilitynetwork.ErrCertificateNotFound) {
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
	if errors.Is(err, capabilitynetwork.ErrCertificateDeleting) {
		writeJSON(w, http.StatusConflict, errResourceDeleting.Error(), nil)
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificatePermission) {
		Forbidden(w, "管理员权限已失效。")
		return
	}
	if err != nil {
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
	var providerAccountID *uint
	if request.ChallengeType == certificateChallengeDNS01 {
		if request.ProviderAccountID == 0 {
			BadRequestFields(w, "证书信息校验失败。", map[string]string{"provider_account_id": "请选择已验证且支持证书签发的供应商账户。"})
			return
		}
		providerAccountID = &request.ProviderAccountID
	} else if request.ProviderAccountID != 0 {
		BadRequestFields(w, "证书信息校验失败。", map[string]string{"provider_account_id": "HTTP-01 Webroot 不使用 DNS 供应商账户。"})
		return
	}
	domainsJSON, _ := json.Marshal(domains)
	autoRenew := true
	if request.AutoRenew != nil {
		autoRenew = *request.AutoRenew
	}
	certificate := capabilitynetwork.CertificateRecord{
		NodeID: request.NodeID, ProviderAccountID: providerAccountID, Name: request.Name, Domains: string(domainsJSON),
		ContactEmail: request.ContactEmail, Environment: request.Environment,
		ChallengeType: request.ChallengeType, WebrootPath: request.WebrootPath, Status: certificateStatusPending,
		AutoRenew: autoRenew, RenewBeforeDays: request.RenewBeforeDays, Revision: 1,
	}
	created, nodeName, err := h.services.CertificateLifecycle.Create(r.Context(), claims.UserID, certificate)
	if errors.Is(err, capabilitynetwork.ErrCertificateDependency) {
		BadRequest(w, "证书依赖的节点或供应商账户不可用。")
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificateDeleting) {
		writeJSON(w, http.StatusConflict, errResourceDeleting.Error(), nil)
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificatePermission) {
		Forbidden(w, "管理员权限已失效。")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, "certificate created", newCertificateListItem(managedCertificateModel(created), nodeName, 0, nil, time.Now().UTC()))
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
	err = h.services.CertificateLifecycle.UpdateRenewal(r.Context(), claims.UserID, id, request.AutoRenew, request.RenewBeforeDays, request.ExpectedRevision)
	if errors.Is(err, capabilitynetwork.ErrCertificateNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errCertificateRevisionConflict) {
		writeJSON(w, http.StatusConflict, "证书续期策略已被其他会话更新，请重新加载。", nil)
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificateDeleting) {
		writeJSON(w, http.StatusConflict, errResourceDeleting.Error(), nil)
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificatePermission) {
		Forbidden(w, "管理员权限已失效。")
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
	if errors.Is(err, capabilitynetwork.ErrCertificateNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errCertificateOperationRunning) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if errors.Is(err, capabilitynetwork.ErrCertificatePermission) {
		Forbidden(w, "管理员权限已失效。")
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, "certificate operation started", operation)
}

func (h *handlers) startManagedCertificateOperation(certificateID uint, operationType string, requestedBy *uint) (model.CertificateOperation, error) {
	h.backgroundJobs()
	actor := uint(0)
	if requestedBy != nil {
		actor = *requestedBy
	}
	record, err := h.services.CertificateLifecycle.Start(context.Background(), actor, certificateID, operationType)
	operation := certificateOperationModel(record)
	if err != nil {
		return operation, err
	}
	return operation, nil
}

func (h *handlers) executeManagedCertificateOperation(operationID uint) {
	h.executeManagedCertificateOperationContext(context.Background(), operationID)
}
func (h *handlers) executeManagedCertificateOperationContext(parent context.Context, operationID uint) {
	prepared, err := h.services.CertificateLifecycle.Prepare(parent, operationID)
	if err != nil {
		if prepared.Operation.ID != 0 {
			h.finishCertificateOperationFailure(certificateOperationModel(prepared.Operation), managedCertificateModel(prepared.Certificate), "loading", err, false)
		}
		return
	}
	operation := certificateOperationModel(prepared.Operation)
	certificate := managedCertificateModel(prepared.Certificate)
	node := certificateExecutionNodeModel(prepared.Node)
	if err := h.validateNodeSSH(node); err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "connecting", err, false)
		return
	}
	domains := decodeCertificateDomains(certificate.Domains)
	secretInput := ""
	pluginProviderKey := ""
	pluginProviderCredential := ""
	switch certificate.ChallengeType {
	case certificateChallengeDNS01:
		if certificate.ProviderAccountID == nil {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 certificate has no provider account"), false)
			return
		}
		if prepared.Provider == nil || prepared.Provider.Status != "active" {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 provider account is unavailable"), false)
			return
		}
		token, err := h.credentialCipher.Decrypt(prepared.Provider.CredentialCiphertext)
		if err != nil {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 provider credential is unavailable"), false)
			return
		}
		if prepared.Provider.ProviderKey == providerCloudflare && providerHasCapability(prepared.Provider.Capabilities, "dns.records") {
			ctx, cancel := context.WithTimeout(parent, 20*time.Second)
			for _, domain := range domains {
				if _, err := findCloudflareZone(ctx, token, domain); err != nil {
					cancel()
					h.finishCertificateOperationFailure(operation, certificate, "preflight", fmt.Errorf("DNS-01 cannot manage %s: %w", domain, err), false)
					return
				}
			}
			cancel()
			secretInput = token + "\n"
		} else if providerHasCapability(prepared.Provider.Capabilities, "certificate.issue") {
			pluginProviderKey = prepared.Provider.ProviderKey
			pluginProviderCredential = token
		} else {
			h.finishCertificateOperationFailure(operation, certificate, "preflight", errors.New("DNS-01 provider does not support certificate issuance"), false)
			return
		}
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
	_ = h.services.CertificateLifecycle.Phase(parent, operation.ID, "requesting")
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "connecting", err, false)
		return
	}
	remote := h.newSSHRemoteSession(conn, node)
	defer remote.Close()
	stopCancellation := context.AfterFunc(parent, func() { _ = remote.Close() })
	defer stopCancellation()
	timeout := time.AfterFunc(certificateOperationTimeout, func() { _ = remote.Close() })
	defer timeout.Stop()
	var metadata issuedCertificateMetadata
	failurePhase := "requesting"
	if pluginProviderKey != "" {
		metadata, failurePhase, err = h.issueCertificateWithProvider(parent, remote, certificate, operation, pluginProviderKey, pluginProviderCredential, domains)
	} else {
		script := buildCertbotCertificateScript(certificate, domains, operation.OperationType == certificateOperationRenew, uuid.NewString())
		var output string
		output, err = remote.RunWithInput(script, true, secretInput)
		if err == nil {
			failurePhase = "validating"
			metadata, err = parseIssuedCertificateMetadata(output, domains, time.Now().UTC())
		} else {
			err = fmt.Errorf("ACME certificate request failed: %w: %s", err, truncateCertificateError(output))
		}
	}
	if err != nil {
		h.finishCertificateOperationFailure(operation, certificate, failurePhase, err, false)
		return
	}
	now := time.Now().UTC()
	nextRenewal := metadata.NotAfter.Add(-time.Duration(certificate.RenewBeforeDays) * 24 * time.Hour)
	if nextRenewal.Before(now.Add(24 * time.Hour)) {
		nextRenewal = now.Add(24 * time.Hour)
	}
	certPath, keyPath := managedCertificatePaths(certificate.ID)
	if err := h.services.CertificateLifecycle.Issued(parent, operation.ID, certificate.ID, capabilitynetwork.CertificateIssued{
		CertPath: certPath, KeyPath: keyPath, SerialNumber: metadata.SerialNumber,
		FingerprintSHA256: metadata.FingerprintSHA256, NotBefore: metadata.NotBefore,
		NotAfter: metadata.NotAfter, IssuedAt: now, NextRenewalAt: nextRenewal,
	}); err != nil {
		h.finishCertificateOperationFailure(operation, certificate, "persisting", err, false)
		return
	}

	_ = h.services.CertificateLifecycle.Phase(parent, operation.ID, "publishing")
	if prepared.BindingProtocolEndpointID != 0 {
		ctx, cancel := context.WithTimeout(parent, nodeConfigPublishTimeout)
		_, _, publishErr := h.publishNodeConfigForNode(ctx, certificate.NodeID, prepared.BindingProtocolEndpointID, requestedByValue(operation.RequestedBy))
		cancel()
		if publishErr != nil {
			h.finishCertificateOperationFailure(operation, certificate, "publishing",
				fmt.Errorf("certificate is active but Zero configuration publish failed: %w", publishErr), true)
			return
		}
	}
	finished := time.Now().UTC()
	summary := fmt.Sprintf("%s certificate valid until %s", operation.OperationType, metadata.NotAfter.Format(time.RFC3339))
	_ = h.services.CertificateLifecycle.Complete(parent, operation.ID, summary, finished)
}

func certificateExecutionNodeModel(row capabilitynetwork.CertificateExecutionNode) model.Node {
	return model.Node{
		ID: row.ID, SSHHost: row.SSHHost, SSHPort: row.SSHPort, SSHUser: row.SSHUser,
		SSHAuthMethod: row.SSHAuthMethod, SSHPwd: row.SSHPwdCiphertext,
		SSHPrivateKeyPassphrase: row.SSHPrivateKeyPassphraseCiphertext,
		SSHPrivilegeMode:        row.SSHPrivilegeMode, SSHPrivilegePassword: row.SSHPrivilegePasswordCiphertext,
		SSHHostKeyFingerprint: row.SSHHostKeyFingerprint,
	}
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
	nextRetry := now.Add(certificateRetryInterval)
	_ = h.services.CertificateLifecycle.Fail(context.Background(), operation.ID, certificate.ID, capabilitynetwork.CertificateFailure{
		Phase: phase, Message: message, Usable: certificateUsable, At: now, NextRetry: nextRetry,
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
	h.startScheduledJob("certificate_renewal", certificateRenewalScanInterval, func(ctx context.Context) error { return h.scanCertificateRenewals(time.Now().UTC()) })
}

func (h *handlers) scanCertificateRenewals(now time.Time) error {
	if h.backgroundWorkPaused() {
		return nil
	}
	certificateIDs, err := h.services.CertificateLifecycle.DueRenewals(context.Background(), now, 20)
	if err != nil {
		return err
	}
	var failures []error
	for _, certificateID := range certificateIDs {
		_, err := h.startManagedCertificateOperation(certificateID, certificateOperationRenew, nil)
		if err != nil && !errors.Is(err, errCertificateOperationRunning) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
