package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
)

func TestNavigationEnforcesSurfaceIdentityAndAdminMenuWrites(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	for _, surface := range []string{"admin", "account"} {
		w := httptest.NewRecorder()
		h.NavigationHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/navigation?surface="+surface, nil))
		if w.Code != http.StatusForbidden {
			t.Fatal("anonymous surface admitted", surface, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.NavigationHandler(w, announcementRequest(http.MethodGet, "/api/v1/navigation?surface=account", token, ""))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	var response struct {
		Data navigation.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, node := range response.Data.Nodes {
		if node.Condition == "admin" {
			t.Fatal("admin-only entry exposed", node)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := httptest.NewRecorder()
		h.AdminMenusHandler(w, announcementRequest(method, "/api/v1/admin/menus?surface=admin", token, "{}"))
		if w.Code != http.StatusForbidden {
			t.Fatal("nonadmin registry access", w.Code)
		}
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := menustore.Read(h.db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Nodes[0].Hidden = true
	body, _ := json.Marshal(snapshot)
	w = httptest.NewRecorder()
	h.AdminMenusHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/menus?surface=admin", adminToken, string(body)))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.AdminMenusHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/menus?surface=admin", adminToken, string(body)))
	if w.Code != http.StatusConflict {
		t.Fatal("stale save admitted", w.Code, w.Body)
	}
}

func TestNavigationReportsDirectHiddenPageAsUnavailable(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.MenuNode{}).Where("id = ?", "account:/account/orders").Update("hidden", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.MenuNode{ID: "custom:orders", Surface: "account", Owner: "custom", Label: "快捷订单", Path: "/account/orders"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/account/orders", false}, {"/account/orders/42", false}, {"/account/plans", true},
	} {
		w := httptest.NewRecorder()
		h.NavigationHandler(w, announcementRequest(http.MethodGet, "/api/v1/navigation?surface=account&path="+url.QueryEscape(tc.path), token, ""))
		var response struct {
			Data struct {
				navigation.Snapshot
				PageAvailable *bool `json:"page_available"`
			} `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.PageAvailable == nil || *response.Data.PageAvailable != tc.want {
			t.Fatal("unexpected page availability", tc.path, w.Code, w.Body)
		}
		for _, node := range response.Data.Nodes {
			if node.Path == "/account/orders" {
				t.Fatal("hidden page still in navigation", node)
			}
		}
	}
	w := httptest.NewRecorder()
	h.NavigationHandler(w, announcementRequest(http.MethodGet, "/api/v1/navigation?surface=account&path=https://example.test", token, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatal("invalid page path admitted", w.Code, w.Body)
	}
}
