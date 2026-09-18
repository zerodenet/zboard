package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

// VerifyProviderCredential is the temporary transport bridge until supplier
// implementations move behind the provider plugin registry.
func (h *handlers) VerifyProviderCredential(ctx context.Context, key, token string) error {
	if key == providerCloudflare {
		_, err := cloudflareRequest[json.RawMessage](ctx, http.MethodGet, "/user/tokens/verify", token, nil)
		return err
	}
	if h.pluginManager == nil {
		return errors.New("unsupported provider credential verification")
	}
	definitions, err := h.pluginManager.ProviderDefinitions(ctx)
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if definition.Key != key {
			continue
		}
		matched := false
		for _, capability := range definition.Capabilities {
			if capability == "dns.records" {
				matched = true
				if err := h.pluginManager.VerifyDNSProviderCredential(ctx, key, token); err != nil {
					return err
				}
			}
			if capability == "certificate.issue" {
				matched = true
				if err := h.pluginManager.VerifyCertificateProviderCredential(ctx, key, token); err != nil {
					return err
				}
			}
		}
		if matched {
			return nil
		}
	}
	return errors.New("unsupported provider credential verification")
}

func (h *handlers) ProviderDefinition(ctx context.Context, key string) (network.ProviderDefinition, error) {
	for _, definition := range providerCatalog {
		if definition.Key == key {
			return definition, nil
		}
	}
	if h.pluginManager != nil {
		providers, err := h.pluginManager.ProviderDefinitions(ctx)
		if err != nil {
			return network.ProviderDefinition{}, err
		}
		for _, provider := range providers {
			if provider.Key == key {
				return network.ProviderDefinition{Key: provider.Key, Name: provider.Name, Capabilities: append([]string{}, provider.Capabilities...)}, nil
			}
		}
	}
	return network.ProviderDefinition{}, errors.New("unsupported provider")
}
func (h *handlers) ProviderAccountUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/provider-accounts/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request network.ProviderUpdate
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	account, err := h.services.ProviderAccounts(h.credentialCipher, h).Update(r.Context(), claims.UserID, id, request)
	if err != nil {
		providerAccountError(w, err)
		return
	}
	OK(w, account)
}
func providerAccountError(w http.ResponseWriter, err error) {
	var validation *network.ProviderValidation
	switch {
	case errors.As(err, &validation):
		BadRequestFields(w, validation.Error(), validation.Fields)
	case errors.Is(err, network.ErrProviderConflict):
		writeJSON(w, http.StatusConflict, "供应商账户已更新，请重新打开编辑窗口。", nil)
	case errors.Is(err, network.ErrProviderNotFound):
		NotFound(w)
	case errors.Is(err, network.ErrProviderPermission):
		writeJSON(w, http.StatusForbidden, "需要管理员权限。", nil)
	case errors.Is(err, network.ErrProviderDuplicate):
		BadRequestFields(w, "供应商账户校验失败。", map[string]string{"name": "账户名称已存在，请使用其他名称。"})
	default:
		ServerError(w, err)
	}
}

func managedDNSError(w http.ResponseWriter, err error) {
	var validation *network.ManagedDNSValidation
	switch {
	case errors.As(err, &validation):
		BadRequestFields(w, validation.Error(), validation.Fields)
	case errors.Is(err, network.ErrManagedDNSNotFound):
		NotFound(w)
	case errors.Is(err, network.ErrManagedDNSPermission):
		writeJSON(w, http.StatusForbidden, "需要管理员权限。", nil)
	case errors.Is(err, network.ErrManagedDNSRevisionConflict):
		writeJSON(w, http.StatusConflict, "DNS 记录已被其他管理员更新，请重新加载。", nil)
	case errors.Is(err, network.ErrManagedDNSOperationRunning):
		writeJSON(w, http.StatusConflict, "DNS 记录正在执行供应商操作，请等待完成后再编辑。", nil)
	case errors.Is(err, network.ErrManagedDNSDeleting):
		writeJSON(w, http.StatusConflict, "DNS 记录已进入删除流程。", nil)
	case errors.Is(err, network.ErrManagedDNSDuplicate):
		BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"domain_name": "该供应商账户下已管理相同域名和记录类型。"})
	case errors.Is(err, network.ErrManagedDNSDependency):
		BadRequestFields(w, "DNS 解析校验失败。", map[string]string{"resource": "目标节点或供应商账户当前不可用。"})
	default:
		ServerError(w, err)
	}
}
