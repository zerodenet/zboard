package handler

import (
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
)

func (h *handlers) FileUploadHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	if h.fileUploadSlots != nil {
		select {
		case h.fileUploadSlots <- struct{}{}:
			defer func() { <-h.fileUploadSlots }()
		default:
			w.Header().Set("Retry-After", "5")
			ServiceUnavailable(w, "上传繁忙，请稍后重试。")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, experience.MaxUploadBytes+(64<<10))
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		BadRequest(w, "上传失败：文件不得超过 5 MiB。")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["file"]) != 1 {
		BadRequest(w, "每次上传一个文件。")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		BadRequest(w, "请选择文件。")
		return
	}
	defer file.Close()
	saved, err := h.services.Files.Upload(r.Context(), experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}, r.FormValue("purpose"), header.Filename, file, time.Now())
	if handleFileError(w, err) {
		return
	}
	OK(w, struct {
		experience.StoredFile
		URL string `json:"url"`
	}{saved, saved.URL()})
}

func (h *handlers) FileGetHandler(w http.ResponseWriter, r *http.Request) {
	publicOnly := strings.HasPrefix(r.URL.Path, "/media/")
	actor := experience.TicketActor{}
	if !publicOnly {
		claims, err := h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
		actor = experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}
	}
	id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	file, blob, err := h.services.Files.Read(r.Context(), actor, id, publicOnly)
	if handleFileError(w, err) {
		return
	}
	defer blob.Close()
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if file.Purpose == "site" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	}
	http.ServeContent(w, r, file.Name, file.CreatedAt, blob)
}

func (h *handlers) FileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	err = h.services.Files.Delete(r.Context(), experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}, id, time.Now())
	if handleFileError(w, err) {
		return
	}
	OK(w, map[string]bool{"deleted": true})
}

func handleFileError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, experience.ErrNotFound):
		NotFound(w)
	case errors.Is(err, experience.ErrPermission), errors.Is(err, experience.ErrTicketForbidden):
		Forbidden(w, "无权访问此文件。")
	case errors.Is(err, experience.ErrInvalid):
		BadRequest(w, err.Error())
	case errors.Is(err, experience.ErrFileInUse):
		writeJSON(w, http.StatusConflict, "已使用的文件不能删除。", nil)
	default:
		ServerError(w, err)
	}
	return true
}
