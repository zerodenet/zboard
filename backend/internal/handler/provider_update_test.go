package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProviderUpdatePreservesReferencesAndCredentialOnRename(t *testing.T) {
	f, node, account := deletionFixture(t)
	record := seedDeletionDNS(t, f, node, account, "rename")
	w := httptest.NewRecorder()
	f.h.ProviderAccountUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), f.admin, fmt.Sprintf(`{"name":"Renamed","expected_revision":%d}`, account.Revision)))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var saved model.ProviderAccount
	f.h.db.First(&saved, account.ID)
	if saved.Name != "Renamed" || saved.CredentialCiphertext != account.CredentialCiphertext || saved.Status != account.Status || saved.Revision != account.Revision+1 {
		t.Fatalf("unexpected saved state: name=%s status=%s revision=%d", saved.Name, saved.Status, saved.Revision)
	}
	var dns model.ManagedDNSRecord
	f.h.db.First(&dns, record.ID)
	if dns.ProviderAccountID != account.ID {
		t.Fatal("reference lost")
	}
	if strings.Contains(w.Body.String(), account.CredentialCiphertext) {
		t.Fatal("encrypted credential exposed")
	}
}

func TestProviderUpdateVerifiesReplacementBeforeSaving(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			f, _, account := deletionFixture(t)
			token := "replacement-cloudflare-token-123456"
			mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/user/tokens/verify" || r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("unexpected verification request")
				}
				if valid {
					fmt.Fprint(w, `{"success":true,"result":{"status":"active"}}`)
				} else {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"success":false,"errors":[{"message":"denied"}]}`)
				}
			})
			w := httptest.NewRecorder()
			f.h.ProviderAccountUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), f.admin, fmt.Sprintf(`{"name":"Replacement","api_token":%q,"expected_revision":%d}`, token, account.Revision)))
			var saved model.ProviderAccount
			f.h.db.First(&saved, account.ID)
			if !valid {
				if w.Code != 400 || saved.CredentialCiphertext != account.CredentialCiphertext || saved.Name != account.Name {
					t.Fatalf("invalid replacement persisted: %d %s", w.Code, w.Body.String())
				}
			} else {
				secret, err := f.h.credentialCipher.Decrypt(saved.CredentialCiphertext)
				if w.Code != 200 || err != nil || secret != token || saved.Status != "active" || saved.LastVerifiedAt == nil {
					t.Fatalf("replacement failed: %d", w.Code)
				}
			}
			if strings.Contains(w.Body.String(), token) {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestProviderUpdateRejectsStaleRevisionAndNonAdmin(t *testing.T) {
	f, _, account := deletionFixture(t)
	for _, tc := range []struct {
		token    string
		revision uint64
		status   int
	}{{f.admin, account.Revision + 1, 409}, {f.token, account.Revision, 403}} {
		w := httptest.NewRecorder()
		f.h.ProviderAccountUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), tc.token, fmt.Sprintf(`{"name":"Changed","expected_revision":%d}`, tc.revision)))
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d", w.Code, tc.status)
		}
	}
}

func TestProviderUpdateDuplicateNameReturnsFieldError(t *testing.T) {
	f, _, account := deletionFixture(t)
	other := account
	other.ID, other.Name = 0, "Existing provider"
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.ProviderAccountUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), f.admin, fmt.Sprintf(`{"name":"Existing provider","expected_revision":%d}`, account.Revision)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "账户名称已存在") {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body.String())
	}
	var saved model.ProviderAccount
	f.h.db.First(&saved, account.ID)
	if saved.Name != account.Name || saved.Revision != account.Revision {
		t.Fatal("failed rename changed account")
	}
}

func TestProviderCreateReturnsInvalidVerificationState(t *testing.T) {
	f := newTrafficReadFixture(t)
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"success":false,"errors":[{"message":"denied"}]}`)
	})
	w := httptest.NewRecorder()
	f.h.ProviderAccountCreateHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/provider-accounts", f.admin, `{"provider_key":"cloudflare","name":"Invalid provider","api_token":"invalid-cloudflare-token"}`))
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"status":"invalid"`) || !strings.Contains(w.Body.String(), `"last_error":"denied"`) {
		t.Fatalf("state: %d %s", w.Code, w.Body.String())
	}
}
