package experience

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrPermission        = errors.New("experience operation requires a current principal")
	ErrNotFound          = errors.New("experience record not found")
	ErrConflict          = errors.New("experience record revision conflict")
	ErrAnnouncementState = errors.New("announcement state transition rejected")
	ErrInvalid           = errors.New("invalid experience input")
)

type Announcement struct {
	ID           uint       `json:"id"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Severity     string     `json:"severity"`
	Audience     string     `json:"audience"`
	Status       string     `json:"status"`
	PopupEnabled bool       `json:"popup_enabled"`
	Dismissible  bool       `json:"dismissible"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	CreatedBy    uint       `json:"created_by"`
	Revision     uint64     `json:"revision"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type AnnouncementChange struct {
	Announcement
	PopupEnabled *bool
	Dismissible  *bool
	Expected     *uint64
}

type AnnouncementReceipt struct {
	AnnouncementID uint      `json:"announcement_id"`
	UserID         uint      `json:"user_id"`
	Revision       uint64    `json:"revision"`
	ReadAt         time.Time `json:"read_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AnnouncementReadItem struct {
	Announcement Announcement
	Read         bool
	ReadAt       *time.Time
	Active       bool
}

type AnnouncementPage struct {
	Items  []AnnouncementReadItem
	Total  int64
	Unread int64
}

type AnnouncementAudienceQuery struct {
	UserID    uint
	Audiences []string
	Now       time.Time
	Offset    int
	Limit     int
}

type AnnouncementAdminPage struct {
	Items []Announcement
	Total int64
}

type AnnouncementRepository interface {
	Acknowledge(context.Context, uint, uint, uint64, time.Time) (AnnouncementReceipt, error)
	SaveAnnouncement(context.Context, uint, AnnouncementChange, time.Time) (Announcement, error)
	DeleteAnnouncement(context.Context, uint, uint) error
	ActiveAnnouncements(context.Context, AnnouncementAudienceQuery) ([]AnnouncementReadItem, error)
	AnnouncementHistory(context.Context, AnnouncementAudienceQuery) (AnnouncementPage, error)
	AnnouncementUnreadCount(context.Context, AnnouncementAudienceQuery) (int64, error)
	AdminAnnouncements(context.Context, string, int, int) (AnnouncementAdminPage, error)
}

type Announcements struct{ Repository AnnouncementRepository }

func (s Announcements) Acknowledge(ctx context.Context, actor, id uint, revision uint64, now time.Time) (AnnouncementReceipt, error) {
	if actor == 0 {
		return AnnouncementReceipt{}, ErrPermission
	}
	if id == 0 || revision == 0 {
		return AnnouncementReceipt{}, ErrNotFound
	}
	return s.Repository.Acknowledge(ctx, actor, id, revision, now.UTC())
}

func (s Announcements) Save(ctx context.Context, actor uint, change AnnouncementChange, now time.Time) (Announcement, error) {
	if actor == 0 {
		return Announcement{}, ErrPermission
	}
	change.Title = strings.TrimSpace(change.Title)
	change.Content = strings.TrimSpace(change.Content)
	change.Severity = strings.ToLower(strings.TrimSpace(change.Severity))
	change.Audience = strings.ToLower(strings.TrimSpace(change.Audience))
	change.Status = strings.ToLower(strings.TrimSpace(change.Status))
	if change.Severity == "" {
		change.Severity = "info"
	}
	if change.Audience == "" {
		change.Audience = "all"
	}
	if change.Status == "" {
		change.Status = "draft"
	}
	if change.Title == "" || len(change.Title) > 160 {
		return Announcement{}, fmt.Errorf("%w: title must contain 1 to 160 bytes", ErrInvalid)
	}
	if change.Content == "" || len(change.Content) > 16*1024 {
		return Announcement{}, fmt.Errorf("%w: content must contain 1 to 16384 bytes", ErrInvalid)
	}
	if !oneOf(change.Severity, "info", "success", "warning", "critical") || !oneOf(change.Audience, "all", "guest", "user", "admin") || !oneOf(change.Status, "draft", "published", "archived") {
		return Announcement{}, ErrInvalid
	}
	if change.StartsAt != nil && change.EndsAt != nil && !change.StartsAt.Before(*change.EndsAt) {
		return Announcement{}, fmt.Errorf("%w: ends_at must be after starts_at", ErrInvalid)
	}
	if change.ID == 0 && change.Status == "archived" {
		return Announcement{}, ErrAnnouncementState
	}
	if change.Status == "published" && change.StartsAt == nil {
		start := now.UTC()
		change.StartsAt = &start
	}
	return s.Repository.SaveAnnouncement(ctx, actor, change, now.UTC())
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s Announcements) Delete(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrPermission
	}
	return s.Repository.DeleteAnnouncement(ctx, actor, id)
}

func (s Announcements) Active(ctx context.Context, query AnnouncementAudienceQuery) ([]AnnouncementReadItem, error) {
	query.Now = query.Now.UTC()
	return s.Repository.ActiveAnnouncements(ctx, query)
}

func (s Announcements) History(ctx context.Context, query AnnouncementAudienceQuery) (AnnouncementPage, error) {
	query.Now = query.Now.UTC()
	return s.Repository.AnnouncementHistory(ctx, query)
}

func (s Announcements) UnreadCount(ctx context.Context, query AnnouncementAudienceQuery) (int64, error) {
	if query.UserID == 0 {
		return 0, nil
	}
	query.Now = query.Now.UTC()
	return s.Repository.AnnouncementUnreadCount(ctx, query)
}

func (s Announcements) AdminList(ctx context.Context, status string, offset, limit int) (AnnouncementAdminPage, error) {
	return s.Repository.AdminAnnouncements(ctx, status, offset, limit)
}
