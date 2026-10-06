package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func offlineUploadFixture(t *testing.T) (*handlers, string, model.Node) {
	h, token := newManagedRuleCreateFixture(t)
	encrypted, err := h.credentialCipher.Encrypt("password")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	node := model.Node{Name: "offline", Config: "{}", SSHHost: "127.0.0.1", SSHPort: 1, SSHUser: "root", SSHAuthMethod: "password", SSHPwd: encrypted, SSHPrivilegeMode: "none", SSHHostKeyFingerprint: "SHA256:" + strings.Repeat("a", 43), SSHVerifiedAt: &now}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	return h, token, node
}
func uploadTestELF() []byte {
	data := make([]byte, 128)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], 62)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint64(data[32:], 64)
	binary.LittleEndian.PutUint16(data[52:], 64)
	binary.LittleEndian.PutUint16(data[54:], 56)
	binary.LittleEndian.PutUint16(data[56:], 1)
	binary.LittleEndian.PutUint32(data[64:], 1)
	binary.LittleEndian.PutUint32(data[68:], 5)
	binary.LittleEndian.PutUint64(data[72:], 120)
	binary.LittleEndian.PutUint64(data[96:], 8)
	return data
}
func kernelUploadRequest(t *testing.T, token, version string, payload []byte) *http.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for name, value := range map[string]string{"version": version, "idempotency_key": "offline-test"} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "zero")
	if err != nil {
		t.Fatal(err)
	}
	file.Write(payload)
	writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/1/kernel/upload", &buffer)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}
func TestOfflineKernelUploadQueuesImmutableTaskWithoutGitHub(t *testing.T) {
	h, token, node := offlineUploadFixture(t)
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("offline path attempted HTTP: %s", request.URL.Host)
	})
	defer func() { http.DefaultTransport = original }()

	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		request := kernelUploadRequest(t, token, "0.0.3-dev.1", uploadTestELF())
		if attempt == 1 {
			request.URL.RawQuery = "version=9.9.9&allow_downgrade=true"
		}
		h.NodeKernelUploadHandler(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	var tasks []model.Task
	if err := h.db.Where("type = ?", taskTypeNodeReconcile).Find(&tasks).Error; err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	var content operationTaskContent
	if err := json.Unmarshal([]byte(tasks[0].Content), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.KernelArtifactID) != 64 || content.KernelVersion != "0.0.3-dev.1" || !strings.Contains(tasks[0].Scope, "local_upload") {
		t.Fatalf("offline intent missing: %+v", content)
	}
	prepared, err := (offlineKernelPreparer{h: h, id: content.KernelArtifactID}).PrepareKernelReconciliation(context.Background(), node.ID)
	if err != nil {
		t.Fatal(err)
	}
	release, err := prepared.ResolveRelease(context.Background(), network.KernelProbe{Architecture: "x86_64", Systemd: true}, content.KernelVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(release.Descriptor().ArtifactURL, "local-upload:") {
		t.Fatal("offline resolver selected online release")
	}
	activation := preparedHandlerKernelActivation{h: h, release: release.(preparedHandlerKernelRelease).release}
	materialized, err := activation.Materialize(context.Background())
	if err != nil || materialized.BinarySHA256() != content.KernelArtifactID {
		t.Fatalf("offline materialization: %v", err)
	}
	var audits int64
	h.db.Model(&model.AuditLog{}).Where("action = ?", "task.create").Count(&audits)
	if audits != 1 {
		t.Fatalf("task audit duplicated: %d", audits)
	}
}
func TestOfflineKernelUploadRejectsUnverifiedNodesAndInvalidFiles(t *testing.T) {
	h, token, node := offlineUploadFixture(t)
	for _, scenario := range []struct {
		version string
		data    []byte
	}{{"latest", uploadTestELF()}, {"0.0.3", []byte("not a kernel")}} {
		recorder := httptest.NewRecorder()
		h.NodeKernelUploadHandler(recorder, kernelUploadRequest(t, token, scenario.version, scenario.data))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid file accepted: %d", recorder.Code)
		}
	}
	if err := h.db.Model(&model.Node{}).Where("id = ?", node.ID).Update("ssh_verified_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	h.NodeKernelUploadHandler(recorder, kernelUploadRequest(t, token, "0.0.3", uploadTestELF()))
	if recorder.Code != http.StatusBadRequest {
		t.Fatal("unverified SSH accepted")
	}
	if _, err := (offlineKernelPreparer{h: h, id: strings.Repeat("a", 64)}).PrepareKernelReconciliation(context.Background(), node.ID); err == nil {
		t.Fatal("task bypassed SSH verification")
	}
	var tasks int64
	h.db.Model(&model.Task{}).Count(&tasks)
	if tasks != 0 {
		t.Fatal("invalid upload enqueued a task")
	}
}
func TestOfflineKernelUploadRequiresCurrentAdministrator(t *testing.T) {
	h, token, _ := offlineUploadFixture(t)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	h.NodeKernelUploadHandler(recorder, kernelUploadRequest(t, token, "0.0.3", uploadTestELF()))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("revoked admin accepted: %d %s", recorder.Code, recorder.Body.String())
	}
}
