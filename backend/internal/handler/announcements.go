package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxActiveAnnouncements = 10

type announcementWriteRequest struct {
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	Severity         string     `json:"severity"`
	Audience         string     `json:"audience"`
	Status           string     `json:"status"`
	Dismissible      *bool      `json:"dismissible"`
	StartsAt         *time.Time `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at"`
	ExpectedRevision *uint64    `json:"expected_revision"`
}

type publicAnnouncement struct {
	ID          uint       `json:"id"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Severity    string     `json:"severity"`
	Dismissible bool       `json:"dismissible"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Revision    uint64     `json:"revision"`
	UpdatedAt   time.Time  `json:"updated_at"`
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

func announcementAudience(r *http.Request, h *handlers) []string {
	claims, err := h.authFromRequest(r)
	if err != nil {
		return []string{"all", "guest"}
	}
	if claims.IsAdmin {
		return []string{"all", "admin"}
	}
	return []string{"all", "user"}
}

func (h *handlers) activeAnnouncements(r *http.Request) ([]publicAnnouncement, error) {
	now := time.Now().UTC()
	var records []model.Announcement
	if err := h.db.WithContext(r.Context()).
		Where("status = ?", "published").
		Where("audience IN ?", announcementAudience(r, h)).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at > ?)", now, now).
		Order("severity = 'critical' DESC, starts_at DESC, id DESC").
		Limit(maxActiveAnnouncements).
		Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]publicAnnouncement, 0, len(records))
	for _, record := range records {
		result = append(result, publicAnnouncement{
			ID: record.ID, Title: record.Title, Content: record.Content, Severity: record.Severity,
			Dismissible: record.Dismissible, StartsAt: record.StartsAt, EndsAt: record.EndsAt,
			Revision: record.Revision, UpdatedAt: record.UpdatedAt,
		})
	}
	return result, nil
}

func (h *handlers) PublicAnnouncementsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := h.activeAnnouncements(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, items)
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
	query := h.db.WithContext(r.Context()).Model(&model.Announcement{})
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	var items []model.Announcement
	if err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
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
	dismissible := true
	if req.Dismissible != nil {
		dismissible = *req.Dismissible
	}
	record := model.Announcement{
		Title: req.Title, Content: req.Content, Severity: req.Severity, Audience: req.Audience,
		Status: req.Status, Dismissible: dismissible, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
		CreatedBy: claims.UserID, Revision: 1,
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "announcement.create", fmt.Sprintf("announcement:%d", record.ID), "status="+record.Status)
	}); err != nil {
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
	var record model.Announcement
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
			return err
		}
		if record.Revision != *req.ExpectedRevision {
			return errConfigRevisionConflict
		}
		record.Title, record.Content, record.Severity, record.Audience, record.Status = req.Title, req.Content, req.Severity, req.Audience, req.Status
		record.StartsAt, record.EndsAt = req.StartsAt, req.EndsAt
		if req.Dismissible != nil {
			record.Dismissible = *req.Dismissible
		}
		record.Revision++
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "announcement.update", fmt.Sprintf("announcement:%d", record.ID), "status="+record.Status)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errConfigRevisionConflict) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
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
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var record model.Announcement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
			return err
		}
		if record.Status != "draft" {
			return errors.New("only draft announcements can be deleted; archive published announcements instead")
		}
		if err := tx.Delete(&record).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "announcement.delete", fmt.Sprintf("announcement:%d", record.ID), "draft deleted")
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, map[string]uint{"id": id})
}
