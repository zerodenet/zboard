package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestFilesAndTicketAttachmentsCompleteAuthenticatedLifecycle(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example"}).Error; err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	f.h.services.ConfigureFileStorage(directory)
	other := model.User{Email: "other-files@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := f.h.issueToken(authClaims{UserID: other.ID, Email: other.Email})
	if err != nil {
		t.Fatal(err)
	}
	upload := func(token, purpose, name, content string, status int) experience.StoredFile {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", name)
		_, _ = part.Write([]byte(content))
		_ = writer.WriteField("purpose", purpose)
		_ = writer.Close()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/files", &body)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		f.h.FileUploadHandler(w, r)
		if w.Code != status {
			t.Fatalf("upload %s: %d %s", purpose, w.Code, w.Body.String())
		}
		var envelope struct{ Data experience.StoredFile }
		_ = json.Unmarshal(w.Body.Bytes(), &envelope)
		return envelope.Data
	}
	request := func(fn http.HandlerFunc, token, method, target, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		fn(w, announcementRequest(method, target, token, body))
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, target, w.Code, w.Body.String())
		}
		return w
	}
	file := upload(f.token, "ticket", "日志.txt", "private connection log", 200)
	request(f.h.FileGetHandler, otherToken, "GET", file.URL(), "", 403)
	request(f.h.FileGetHandler, "", "GET", file.URL(), "", 401)
	request(f.h.FileGetHandler, "", "GET", "/media/"+file.ID, "", 404)
	get := request(f.h.FileGetHandler, f.token, "GET", file.URL(), "", 200)
	if get.Body.String() != "private connection log" || get.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(get.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("private response leaked or changed", get.Header())
	}
	created := request(f.h.TicketCreateHandler, f.token, "POST", "/api/v1/tickets", fmt.Sprintf(`{"subject":"Upload fixture","category":"connection","priority":1,"body":"Description","attachments":[{"file_id":%q},{"url":"https://files.example/trace.log","name":"远程日志"}]}`, file.ID), 200)
	var detail struct{ Data experience.TicketDetail }
	if err := json.Unmarshal(created.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Data.Messages) != 1 || len(detail.Data.Messages[0].Attachments) != 2 || detail.Data.Messages[0].Attachments[0].FileID != file.ID || detail.Data.Messages[0].Attachments[0].Size != file.Size {
		t.Fatalf("attachments did not persist: %s", created.Body.String())
	}
	id := detail.Data.Ticket.ID
	request(f.h.FileDeleteHandler, f.token, "DELETE", file.URL(), "", 409)
	request(f.h.FileGetHandler, f.admin, "GET", file.URL(), "", 200)
	f.h.services.ConfigureFileStorage(directory)
	request(f.h.FileGetHandler, f.token, "GET", file.URL(), "", 200)
	// Foreign references must roll back the entire create, not leave a ticket or
	// message without its requested attachment.
	request(f.h.TicketCreateHandler, otherToken, "POST", "/api/v1/tickets", fmt.Sprintf(`{"subject":"Foreign attachment","category":"other","priority":1,"body":"Description","attachments":[{"file_id":%q}]}`, file.ID), 400)
	var count int64
	f.h.db.Model(&model.Ticket{}).Count(&count)
	if count != 1 {
		t.Fatal("foreign attachment left a ticket", count)
	}
	adminFile := upload(f.admin, "ticket", "response.log", "admin reply log", 200)
	request(f.h.TicketReplyHandler, f.admin, "POST", fmt.Sprintf("/api/v1/admin/tickets/%d/messages", id), fmt.Sprintf(`{"body":"Reply","attachments":[{"file_id":%q}]}`, adminFile.ID), 200)
	request(f.h.FileGetHandler, f.token, "GET", adminFile.URL(), "", 200)
	request(f.h.FileGetHandler, otherToken, "GET", adminFile.URL(), "", 403)
	request(f.h.TicketCloseHandler, f.token, "POST", fmt.Sprintf("/api/v1/tickets/%d/close", id), "", 200)
	unbound := upload(f.token, "ticket", "draft.txt", "unused draft", 200)
	request(f.h.TicketReplyHandler, f.token, "POST", fmt.Sprintf("/api/v1/tickets/%d/messages", id), fmt.Sprintf(`{"body":"Late","attachments":[{"file_id":%q}]}`, unbound.ID), 400)
	request(f.h.FileDeleteHandler, f.token, "DELETE", unbound.URL(), "", 200)
	request(f.h.FileDeleteHandler, f.token, "DELETE", unbound.URL(), "", 200)
	request(f.h.FileGetHandler, f.token, "GET", unbound.URL(), "", 404)
	if _, err := os.Stat(filepath.Join(directory, unbound.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted file remained on disk", err)
	}
	if err := f.h.db.Model(&other).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	request(f.h.FileGetHandler, otherToken, "GET", file.URL(), "", 401)
}

func TestSiteImageUploadAndSettingsKeepPublicAndPrivateFilesSeparate(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example"}).Error; err != nil {
		t.Fatal(err)
	}
	f.h.services.ConfigureFileStorage(t.TempDir())
	logo := `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h10v10"/></svg>`
	makeUpload := func(token string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "logo.svg")
		part.Write([]byte(logo))
		writer.WriteField("purpose", "site")
		writer.Close()
		r := httptest.NewRequest("POST", "/api/v1/files", &body)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		f.h.FileUploadHandler(w, r)
		return w
	}
	if w := makeUpload(f.token); w.Code != 403 {
		t.Fatal("non-admin uploaded public asset", w.Code)
	}
	w := makeUpload(f.admin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var data struct{ Data struct{ ID, URL string } }
	json.Unmarshal(w.Body.Bytes(), &data)
	get := httptest.NewRecorder()
	f.h.FileGetHandler(get, httptest.NewRequest("GET", data.Data.URL, nil))
	if get.Code != 200 || get.Body.String() != logo || get.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(get.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("public image failed", get.Code, get.Header())
	}
	for _, key := range []string{"site_logo", "site_logo_dark", "site_favicon"} {
		if err := f.h.db.Where("config_key = ?", key).FirstOrCreate(&model.SystemConfig{ConfigKey: key, Name: key, ValueType: "string", IsPublic: true, Revision: 1}).Error; err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"value": data.Data.URL})
		update := httptest.NewRecorder()
		f.h.AdminSystemConfigUpdateHandler(update, announcementRequest("PUT", "/api/v1/admin/system-configs/"+key, f.admin, string(body)))
		if update.Code != 200 {
			t.Fatal("local image setting rejected", key, update.Code, update.Body.String())
		}
	}
	deleted := httptest.NewRecorder()
	f.h.FileDeleteHandler(deleted, announcementRequest("DELETE", "/api/v1/files/"+data.Data.ID, f.admin, ""))
	if deleted.Code != 409 {
		t.Fatal("published logo could be deleted", deleted.Code)
	}
	bad := httptest.NewRecorder()
	f.h.AdminSystemConfigUpdateHandler(bad, announcementRequest("PUT", "/api/v1/admin/system-configs/site_logo", f.admin, `{"value":"/media/11111111-2222-3333-4444-555555555555"}`))
	if bad.Code != 400 {
		t.Fatal("missing asset reference accepted", bad.Code)
	}
}
