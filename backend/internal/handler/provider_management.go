package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	providerCloudflare = "cloudflare"
	dnsStatusPending   = "pending"
	dnsStatusSyncing   = "syncing"
	dnsStatusActive    = "active"
	dnsStatusDrifted   = "drifted"
	dnsStatusFailed    = "failed"

	dnsPublicCheckInterval = 15 * time.Second
	dnsPublicCheckTimeout  = 8 * time.Second
)

var cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

type providerDefinition struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

var providerCatalog = []providerDefinition{
	{Key: providerCloudflare, Name: "Cloudflare", Capabilities: []string{"dns.records", "certificate.origin"}},
	{Key: "letsencrypt", Name: "Let's Encrypt", Capabilities: []string{"certificate.public"}},
}

type providerAccountWriteRequest struct {
	ProviderKey string `json:"provider_key"`
	Name        string `json:"name"`
	APIToken    string `json:"api_token"`
}

type managedDNSWriteRequest struct {
	ProviderAccountID uint                    `json:"provider_account_id"`
	NodeID            uint                    `json:"node_id"`
	DomainName        string                  `json:"domain_name"`
	RecordType        string                  `json:"record_type"`
	RecordValue       string                  `json:"record_value"`
	TTL               int                     `json:"ttl"`
	Proxied           bool                    `json:"proxied"`
	TakeoverExisting  bool                    `json:"takeover_existing"`
	ExpectedRevision  uint64                  `json:"expected_revision"`
	Records           []managedDNSRecordInput `json:"records"`
}

type managedDNSRecordInput struct {
	RecordType  string `json:"record_type"`
	RecordValue string `json:"record_value"`
}

type providerAccountView struct {
	model.ProviderAccount
	Capabilities []string `json:"capabilities"`
	UsageCount   int64    `json:"usage_count"`
}

type managedDNSView struct {
	model.ManagedDNSRecord
	ProviderName    string                   `json:"provider_name"`
	ProviderKey     string                   `json:"provider_key"`
	NodeName        string                   `json:"node_name"`
	LatestOperation *model.ProviderOperation `json:"latest_operation,omitempty"`
}

type cloudflareEnvelope[T any] struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
	Result  T                    `json:"result"`
}

type cloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareRequestError struct {
	StatusCode int
	Message    string
}

func (e *cloudflareRequestError) Error() string { return e.Message }

type cloudflareZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cloudflareRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func (h *handlers) ProviderDefinitionListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	OK(w, providerCatalog)
}

func (h *handlers) ProviderAccountListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var accounts []model.ProviderAccount
	if err := h.db.Order("id desc").Find(&accounts).Error; err != nil {
		ServerError(w, err)
		return
	}
	views := make([]providerAccountView, 0, len(accounts))
	for _, account := range accounts {
		var capabilities []string
		_ = json.Unmarshal([]byte(account.Capabilities), &capabilities)
		var count int64
		if err := h.db.Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", account.ID).Count(&count).Error; err != nil {
			ServerError(w, err)
			return
		}
		var certificates int64
		if err := h.db.Model(&model.ManagedCertificate{}).Where("provider_account_id = ?", account.ID).Count(&certificates).Error; err != nil {
			ServerError(w, err)
			return
		}
		count += certificates
		views = append(views, providerAccountView{ProviderAccount: account, Capabilities: capabilities, UsageCount: count})
	}
	OK(w, views)
}

func (h *handlers) ProviderAccountCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request providerAccountWriteRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.ProviderKey = strings.ToLower(strings.TrimSpace(request.ProviderKey))
	request.Name = strings.TrimSpace(request.Name)
	request.APIToken = strings.TrimSpace(request.APIToken)
	fields := map[string]string{}
	if request.ProviderKey != providerCloudflare {
		fields["provider_key"] = "当前只支持创建 Cloudflare 外部供应商账户。"
	}
	if request.Name == "" || len([]byte(request.Name)) > 80 {
		fields["name"] = "账户名称需要包含 1–80 个 UTF-8 字节。"
	}
	if len(request.APIToken) < 20 || len(request.APIToken) > 512 {
		fields["api_token"] = "请输入有效的 Cloudflare API Token。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "供应商账户校验失败。", fields)
		return
	}
	encrypted, err := h.credentialCipher.Encrypt(request.APIToken)
	if err != nil {
		ServerError(w, err)
		return
	}
	account := model.ProviderAccount{
		ProviderKey: request.ProviderKey, Name: request.Name,
		Capabilities:         `["dns.records","certificate.origin"]`,
		CredentialCiphertext: encrypted, CredentialPrefix: secretPrefix(request.APIToken),
		Status: "pending", Revision: 1, CreatedBy: claims.UserID,
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "provider_account.create", fmt.Sprintf("provider_account:%d", account.ID), "provider=cloudflare")
	}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			BadRequestFields(w, "供应商账户校验失败。", map[string]string{"name": "账户名称已存在。"})
			return
		}
		ServerError(w, err)
		return
	}
	verifyErr := h.verifyProviderAccount(r.Context(), &account)
	message := "provider account created"
	if verifyErr != nil {
		message = "provider account saved but verification failed"
	}
	writeJSON(w, http.StatusCreated, message, providerAccountView{ProviderAccount: account, Capabilities: []string{"dns.records", "certificate.origin"}})
}

func (h *handlers) ProviderAccountVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/provider-accounts/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var account model.ProviderAccount
	if err := h.db.First(&account, id).Error; err != nil {
		NotFound(w)
		return
	}
	if err := h.verifyProviderAccount(r.Context(), &account); err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, account)
}

func (h *handlers) verifyProviderAccount(ctx context.Context, account *model.ProviderAccount) error {
	token, err := h.credentialCipher.Decrypt(account.CredentialCiphertext)
	if err != nil {
		return err
	}
	_, err = cloudflareRequest[json.RawMessage](ctx, http.MethodGet, "/user/tokens/verify", token, nil)
	now := time.Now().UTC()
	if err != nil {
		_ = h.db.Model(account).Updates(map[string]interface{}{"status": "invalid", "last_error": truncateCertificateError(err.Error())}).Error
		return err
	}
	if err := h.db.Model(account).Updates(map[string]interface{}{"status": "active", "last_verified_at": now, "last_error": ""}).Error; err != nil {
		return err
	}
	account.Status, account.LastVerifiedAt, account.LastError = "active", &now, ""
	return nil
}

func (h *handlers) ManagedDNSListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.ManagedDNSRecord{})
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query = query.Where("domain_name LIKE ?", "%"+q+"%")
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	var records []model.ManagedDNSRecord
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		ServerError(w, err)
		return
	}
	views, err := h.decorateManagedDNSRecords(records)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(views, total, offset, limit))
}

func (h *handlers) decorateManagedDNSRecords(records []model.ManagedDNSRecord) ([]managedDNSView, error) {
	views := make([]managedDNSView, 0, len(records))
	for _, record := range records {
		var account model.ProviderAccount
		var node model.Node
		if err := h.db.First(&account, record.ProviderAccountID).Error; err != nil {
			return nil, err
		}
		if err := h.db.First(&node, record.NodeID).Error; err != nil {
			return nil, err
		}
		var operation model.ProviderOperation
		op := (*model.ProviderOperation)(nil)
		err := h.db.Where("resource_type = ? AND resource_id = ?", "dns_record", record.ID).Order("id desc").First(&operation).Error
		if err == nil {
			op = &operation
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		views = append(views, managedDNSView{ManagedDNSRecord: record, ProviderName: account.Name, ProviderKey: account.ProviderKey, NodeName: node.Name, LatestOperation: op})
	}
	return views, nil
}

func (h *handlers) ManagedDNSCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request managedDNSWriteRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	inputs := request.Records
	if len(inputs) == 0 {
		inputs = []managedDNSRecordInput{{RecordType: request.RecordType, RecordValue: request.RecordValue}}
	}
	if len(inputs) < 1 || len(inputs) > 2 {
		BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"records": "一次只能创建一条 A、一条 AAAA 记录。"})
		return
	}
	records := make([]model.ManagedDNSRecord, 0, len(inputs))
	seenTypes := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		recordRequest := request
		recordRequest.RecordType = input.RecordType
		recordRequest.RecordValue = input.RecordValue
		recordRequest.Records = nil
		record, validateErr := h.validateManagedDNSRequest(recordRequest)
		if validateErr != nil {
			BadRequestError(w, prefixValidationError(validateErr, fmt.Sprintf("records.%d.", index)))
			return
		}
		if _, exists := seenTypes[record.RecordType]; exists {
			BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"records": "A 和 AAAA 各自最多提交一次。"})
			return
		}
		seenTypes[record.RecordType] = struct{}{}
		record.CreatedBy = claims.UserID
		records = append(records, record)
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		for index := range records {
			if err := requireAvailableNode(tx, records[index].NodeID); err != nil {
				return err
			}
			if err := requireAvailableProvider(tx, records[index].ProviderAccountID); err != nil {
				return err
			}
			if err := tx.Create(&records[index]).Error; err != nil {
				return err
			}
			if err := createAuditLog(tx, claims, "dns_record.create", fmt.Sprintf("managed_dns_record:%d", records[index].ID),
				fmt.Sprintf("domain=%s type=%s node=%d provider_account=%d", records[index].DomainName, records[index].RecordType, records[index].NodeID, records[index].ProviderAccountID)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"domain_name": "该供应商账户下已管理相同域名和记录类型。"})
			return
		}
		ServerError(w, err)
		return
	}
	operations := make([]model.ProviderOperation, 0, len(records))
	for _, record := range records {
		operation, startErr := h.startDNSOperation(record.ID, request.TakeoverExisting, &claims.UserID)
		if startErr != nil {
			ServerError(w, startErr)
			return
		}
		operations = append(operations, operation)
	}
	writeJSON(w, http.StatusAccepted, "dns synchronization started", map[string]interface{}{"records": records, "operations": operations})
}

func (h *handlers) ManagedDNSUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/dns-records/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request managedDNSWriteRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if request.ExpectedRevision == 0 {
		BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"expected_revision": "请刷新后再编辑该 DNS 记录。"})
		return
	}
	updated, err := h.validateManagedDNSRequest(request)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	var currentRevision uint64
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var existing model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, id).Error; err != nil {
			return err
		}
		if err := requireAvailableNode(tx, existing.NodeID); err != nil {
			return err
		}
		if err := requireAvailableNode(tx, updated.NodeID); err != nil {
			return err
		}
		if existing.Status == resourceStatusDeleting {
			return errResourceDeleting
		}
		currentRevision = existing.Revision
		if existing.Revision != request.ExpectedRevision {
			return errManagedDNSRevisionConflict
		}
		if existing.ProviderAccountID != updated.ProviderAccountID || existing.DomainName != updated.DomainName || existing.RecordType != updated.RecordType {
			return validationError("DNS 解析校验失败。", map[string]string{
				"identity": "供应商、域名和记录类型不能直接修改；如需变更，请删除后重新创建。",
			})
		}
		var running int64
		if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", existing.ID, "running").Count(&running).Error; err != nil {
			return err
		}
		if running > 0 || existing.Status == dnsStatusSyncing {
			return errManagedDNSOperationRunning
		}
		updates := map[string]interface{}{
			"node_id": updated.NodeID, "record_value": updated.RecordValue, "ttl": updated.TTL,
			"proxied": updated.Proxied, "desired_hash": updated.DesiredHash,
			"status": dnsStatusPending, "public_resolved": false, "last_public_check_at": nil,
			"last_error": "", "revision": existing.Revision + 1,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "dns_record.update", fmt.Sprintf("managed_dns_record:%d", existing.ID),
			fmt.Sprintf("node=%d ttl=%d proxied=%t revision=%d", updated.NodeID, updated.TTL, updated.Proxied, existing.Revision+1))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errManagedDNSRevisionConflict) {
		writeJSON(w, http.StatusConflict, "DNS 记录已被其他管理员更新，请重新加载。", map[string]interface{}{"current_revision": currentRevision})
		return
	}
	if errors.Is(err, errManagedDNSOperationRunning) {
		writeJSON(w, http.StatusConflict, "DNS 记录正在执行供应商操作，请等待完成后再编辑。", nil)
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
	operation, err := h.startDNSOperation(id, false, &claims.UserID)
	if err != nil {
		ServerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "dns update synchronization started", operation)
}

var (
	errManagedDNSRevisionConflict = errors.New("managed DNS revision conflict")
	errManagedDNSOperationRunning = errors.New("managed DNS operation is running")
)

func deleteCloudflareDNSRecord(ctx context.Context, token, zoneID, recordID string) error {
	_, err := cloudflareRequest[json.RawMessage](ctx, http.MethodDelete,
		fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zoneID), url.PathEscape(recordID)), token, nil)
	return err
}

func cloudflareRecordAlreadyAbsent(err error) bool {
	var requestErr *cloudflareRequestError
	return errors.As(err, &requestErr) && requestErr.StatusCode == http.StatusNotFound
}

func (h *handlers) ManagedDNSSyncHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/dns-records/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	takeover := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("takeover")), "true")
	operation, err := h.startDNSOperation(id, takeover, &claims.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, "dns synchronization started", operation)
}

func (h *handlers) validateManagedDNSRequest(request managedDNSWriteRequest) (model.ManagedDNSRecord, error) {
	request.DomainName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(request.DomainName)), ".")
	request.RecordType = strings.ToUpper(strings.TrimSpace(request.RecordType))
	request.RecordValue = strings.TrimSpace(request.RecordValue)
	if request.TTL == 0 {
		request.TTL = 1
	}
	fields := map[string]string{}
	domains, domainFields := normalizeCertificateDomains([]string{request.DomainName})
	if len(domainFields) > 0 || len(domains) != 1 {
		fields["domain_name"] = "请输入有效的完整域名。"
	}
	if request.RecordType != "A" && request.RecordType != "AAAA" {
		fields["record_type"] = "第一版只支持 A 和 AAAA 记录。"
	}
	var node model.Node
	if request.NodeID == 0 || h.db.First(&node, request.NodeID).Error != nil {
		fields["node_id"] = "请选择有效的目标节点。"
	} else if request.RecordValue == "" {
		for _, candidate := range []string{strings.TrimSpace(node.Address), strings.TrimSpace(node.SSHHost)} {
			ip := net.ParseIP(candidate)
			if ip != nil && ((request.RecordType == "A" && ip.To4() != nil) || (request.RecordType == "AAAA" && ip.To4() == nil)) {
				request.RecordValue = candidate
				break
			}
		}
	}
	ip := net.ParseIP(request.RecordValue)
	if ip == nil || (request.RecordType == "A" && ip.To4() == nil) || (request.RecordType == "AAAA" && ip.To4() != nil) {
		fields["record_value"] = "记录值必须是与记录类型匹配的公网 IP 地址。"
	} else if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		fields["record_value"] = "记录值必须是可公开路由的节点 IP 地址。"
	}
	if request.TTL != 1 && (request.TTL < 60 || request.TTL > 86400) {
		fields["ttl"] = "TTL 必须为自动（1）或 60–86400 秒。"
	}
	var account model.ProviderAccount
	if request.ProviderAccountID == 0 || h.db.First(&account, request.ProviderAccountID).Error != nil || account.ProviderKey != providerCloudflare {
		fields["provider_account_id"] = "请选择有效的 Cloudflare 供应商账户。"
	} else if account.Status != "active" {
		fields["provider_account_id"] = "Cloudflare 账户尚未通过验证。"
	}
	if len(fields) > 0 {
		return model.ManagedDNSRecord{}, validationError("DNS 解析校验失败。", fields)
	}
	record := model.ManagedDNSRecord{
		ProviderAccountID: request.ProviderAccountID, NodeID: request.NodeID,
		DomainName: request.DomainName, RecordType: request.RecordType, RecordValue: request.RecordValue,
		TTL: request.TTL, Proxied: request.Proxied, Status: dnsStatusPending, Revision: 1,
	}
	record.DesiredHash = dnsRecordHash(record.RecordType, record.DomainName, record.RecordValue, record.TTL, record.Proxied)
	return record, nil
}

func (h *handlers) startDNSOperation(recordID uint, takeover bool, requestedBy *uint) (model.ProviderOperation, error) {
	var operation model.ProviderOperation
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var record model.ManagedDNSRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, recordID).Error; err != nil {
			return err
		}
		if record.Status == resourceStatusDeleting {
			return errResourceDeleting
		}
		if err := requireAvailableNode(tx, record.NodeID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ProviderOperation{}).Where("resource_type = ? AND resource_id = ? AND status = ?", "dns_record", record.ID, "running").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("DNS synchronization is already running")
		}
		now := time.Now().UTC()
		kind := "sync"
		if takeover {
			kind = "takeover"
		}
		operation = model.ProviderOperation{
			ProviderAccountID: record.ProviderAccountID, ResourceType: "dns_record", ResourceID: record.ID,
			OperationType: kind, Status: "running", Phase: "queued", RequestedBy: requestedBy, StartedAt: &now,
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		return tx.Model(&record).Updates(map[string]interface{}{"status": dnsStatusSyncing, "last_error": ""}).Error
	})
	if err == nil {
		go h.executeDNSOperation(operation.ID)
	}
	return operation, err
}

func (h *handlers) executeDNSOperation(operationID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var operation model.ProviderOperation
	if err := h.db.First(&operation, operationID).Error; err != nil {
		return
	}
	fail := func(phase string, err error) {
		now := time.Now().UTC()
		message := truncateCertificateError(err.Error())
		_ = h.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.ManagedDNSRecord{}).Where("id = ?", operation.ResourceID).Updates(map[string]interface{}{"status": dnsStatusFailed, "last_error": message}).Error; err != nil {
				return err
			}
			return tx.Model(&operation).Updates(map[string]interface{}{"status": "failed", "phase": phase, "error": message, "finished_at": now}).Error
		})
	}
	var record model.ManagedDNSRecord
	if err := h.db.First(&record, operation.ResourceID).Error; err != nil {
		fail("loading", err)
		return
	}
	var account model.ProviderAccount
	if err := h.db.First(&account, record.ProviderAccountID).Error; err != nil {
		fail("loading", err)
		return
	}
	token, err := h.credentialCipher.Decrypt(account.CredentialCiphertext)
	if err != nil {
		fail("credentials", err)
		return
	}
	_ = h.db.Model(&operation).Update("phase", "resolving_zone").Error
	zone, err := findCloudflareZone(ctx, token, record.DomainName)
	if err != nil {
		fail("resolving_zone", err)
		return
	}
	_ = h.db.Model(&operation).Update("phase", "inspecting_record").Error
	existing, err := findCloudflareRecord(ctx, token, zone.ID, record.RecordType, record.DomainName)
	if err != nil {
		fail("inspecting_record", err)
		return
	}
	if existing != nil && record.ProviderRecordID == "" && operation.OperationType != "takeover" {
		fail("inspecting_record", errors.New("远端已存在同名记录；请明确选择接管后重试"))
		return
	}
	_ = h.db.Model(&operation).Update("phase", "applying_record").Error
	payload := map[string]interface{}{"type": record.RecordType, "name": record.DomainName, "content": record.RecordValue, "ttl": record.TTL, "proxied": record.Proxied}
	path, method := "/zones/"+zone.ID+"/dns_records", http.MethodPost
	if existing != nil {
		path, method = path+"/"+existing.ID, http.MethodPut
	}
	applied, err := cloudflareRequest[cloudflareRecord](ctx, method, path, token, payload)
	if err != nil {
		fail("applying_record", err)
		return
	}
	observed := dnsRecordHash(applied.Type, strings.ToLower(applied.Name), applied.Content, applied.TTL, applied.Proxied)
	now := time.Now().UTC()
	status := dnsStatusActive
	if observed != record.DesiredHash {
		status = dnsStatusDrifted
	}
	publicResolved := verifyPublicDNS(ctx, record)
	if err := h.db.Model(&record).Updates(map[string]interface{}{
		"provider_zone_id": zone.ID, "provider_record_id": applied.ID, "observed_hash": observed,
		"status": status, "last_synced_at": now, "last_public_check_at": now,
		"public_resolved": publicResolved, "last_error": "",
	}).Error; err != nil {
		fail("persisting", err)
		return
	}
	summary := fmt.Sprintf("%s %s -> %s", record.RecordType, record.DomainName, record.RecordValue)
	_ = h.db.Model(&operation).Updates(map[string]interface{}{"status": "succeeded", "phase": "completed", "result_summary": summary, "error": "", "finished_at": now}).Error
}

func cloudflareRequest[T any](ctx context.Context, method, path, token string, payload interface{}) (T, error) {
	var zero T
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return zero, err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBaseURL+path, body)
	if err != nil {
		return zero, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return zero, err
	}
	var envelope cloudflareEnvelope[T]
	if err := json.Unmarshal(data, &envelope); err != nil {
		return zero, fmt.Errorf("Cloudflare returned an invalid response (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		messages := make([]string, 0, len(envelope.Errors))
		for _, apiErr := range envelope.Errors {
			messages = append(messages, apiErr.Message)
		}
		if len(messages) == 0 {
			messages = append(messages, response.Status)
		}
		return zero, &cloudflareRequestError{StatusCode: response.StatusCode, Message: strings.Join(messages, "; ")}
	}
	return envelope.Result, nil
}

func findCloudflareZone(ctx context.Context, token, domain string) (cloudflareZone, error) {
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1; i++ {
		candidate := strings.Join(labels[i:], ".")
		query := url.Values{"name": []string{candidate}, "status": []string{"active"}, "per_page": []string{"50"}}
		zones, err := cloudflareRequest[[]cloudflareZone](ctx, http.MethodGet, "/zones?"+query.Encode(), token, nil)
		if err != nil {
			return cloudflareZone{}, err
		}
		if len(zones) == 1 {
			return zones[0], nil
		}
		if len(zones) > 1 {
			return cloudflareZone{}, fmt.Errorf("Cloudflare 返回了多个匹配 Zone：%s", candidate)
		}
	}
	return cloudflareZone{}, fmt.Errorf("Cloudflare 账户中没有可管理 %s 的 Zone", domain)
}

func findCloudflareRecord(ctx context.Context, token, zoneID, recordType, name string) (*cloudflareRecord, error) {
	query := url.Values{"type": []string{recordType}, "name": []string{name}, "per_page": []string{"100"}}
	records, err := cloudflareRequest[[]cloudflareRecord](ctx, http.MethodGet,
		fmt.Sprintf("/zones/%s/dns_records?%s", zoneID, query.Encode()), token, nil)
	if err != nil {
		return nil, err
	}
	if len(records) > 1 {
		return nil, errors.New("Cloudflare 中存在多条同名同类型记录，无法安全接管")
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func dnsRecordHash(recordType, name, value string, ttl int, proxied bool) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%s\n%d\n%t", strings.ToUpper(recordType), strings.ToLower(name), value, ttl, proxied)))
	return fmt.Sprintf("%x", sum[:])
}

func verifyPublicDNS(ctx context.Context, record model.ManagedDNSRecord) bool {
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: 2 * time.Second}
				return dialer.DialContext(ctx, network, server)
			},
		}
		addresses, err := resolver.LookupIPAddr(ctx, record.DomainName)
		if err != nil {
			continue
		}
		expected := net.ParseIP(record.RecordValue)
		for _, address := range addresses {
			sameFamily := (record.RecordType == "A" && address.IP.To4() != nil) ||
				(record.RecordType == "AAAA" && address.IP.To4() == nil)
			if sameFamily && (record.Proxied || address.IP.Equal(expected)) {
				return true
			}
		}
	}
	return false
}

func (h *handlers) StartDNSPublicObservationWorker() {
	go func() {
		h.scanDNSPublicObservations(time.Now().UTC())
		ticker := time.NewTicker(dnsPublicCheckInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			h.scanDNSPublicObservations(now.UTC())
		}
	}()
}

func (h *handlers) scanDNSPublicObservations(now time.Time) {
	if h.backgroundWorkPaused() {
		return
	}
	var records []model.ManagedDNSRecord
	if err := h.db.Where(
		"public_resolved = ? AND last_synced_at IS NOT NULL AND status IN ? AND (last_public_check_at IS NULL OR last_public_check_at <= ?)",
		false, []string{dnsStatusActive, dnsStatusDrifted}, now.Add(-dnsPublicCheckInterval),
	).Order("last_public_check_at asc, id asc").Limit(50).Find(&records).Error; err != nil {
		return
	}
	for _, record := range records {
		ctx, cancel := context.WithTimeout(context.Background(), dnsPublicCheckTimeout)
		resolved := verifyPublicDNS(ctx, record)
		cancel()
		updates := map[string]interface{}{"last_public_check_at": now}
		if resolved {
			updates["public_resolved"] = true
		}
		_ = h.db.Model(&model.ManagedDNSRecord{}).Where("id = ? AND public_resolved = ?", record.ID, false).Updates(updates).Error
	}
}

func secretPrefix(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "…" + value[len(value)-4:]
}
