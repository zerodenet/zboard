package experiencestore

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type experienceQueryCounter struct {
	logger.Interface
	count atomic.Int64
}

func (l *experienceQueryCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.count.Add(1)
	l.Interface.Trace(ctx, begin, fc, err)
}

func experienceFixture(t *testing.T) (*gorm.DB, model.User, model.User, model.User) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "experience.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "experience-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	owner := model.User{Email: "ticket-owner@example.test", Password: "unused", Status: "active"}
	other := model.User{Email: "ticket-other@example.test", Password: "unused", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	return db, admin, owner, other
}

func TestAnnouncementsEnforceCurrentAuthorityRevisionAndAuditRollback(t *testing.T) {
	db, admin, owner, _ := experienceFixture(t)
	store := Announcements{DB: db}
	ctx, now := context.Background(), time.Now().UTC()
	created, err := store.SaveAnnouncement(ctx, admin.ID, experience.AnnouncementChange{Announcement: experience.Announcement{Title: "Notice", Content: "Body", Severity: "info", Audience: "user", Status: "published", StartsAt: &now}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acknowledge(ctx, owner.ID, created.ID, created.Revision+1, now); !errors.Is(err, experience.ErrConflict) {
		t.Fatalf("stale receipt error = %v", err)
	}
	if receipt, err := store.Acknowledge(ctx, owner.ID, created.ID, created.Revision, now); err != nil || receipt.UserID != owner.ID {
		t.Fatalf("receipt = %+v err=%v", receipt, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	expected := created.Revision
	created.Title = "Changed"
	if _, err := store.SaveAnnouncement(ctx, admin.ID, experience.AnnouncementChange{Announcement: created, Expected: &expected}, now); !errors.Is(err, experience.ErrPermission) {
		t.Fatalf("revoked admin error = %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_announcement_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'announcement.update' BEGIN SELECT RAISE(ABORT, 'audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveAnnouncement(ctx, admin.ID, experience.AnnouncementChange{Announcement: created, Expected: &expected}, now); err == nil {
		t.Fatal("audit failure accepted")
	}
	var persisted model.Announcement
	if err := db.First(&persisted, created.ID).Error; err != nil || persisted.Title != "Notice" || persisted.Revision != 1 {
		t.Fatalf("announcement rollback = %+v err=%v", persisted, err)
	}
}

func TestTicketsEnforceOwnershipClosedStateAndAtomicAdminAudit(t *testing.T) {
	db, admin, owner, other := experienceFixture(t)
	store := Tickets{DB: db}
	ctx, now := context.Background(), time.Now().UTC()
	id, err := store.CreateTicket(ctx, owner.ID, experience.NewTicket{TicketNo: "T20260916-TEST0001", Subject: "Help", Category: "other", Priority: 1, Body: "Initial"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplyTicket(ctx, experience.TicketActor{ID: other.ID}, id, "intrusion", now); !errors.Is(err, experience.ErrTicketForbidden) {
		t.Fatalf("other user reply error = %v", err)
	}
	if err := store.ChangeTicketStatus(ctx, experience.TicketActor{ID: owner.ID}, id, "resolved", true, now); !errors.Is(err, experience.ErrTicketForbidden) {
		t.Fatalf("user admin override error = %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_ticket_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'ticket.status.update' BEGIN SELECT RAISE(ABORT, 'audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeTicketStatus(ctx, experience.TicketActor{ID: admin.ID, IsAdmin: true}, id, "resolved", true, now); err == nil {
		t.Fatal("audit failure accepted")
	}
	var ticket model.Ticket
	if err := db.First(&ticket, id).Error; err != nil || ticket.Status != "open" {
		t.Fatalf("ticket rollback = %+v err=%v", ticket, err)
	}
	var statusEvents int64
	if err := db.Model(&model.TicketMessage{}).Where("ticket_id = ? AND message_type = ?", id, "status").Count(&statusEvents).Error; err != nil || statusEvents != 0 {
		t.Fatalf("status events = %d err=%v", statusEvents, err)
	}
	if err := db.Exec("DROP TRIGGER fail_ticket_audit").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeTicketStatus(ctx, experience.TicketActor{ID: owner.ID}, id, "closed", false, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplyTicket(ctx, experience.TicketActor{ID: owner.ID}, id, "late", now); !errors.Is(err, experience.ErrTicketClosed) {
		t.Fatalf("closed reply error = %v", err)
	}
}

func TestTicketReadModelsUseTwoQueries(t *testing.T) {
	db, _, owner, _ := experienceFixture(t)
	now := time.Now().UTC()
	id, err := (Tickets{DB: db}).CreateTicket(context.Background(), owner.ID, experience.NewTicket{TicketNo: "T20260917-READ0001", Subject: "Read", Category: "other", Priority: 1, Body: "Initial"}, now)
	if err != nil {
		t.Fatal(err)
	}
	counter := &experienceQueryCounter{Interface: db.Config.Logger}
	store := Tickets{DB: db.Session(&gorm.Session{Logger: counter})}
	page, err := store.ListTickets(context.Background(), experience.TicketQuery{ActorID: owner.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if counter.count.Load() != 2 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].MessageCount != 1 {
		t.Fatalf("list queries=%d page=%+v", counter.count.Load(), page)
	}
	counter.count.Store(0)
	detail, err := store.TicketDetail(context.Background(), experience.TicketActor{ID: owner.ID}, id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if counter.count.Load() != 2 || detail.Ticket.ID != id || len(detail.Messages) != 1 {
		t.Fatalf("detail queries=%d detail=%+v", counter.count.Load(), detail)
	}
}
