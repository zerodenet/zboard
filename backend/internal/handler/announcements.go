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

func announcementReadMap(db *gorm.DB, userID uint, records []model.Announcement) (map[uint]model.AnnouncementRead, error) {
	reads := make(map[uint]model.AnnouncementRead)
	if userID == 0 || len(records) == 0 {
		return reads, nil
	}
	ids := make([]uint, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	var receipts []model.AnnouncementRead
	if err := db.Where("user_id = ? AND announcement_id IN ?", userID, ids).Find(&receipts).Error; err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		reads[receipt.AnnouncementID] = receipt
	}
	return reads, nil
}

func announcementPublicView(record model.Announcement, receipt model.AnnouncementRead) publicAnnouncement {
	read := receipt.Revision >= record.Revision
	var readAt *time.Time
	if read {
		value := receipt.ReadAt
		readAt = &value
	}
	return publicAnnouncement{
		ID: record.ID, Title: record.Title, Content: record.Content, Severity: record.Severity,
		PopupEnabled: record.PopupEnabled, Dismissible: record.Dismissible, StartsAt: record.StartsAt, EndsAt: record.EndsAt,
		Revision: record.Revision, Read: read, ReadAt: readAt, UpdatedAt: record.UpdatedAt,
	}
}

func (h *handlers) activeAnnouncements(r *http.Request) ([]publicAnnouncement, error) {
	now := time.Now().UTC()
	audiences, userID := announcementViewer(r, h)
	var records []model.Announcement
	if err := h.db.WithContext(r.Context()).
		Where("status = ?", "published").
		Where("audience IN ?", audiences).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at > ?)", now, now).
		Order("popup_enabled DESC, COALESCE(starts_at, created_at) DESC, id DESC").
		Limit(maxActiveAnnouncements).
		Find(&records).Error; err != nil {
		return nil, err
	}
	reads, err := announcementReadMap(h.db.WithContext(r.Context()), userID, records)
	if err != nil {
		return nil, err
	}
	result := make([]publicAnnouncement, 0, len(records))
	for _, record := range records {
		result = append(result, announcementPublicView(record, reads[record.ID]))
	}
	return result, nil
}

func (h *handlers) announcementUnreadCount(r *http.Request) (int64, error) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		return 0, nil
	}
	now := time.Now().UTC()
	var unread int64
	err = h.db.WithContext(r.Context()).Model(&model.Announcement{}).
		Where("status = ?", "published").
		Where("audience IN ?", announcementAudiencesForClaims(claims)).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at > ?)", now, now).
		Where(`NOT EXISTS (
			SELECT 1 FROM announcement_reads
			WHERE announcement_reads.announcement_id = announcements.id
			  AND announcement_reads.user_id = ?
			  AND announcement_reads.revision >= announcements.revision
		)`, claims.UserID).
		Count(&unread).Error
	return unread, err
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
	historyQuery := func() *gorm.DB {
		return h.db.WithContext(r.Context()).Model(&model.Announcement{}).
			Where("status = ?", "published").
			Where("audience IN ?", announcementAudiencesForClaims(claims)).
			Where("starts_at IS NULL OR starts_at <= ?", now)
	}
	var total, unread int64
	if err := historyQuery().Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := historyQuery().Where("ends_at IS NULL OR ends_at > ?", now).Where(`NOT EXISTS (
		SELECT 1 FROM announcement_reads
		WHERE announcement_reads.announcement_id = announcements.id
		  AND announcement_reads.user_id = ?
		  AND announcement_reads.revision >= announcements.revision
	)`, claims.UserID).Count(&unread).Error; err != nil {
		ServerError(w, err)
		return
	}
	var records []model.Announcement
	if err := historyQuery().
		Order(clause.Expr{SQL: "CASE WHEN ends_at IS NULL OR ends_at > ? THEN 0 ELSE 1 END", Vars: []interface{}{now}, WithoutParentheses: true}).
		Order("popup_enabled DESC").
		Order("CASE severity WHEN 'critical' THEN 4 WHEN 'warning' THEN 3 WHEN 'success' THEN 2 ELSE 1 END DESC").
		Order("starts_at DESC, id DESC").
		Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		ServerError(w, err)
		return
	}
	reads, err := announcementReadMap(h.db.WithContext(r.Context()), claims.UserID, records)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]accountAnnouncement, 0, len(records))
	for _, record := range records {
		active := record.EndsAt == nil || record.EndsAt.After(now)
		items = append(items, accountAnnouncement{
			publicAnnouncement: announcementPublicView(record, reads[record.ID]),
			Audience:           record.Audience, Status: record.Status, Active: active, CreatedAt: record.CreatedAt,
		})
	}
	data := pagedData(items, total, offset, limit)
	data["unread_count"] = unread
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
	var receipt model.AnnouncementRead
	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var record model.Announcement
		if err := tx.First(&record, id).Error; err != nil {
			return err
		}
		if record.Status != "published" {
			return gorm.ErrRecordNotFound
		}
		if record.StartsAt != nil && record.StartsAt.After(time.Now().UTC()) {
			return gorm.ErrRecordNotFound
		}
		if !containsString(announcementAudiencesForClaims(claims), record.Audience) {
			return gorm.ErrRecordNotFound
		}
		if record.Revision != req.Revision {
			return errConfigRevisionConflict
		}
		now := time.Now().UTC()
		receipt = model.AnnouncementRead{AnnouncementID: record.ID, UserID: claims.UserID, Revision: record.Revision, ReadAt: now}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "announcement_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"revision", "read_at", "updated_at"}),
		}).Create(&receipt).Error
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
	record := model.Announcement{
		Title: req.Title, Content: req.Content, Severity: req.Severity, Audience: req.Audience,
		Status: req.Status, PopupEnabled: req.PopupEnabled != nil && *req.PopupEnabled, Dismissible: dismissible, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
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
		if record.Status == "archived" {
			return errors.New("archived announcements cannot be edited")
		}
		if record.Status == "draft" && req.Status == "archived" {
			return errors.New("a draft announcement cannot be archived before it is published")
		}
		if record.Status == "published" && req.Status == "draft" {
			return errors.New("a published announcement cannot return to draft")
		}
		if record.Status == "draft" && req.Status == "published" && req.StartsAt == nil {
			now := time.Now().UTC()
			req.StartsAt = &now
		}
		record.Title, record.Content, record.Severity, record.Audience, record.Status = req.Title, req.Content, req.Severity, req.Audience, req.Status
		record.StartsAt, record.EndsAt = req.StartsAt, req.EndsAt
		if req.PopupEnabled != nil {
			record.PopupEnabled = *req.PopupEnabled
		}
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
		if record.Status != "draft" && record.Status != "archived" {
			return errors.New("only draft or archived announcements can be deleted; archive published announcements first")
		}
		if err := tx.Where("announcement_id = ?", record.ID).Delete(&model.AnnouncementRead{}).Error; err != nil {
			return err
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
