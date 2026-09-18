package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
)

const maxActiveAnnouncements = 5

type announcementWriteRequest struct {
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	Severity         string     `json:"severity"`
	Audience         string     `json:"audience"`
	Status           string     `json:"status"`
	PopupEnabled     *bool      `json:"popup_enabled"`
	Dismissible      *bool      `json:"dismissible"`
	StartsAt         *time.Time `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at"`
	ExpectedRevision *uint64    `json:"expected_revision"`
}

type publicAnnouncement struct {
	ID           uint       `json:"id"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Severity     string     `json:"severity"`
	PopupEnabled bool       `json:"popup_enabled"`
	Dismissible  bool       `json:"dismissible"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	Revision     uint64     `json:"revision"`
	Read         bool       `json:"read"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type accountAnnouncement struct {
	publicAnnouncement
	Audience  string    `json:"audience"`
	Status    string    `json:"status"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type announcementReadRequest struct {
	Revision uint64 `json:"revision"`
}

func normalizeAnnouncementWrite(req *announcementWriteRequest) error {
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Severity = strings.ToLower(strings.TrimSpace(req.Severity))
	req.Audience = strings.ToLower(strings.TrimSpace(req.Audience))
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Severity == "" {
		req.Severity = "info"
	}
	if req.Audience == "" {
		req.Audience = "all"
	}
	if req.Status == "" {
		req.Status = "draft"
	}
	if req.Title == "" || len(req.Title) > 160 {
		return errors.New("title must contain 1 to 160 bytes")
	}
	if req.Content == "" || len(req.Content) > 16*1024 {
		return errors.New("content must contain 1 to 16384 bytes")
	}
	if !containsString([]string{"info", "success", "warning", "critical"}, req.Severity) {
		return errors.New("severity must be info, success, warning, or critical")
	}
	if !containsString([]string{"all", "guest", "user", "admin"}, req.Audience) {
		return errors.New("audience must be all, guest, user, or admin")
	}
	if !containsString([]string{"draft", "published", "archived"}, req.Status) {
		return errors.New("status must be draft, published, or archived")
	}
	if req.StartsAt != nil {
		value := req.StartsAt.UTC()
		req.StartsAt = &value
	}
	if req.EndsAt != nil {
		value := req.EndsAt.UTC()
		req.EndsAt = &value
	}
	if req.StartsAt != nil && req.EndsAt != nil && !req.StartsAt.Before(*req.EndsAt) {
		return errors.New("ends_at must be after starts_at")
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func announcementAudiencesForClaims(claims authClaims) []string {
	if claims.IsAdmin {
		return []string{"all", "admin"}
	}
	return []string{"all", "user"}
}

func announcementViewer(r *http.Request, h *handlers) ([]string, uint) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		return []string{"all", "guest"}, 0
	}
	return announcementAudiencesForClaims(claims), claims.UserID
}

func announcementPublicView(item experience.AnnouncementReadItem) publicAnnouncement {
	record := item.Announcement
	return publicAnnouncement{
		ID: record.ID, Title: record.Title, Content: record.Content, Severity: record.Severity,
		PopupEnabled: record.PopupEnabled, Dismissible: record.Dismissible, StartsAt: record.StartsAt, EndsAt: record.EndsAt,
		Revision: record.Revision, Read: item.Read, ReadAt: item.ReadAt, UpdatedAt: record.UpdatedAt,
	}
}

func (h *handlers) activeAnnouncements(r *http.Request) ([]publicAnnouncement, error) {
	now := time.Now().UTC()
	audiences, userID := announcementViewer(r, h)
	items, err := h.services.Announcements.Active(r.Context(), experience.AnnouncementAudienceQuery{UserID: userID, Audiences: audiences, Now: now, Limit: maxActiveAnnouncements})
	if err != nil {
		return nil, err
	}
	result := make([]publicAnnouncement, 0, len(items))
	for _, item := range items {
		result = append(result, announcementPublicView(item))
	}
	return result, nil
}

func (h *handlers) announcementUnreadCount(r *http.Request) (int64, error) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		return 0, nil
	}
	return h.services.Announcements.UnreadCount(r.Context(), experience.AnnouncementAudienceQuery{UserID: claims.UserID, Audiences: announcementAudiencesForClaims(claims), Now: time.Now().UTC()})
}

func (h *handlers) PublicAnnouncementsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := h.activeAnnouncements(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, items)
}

func (h *handlers) AccountAnnouncementsListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	now := time.Now().UTC()
	page, err := h.services.Announcements.History(r.Context(), experience.AnnouncementAudienceQuery{UserID: claims.UserID, Audiences: announcementAudiencesForClaims(claims), Now: now, Offset: offset, Limit: limit})
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]accountAnnouncement, 0, len(page.Items))
	for _, item := range page.Items {
		record := item.Announcement
		items = append(items, accountAnnouncement{
			publicAnnouncement: announcementPublicView(item),
			Audience:           record.Audience, Status: record.Status, Active: item.Active, CreatedAt: record.CreatedAt,
		})
	}
	data := pagedData(items, page.Total, offset, limit)
	data["unread_count"] = page.Unread
	OK(w, data)
}

func (h *handlers) AccountAnnouncementReadHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/account/announcements/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req announcementReadRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.Revision == 0 {
		BadRequest(w, "revision is required")
		return
	}
	receipt, err := h.services.Announcements.Acknowledge(r.Context(), claims.UserID, id, req.Revision, time.Now().UTC())
	if errors.Is(err, experience.ErrNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, experience.ErrConflict) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, receipt)
}

func (h *handlers) AdminAnnouncementsListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	page, err := h.services.Announcements.AdminList(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), offset, limit)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(page.Items, page.Total, offset, limit))
}

func (h *handlers) AdminAnnouncementCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req announcementWriteRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := normalizeAnnouncementWrite(&req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.Status == "archived" {
		BadRequest(w, "a new announcement cannot be archived before it is published")
		return
	}
	dismissible := true
	if req.Dismissible != nil {
		dismissible = *req.Dismissible
	}
	if req.Status == "published" && req.StartsAt == nil {
		now := time.Now().UTC()
		req.StartsAt = &now
	}
	record, err := h.services.Announcements.Save(r.Context(), claims.UserID, experience.AnnouncementChange{Announcement: experience.Announcement{
		Title: req.Title, Content: req.Content, Severity: req.Severity, Audience: req.Audience, Status: req.Status,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt,
	}, PopupEnabled: req.PopupEnabled, Dismissible: &dismissible}, time.Now().UTC())
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
		return
	}
	if errors.Is(err, experience.ErrAnnouncementState) {
		BadRequest(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, "announcement created", record)
}

func parseAnnouncementID(path string) (uint, error) {
	raw := strings.Trim(strings.TrimPrefix(path, "/api/v1/admin/announcements/"), "/")
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid announcement id")
	}
	return uint(value), nil
}

func (h *handlers) AdminAnnouncementUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseAnnouncementID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req announcementWriteRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.ExpectedRevision == nil {
		BadRequest(w, "expected_revision is required")
		return
	}
	if err := normalizeAnnouncementWrite(&req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	record, err := h.services.Announcements.Save(r.Context(), claims.UserID, experience.AnnouncementChange{Announcement: experience.Announcement{
		ID: id, Title: req.Title, Content: req.Content, Severity: req.Severity, Audience: req.Audience, Status: req.Status,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt,
	}, PopupEnabled: req.PopupEnabled, Dismissible: req.Dismissible, Expected: req.ExpectedRevision}, time.Now().UTC())
	if errors.Is(err, experience.ErrNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, experience.ErrConflict) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if errors.Is(err, experience.ErrAnnouncementState) {
		BadRequest(w, err.Error())
		return
	}
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, record)
}

func (h *handlers) AdminAnnouncementDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseAnnouncementID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	err = h.services.Announcements.Delete(r.Context(), claims.UserID, id)
	if errors.Is(err, experience.ErrNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, map[string]uint{"id": id})
}
