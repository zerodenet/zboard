package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRegistrationStatusPagesPendingEventsAndRechecksAdministrator(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	admin := model.User{Email: "registration-status@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	rows := []model.AccountRegistrationEvent{
		{AccountID: 100, OccurredAt: now.Add(-time.Hour)},
		{AccountID: 101, OccurredAt: now.Add(-2 * time.Hour)},
		{AccountID: 102, OccurredAt: now.Add(-3 * time.Hour), ProcessedAt: &now},
	}
	if err := h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.RegistrationEventStatus()
	summary, err := service.Summary(context.Background(), admin.ID)
	if err != nil || summary.Pending != 2 || len(summary.Items) != 0 || summary.OldestAt == nil || !summary.OldestAt.Equal(rows[1].OccurredAt) {
		t.Fatal(summary, err)
	}
	page, err := service.Pending(context.Background(), admin.ID, 1, 1)
	if err != nil || page.Pending != 2 || len(page.Items) != 1 || page.Items[0].AccountID != 101 || page.OldestAt == nil || !page.OldestAt.Equal(rows[1].OccurredAt) {
		t.Fatal(page, err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Pending(context.Background(), admin.ID, 1, 0); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator read events", err)
	}
	if _, err := service.Summary(context.Background(), admin.ID); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator read summary", err)
	}
}
