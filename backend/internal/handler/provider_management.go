package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
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

var providerCatalog = []network.ProviderDefinition{
	{Key: providerCloudflare, Name: "Cloudflare", Capabilities: []string{"dns.records", "certificate.origin"}},
	{Key: "letsencrypt", Name: "Let's Encrypt", Capabilities: []string{"certificate.public"}},
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
	Comment string `json:"comment"`
}

func (h *handlers) ProviderDefinitionListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	catalog := append([]network.ProviderDefinition{}, providerCatalog...)
	if h.pluginManager != nil {
		providers, err := h.pluginManager.ProviderDefinitions(r.Context())
		if err == nil {
			for _, provider := range providers {
				catalog = append(catalog, network.ProviderDefinition{Key: provider.Key, Name: provider.Name, Capabilities: append([]string{}, provider.Capabilities...)})
			}
		}
	}
	OK(w, catalog)
}

func (h *handlers) ProviderAccountListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	accounts, err := h.services.ProviderDirectory.List(r.Context(), claims.UserID)
	if err != nil {
		providerAccountError(w, err)
		return
	}
	OK(w, accounts)
}

func (h *handlers) ProviderAccountCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request network.ProviderCreate
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	account, verificationErr, err := h.services.ProviderCreation(h.credentialCipher, h, h).Create(r.Context(), claims.UserID, request)
	if err != nil {
		providerAccountError(w, err)
		return
	}
	message := "provider account created"
	if verificationErr != nil {
		message = "provider account saved but verification failed"
	}
	writeJSON(w, http.StatusCreated, message, account)
}

func (h *handlers) ProviderAccountVerifyHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/provider-accounts/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	account, err := h.services.ProviderAccounts(h.credentialCipher, h).Verify(r.Context(), claims.UserID, id)
	if err != nil {
		if errors.Is(err, network.ErrProviderConflict) || errors.Is(err, network.ErrProviderPermission) || errors.Is(err, network.ErrProviderNotFound) {
			providerAccountError(w, err)
		} else {
			BadRequest(w, err.Error())
		}
		return
	}
	capabilities, _ := json.Marshal(account.Capabilities)
	OK(w, struct {
		network.ProviderAccount
		Capabilities string `json:"capabilities"`
	}{account, string(capabilities)})
}

func (h *handlers) ManagedDNSListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	page, err := h.services.ManagedDNS(h.credentialCipher, h, h).List(r.Context(), claims.UserID, network.ManagedDNSListQuery{
		Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), Offset: offset, Limit: limit,
	})
	if err != nil {
		managedDNSError(w, err)
		return
	}
	OK(w, pagedData(page.Records, page.Total, offset, limit))
}

func (h *handlers) ManagedDNSCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request network.ManagedDNSWrite
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.backgroundJobs()
	records, operations, err := h.services.ManagedDNS(h.credentialCipher, h, h).Create(r.Context(), claims.UserID, request)
	if err != nil {
		managedDNSError(w, err)
		return
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
	var request network.ManagedDNSWrite
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.backgroundJobs()
	_, operation, err := h.services.ManagedDNS(h.credentialCipher, h, h).Update(r.Context(), claims.UserID, id, request)
	if err != nil {
		managedDNSError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "dns update synchronization started", operation)
}

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
	h.backgroundJobs()
	operation, err := h.services.ManagedDNS(h.credentialCipher, h, h).Start(r.Context(), claims.UserID, id, takeover)
	if err != nil {
		managedDNSError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "dns synchronization started", operation)
}

func (h *handlers) startDNSOperation(recordID uint, takeover bool, requestedBy *uint) (model.ProviderOperation, error) {
	h.backgroundJobs()
	actor := uint(0)
	if requestedBy != nil {
		actor = *requestedBy
	}
	operation, err := h.services.ManagedDNS(h.credentialCipher, h, h).Start(context.Background(), actor, recordID, takeover)
	return model.ProviderOperation{
		ID: operation.ID, ProviderAccountID: operation.ProviderAccountID, ResourceType: operation.ResourceType,
		ResourceID: operation.ResourceID, OperationType: operation.OperationType, Status: operation.Status,
		Phase: operation.Phase, RequestedBy: operation.RequestedBy, ResultSummary: operation.ResultSummary,
		Error: operation.Error, StartedAt: operation.StartedAt, FinishedAt: operation.FinishedAt,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt,
	}, err
}

func (h *handlers) executeDNSOperation(operationID uint) {
	h.executeDNSOperationContext(context.Background(), operationID)
}
func (h *handlers) executeDNSOperationContext(parent context.Context, operationID uint) error {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	return h.services.ManagedDNS(h.credentialCipher, h, h).Execute(ctx, operationID)
}

func (h *handlers) ApplyManagedDNS(ctx context.Context, request network.ManagedDNSProviderRequest, progress func(string) error) (network.ManagedDNSProviderResult, error) {
	if request.ProviderKey != providerCloudflare {
		if h.pluginManager == nil {
			return network.ManagedDNSProviderResult{}, plugins.ErrUnavailable
		}
		if err := progress("dispatching_provider"); err != nil {
			return network.ManagedDNSProviderResult{}, err
		}
		return h.pluginManager.ApplyDNSProvider(ctx, request.ProviderKey, request.Credential, request.Record, request.Takeover)
	}
	record := request.Record
	zone, err := findCloudflareZone(ctx, request.Credential, record.DomainName)
	if err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	if err := progress("inspecting_record"); err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	existing, err := findCloudflareRecord(ctx, request.Credential, zone.ID, record.RecordType, record.DomainName)
	if err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	if existing != nil && record.ProviderRecordID == "" && !request.Takeover {
		return network.ManagedDNSProviderResult{}, errors.New("远端已存在同名记录；请明确选择接管后重试")
	}
	if err := progress("applying_record"); err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	applied, err := applyCloudflareManagedDNSRecord(ctx, request.Credential, zone.ID, record, existing)
	if err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	return network.ManagedDNSProviderResult{ZoneID: zone.ID, RecordID: applied.ID, RecordType: applied.Type, Name: applied.Name, Value: applied.Content, TTL: applied.TTL, Proxied: applied.Proxied}, nil
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
	return network.DNSRecordHash(recordType, name, value, ttl, proxied)
}

func (h *handlers) ManagedDNSResolves(ctx context.Context, record network.ManagedDNSRecord) bool {
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
	h.startScheduledJob("dns_observation", dnsPublicCheckInterval, func(ctx context.Context) error { return h.observePublicDNS(ctx, time.Now().UTC()) })
}

func (h *handlers) scanDNSPublicObservations(now time.Time) {
	_ = h.observePublicDNS(context.Background(), now)
}
func (h *handlers) observePublicDNS(ctx context.Context, now time.Time) error {
	return h.services.ManagedDNS(h.credentialCipher, h, h).ObservePublic(ctx, now, dnsPublicCheckInterval, dnsPublicCheckTimeout, 4)
}
