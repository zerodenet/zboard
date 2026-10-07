package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func (h *handlers) offlineKernelStore() zeroadapter.OfflineArtifactStore {
	root := ""
	if strings.TrimSpace(h.zeroArtifactDir) != "" {
		root = filepath.Join(h.zeroArtifactDir, "kernel-uploads")
	}
	return zeroadapter.OfflineArtifactStore{Root: root}
}

type offlineKernelPreparer struct {
	h  *handlers
	id string
}

func (p offlineKernelPreparer) PrepareKernelReconciliation(ctx context.Context, nodeID uint) (network.PreparedKernelReconciliation, error) {
	prepared, err := p.h.PrepareKernelReconciliation(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	result := prepared.(preparedHandlerKernelReconciliation)
	if result.node.SSHVerifiedAt == nil || result.node.SSHHostKeyFingerprint == "" {
		return nil, errors.New("verify node SSH identity before offline installation")
	}
	result.offlineID = p.id
	return result, nil
}

func (h *handlers) prepareOfflineKernelRelease(ctx context.Context, node model.Node, id, version string) (network.PreparedKernelRelease, error) {
	if !localZeroVersionPattern.MatchString(version) {
		return nil, errors.New("invalid offline kernel version")
	}
	binary, err := h.offlineKernelStore().Load(ctx, id)
	if err != nil {
		return nil, err
	}
	release := zeroRelease{Version: version, Tag: "v" + version, ArtifactURL: "local-upload:" + id, ArtifactSHA256: id, ArtifactSize: int64(len(binary)), OfflineID: id}
	return preparedHandlerKernelRelease{h: h, node: node, release: release}, nil
}

// NodeKernelUploadHandler persists the immutable local binary before accepting
// the same durable reconciliation task used by online installations.
func (h *handlers) NodeKernelUploadHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if node.SSHVerifiedAt == nil || node.SSHHostKeyFingerprint == "" {
		BadRequest(w, "verify node SSH identity before offline installation")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, zeroadapter.OfflineArtifactMaxBytes+(64<<10))
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeJSON(w, http.StatusRequestEntityTooLarge, "kernel upload exceeds 128 MiB", nil)
		} else {
			BadRequest(w, "invalid kernel upload")
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	for name, values := range r.MultipartForm.Value {
		if (name != "version" && name != "allow_downgrade" && name != "idempotency_key") || len(values) != 1 {
			BadRequest(w, "invalid upload fields")
			return
		}
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		BadRequest(w, "select exactly one kernel file")
		return
	}
	version := strings.TrimPrefix(strings.TrimSpace(r.PostForm.Get("version")), "v")
	if !localZeroVersionPattern.MatchString(version) {
		BadRequest(w, "a valid exact kernel version is required")
		return
	}
	downgrade := r.PostForm.Get("allow_downgrade")
	if downgrade != "" && downgrade != "true" && downgrade != "false" {
		BadRequest(w, "invalid allow_downgrade")
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("idempotency_key"))
	if len(key) > 128 {
		BadRequest(w, "idempotency_key is too long")
		return
	}
	file, err := r.MultipartForm.File["file"][0].Open()
	if err != nil {
		BadRequest(w, "cannot read kernel upload")
		return
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, zeroadapter.OfflineArtifactMaxBytes+1))
	if err != nil {
		BadRequest(w, "cannot read kernel upload")
		return
	}
	artifactID, size, err := h.offlineKernelStore().Import(r.Context(), payload)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email, KernelVersion: version, KernelArtifactID: artifactID, AllowDowngrade: downgrade == "true"}
	scope := map[string]interface{}{"node_ids": []uint{node.ID}, "kernel_version": version, "kernel_source": "local_upload", "kernel_sha256": artifactID, "kernel_size": size}
	task, err := h.createOperationTask(r.Context(), claims, taskTypeNodeReconcile, scope, content, "node", []uint{node.ID}, key)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "offline kernel task accepted", task)
}
