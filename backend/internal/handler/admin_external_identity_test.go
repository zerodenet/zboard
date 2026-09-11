package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestAdminCanInspectAndUnlinkExternalIdentity(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	target := model.User{Email: "oauth-user@example.test", Password: "!external", Status: userStatusActive}
	if err := h.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.ExternalIdentity{
		ID: "binding-1", UserID: target.ID, PluginID: "zboard.oauth~github",
		Publisher: "higanbana986", Issuer: "https://github.com", Subject: "github-user-42",
	}
	if err := h.db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	items, err := loadAdminUserListItems(h.db, []model.User{target}, binding.CreatedAt)
	if err != nil || len(items) != 1 || items[0].IdentityBindingCount != 1 {
		t.Fatalf("user list binding count = %+v, err=%v", items, err)
	}

	detailResponse := httptest.NewRecorder()
	h.AdminUserGetHandler(detailResponse, announcementRequest(http.MethodGet, "/api/v1/admin/users/"+strconv.FormatUint(uint64(target.ID), 10), token, ""))
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d body = %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail struct {
		Data struct {
			Count      int                     `json:"identity_binding_count"`
			Identities []adminExternalIdentity `json:"external_identities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.Count != 1 || len(detail.Data.Identities) != 1 || detail.Data.Identities[0].ProviderID != "github" || detail.Data.Identities[0].Subject != "github-user-42" {
		t.Fatalf("binding detail missing: %+v", detail.Data)
	}

	request := pathvar.WithVars(
		announcementRequest(http.MethodDelete, "/api/v1/admin/users/2/identities/binding-1", token, ""),
		map[string]string{"user_id": strconv.FormatUint(uint64(target.ID), 10), "identity_id": binding.ID},
	)
	unlinkResponse := httptest.NewRecorder()
	h.AdminExternalIdentityDeleteHandler(unlinkResponse, request)
	if unlinkResponse.Code != http.StatusOK {
		t.Fatalf("unlink status = %d body = %s", unlinkResponse.Code, unlinkResponse.Body.String())
	}
	var remaining int64
	if err := h.db.Model(&model.ExternalIdentity{}).Where("id = ?", binding.ID).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("binding still exists: count=%d err=%v", remaining, err)
	}
	var audit model.AuditLog
	if err := h.db.Where("action = ? AND target = ?", "identity.unlink.admin", "identity:"+binding.ID).First(&audit).Error; err != nil {
		t.Fatal("administrator unlink was not audited", err)
	}
}
