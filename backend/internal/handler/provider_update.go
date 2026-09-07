package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// An omitted token preserves the encrypted credential and its verification state.
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
	var request struct {
		Name             string `json:"name"`
		APIToken         string `json:"api_token"`
		ExpectedRevision uint64 `json:"expected_revision"`
	}
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.APIToken = strings.TrimSpace(request.APIToken)
	fields := map[string]string{}
	if request.Name == "" || len([]byte(request.Name)) > 80 {
		fields["name"] = "账户名称需要包含 1–80 个 UTF-8 字节。"
	}
	if request.APIToken != "" && (len(request.APIToken) < 20 || len(request.APIToken) > 512) {
		fields["api_token"] = "请输入有效的 Cloudflare API Token，或留空保留原凭据。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "供应商账户校验失败。", fields)
		return
	}
	var account model.ProviderAccount
	if err := h.db.First(&account, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
		} else {
			ServerError(w, err)
		}
		return
	}
	if request.ExpectedRevision != account.Revision {
		writeJSON(w, http.StatusConflict, "供应商账户已更新，请重新打开编辑窗口。", nil)
		return
	}
	updates := map[string]interface{}{"name": request.Name, "revision": account.Revision + 1}
	if request.APIToken != "" {
		// Verify before replacing a working token used by DNS and certificates.
		if _, err := cloudflareRequest[json.RawMessage](r.Context(), http.MethodGet, "/user/tokens/verify", request.APIToken, nil); err != nil {
			BadRequestFields(w, "新 Token 验证失败，原凭据已保留。", map[string]string{"api_token": "请检查 Cloudflare Token 是否有效以及所需权限后重试。"})
			return
		}
		encrypted, err := h.credentialCipher.Encrypt(request.APIToken)
		if err != nil {
			ServerError(w, err)
			return
		}
		updates["credential_ciphertext"] = encrypted
		updates["credential_prefix"] = secretPrefix(request.APIToken)
		updates["status"] = "active"
		updates["last_verified_at"] = time.Now().UTC()
		updates["last_error"] = ""
	}
	conflict := errors.New("provider revision conflict")
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var current model.ProviderAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return err
		}
		if current.Revision != request.ExpectedRevision {
			return conflict
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "provider_account.update", fmt.Sprintf("provider_account:%d", id), fmt.Sprintf("credential_replaced=%t", request.APIToken != ""))
	})
	if errors.Is(err, conflict) {
		writeJSON(w, http.StatusConflict, "供应商账户已更新，请重新打开编辑窗口。", nil)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if isDuplicateError(err) {
		BadRequestFields(w, "供应商账户校验失败。", map[string]string{"name": "账户名称已存在，请使用其他名称。"})
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.First(&account, id).Error; err != nil {
		ServerError(w, err)
		return
	}
	var capabilities []string
	_ = json.Unmarshal([]byte(account.Capabilities), &capabilities)
	OK(w, providerAccountView{ProviderAccount: account, Capabilities: capabilities})
}
