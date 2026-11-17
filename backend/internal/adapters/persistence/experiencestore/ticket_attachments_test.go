package experiencestore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/logger"
)

func TestTicketAttachmentFanoutKeepsMessagePaginationAndQueryBudget(t *testing.T) {
	db, _, owner, _ := experienceFixture(t)
	now := time.Now().UTC()
	ticket := model.Ticket{TicketNo: "ATTACHMENT-PAGING", UserID: owner.ID, Subject: "Paging", Category: "other", Priority: 1, Status: "open", LastMessageAt: now}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 24; index++ {
		message := model.TicketMessage{TicketID: ticket.ID, AuthorID: &owner.ID, AuthorRole: "user", Type: "message", Body: fmt.Sprint(index), CreatedAt: now.Add(time.Duration(index) * time.Second)}
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
		items := make([]experience.TicketAttachment, 5)
		for attachment := range items {
			items[attachment] = experience.TicketAttachment{Name: fmt.Sprint(attachment), URL: fmt.Sprintf("https://files.example/%d/%d", index, attachment)}
		}
		if err := saveTicketAttachments(db, owner.ID, message.ID, items); err != nil {
			t.Fatal(err)
		}
	}
	counter := &experienceQueryCounter{Interface: logger.Discard}
	db.Logger = counter
	store := Tickets{DB: db}
	page, err := store.TicketDetail(context.Background(), experience.TicketActor{ID: owner.ID}, ticket.ID, 0, 20)
	if err != nil || len(page.Messages) != 20 || !page.HasOlderMessages || counter.count.Load() != 2 {
		t.Fatalf("pagination/query budget changed: %+v queries=%d err=%v", page, counter.count.Load(), err)
	}
	for _, message := range page.Messages {
		if len(message.Attachments) != 5 {
			t.Fatal("attachment fanout truncated", message)
		}
	}
	older, err := store.TicketDetail(context.Background(), experience.TicketActor{ID: owner.ID}, ticket.ID, page.OldestMessageID, 20)
	if err != nil || len(older.Messages) != 4 || older.HasOlderMessages {
		t.Fatalf("older page: %+v %v", older, err)
	}
}
