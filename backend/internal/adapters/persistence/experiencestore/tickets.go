package experiencestore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Tickets struct{ DB *gorm.DB }

func (s Tickets) CreateTicket(ctx context.Context, actor uint, input experience.NewTicket, now time.Time) (id uint, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := model.Ticket{TicketNo: input.TicketNo, UserID: actor, Subject: input.Subject, Category: input.Category, Priority: input.Priority, Status: "open", LastMessageAt: now}
		// Financial settlement/termination takes source orders before the owner.
		// Do not hold the user's SHARE lock while waiting for that source order.
		if input.References != nil {
			var err error
			row.OrderID, err = ownedTicketReference(tx, "orders", actor, input.References.OrderID)
			if err != nil {
				return err
			}
		}
		user, err := currentExperienceUser(tx, actor, false)
		if err != nil {
			return err
		}
		if err := bindTicketReferences(tx, &row, input.References); err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		message := model.TicketMessage{TicketID: row.ID, AuthorID: &user.ID, AuthorRole: "user", Type: "message", Body: input.Body, CreatedAt: now}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		if err := saveTicketAttachments(tx, user.ID, message.ID, input.Attachments); err != nil {
			return err
		}
		id = row.ID
		return nil
	})
	return id, err
}

func (s Tickets) ReplyTicket(ctx context.Context, actor experience.TicketActor, id uint, body string, now time.Time, attachments ...experience.TicketAttachment) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := currentExperienceUser(tx, actor.ID, actor.IsAdmin)
		if err != nil {
			return err
		}
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, id).Error; err != nil {
			return announcementNotFound(err)
		}
		if !actor.IsAdmin && ticket.UserID != user.ID {
			return experience.ErrTicketForbidden
		}
		if ticket.Status == "closed" {
			return experience.ErrTicketClosed
		}
		role, next := "user", "pending_admin"
		if actor.IsAdmin {
			role, next = "admin", "pending_user"
		}
		message := model.TicketMessage{TicketID: ticket.ID, AuthorID: &user.ID, AuthorRole: role, Type: "message", Body: body, CreatedAt: now}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		if err := saveTicketAttachments(tx, user.ID, message.ID, attachments); err != nil {
			return err
		}
		previous := ticket.Status
		if err := tx.Model(&ticket).Updates(map[string]any{"last_message_at": now, "status": next, "resolved_at": nil, "closed_at": nil}).Error; err != nil {
			return err
		}
		if previous != next {
			return createTicketStatusEvent(tx, ticket.ID, user.ID, role, previous, next, now)
		}
		return nil
	})
}

func (s Tickets) ChangeTicketStatus(ctx context.Context, actor experience.TicketActor, id uint, status string, adminOverride bool, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := currentExperienceUser(tx, actor.ID, actor.IsAdmin)
		if err != nil {
			return err
		}
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, id).Error; err != nil {
			return announcementNotFound(err)
		}
		if !actor.IsAdmin && ticket.UserID != user.ID {
			return experience.ErrTicketForbidden
		}
		if adminOverride && !actor.IsAdmin {
			return experience.ErrTicketForbidden
		}
		if !adminOverride && status != "closed" {
			return experience.ErrTicketForbidden
		}
		if ticket.Status != status {
			previous := ticket.Status
			updates := map[string]any{"status": status, "last_message_at": now}
			switch status {
			case "resolved":
				updates["resolved_at"], updates["closed_at"] = now, nil
			case "closed":
				updates["closed_at"] = now
			default:
				updates["resolved_at"], updates["closed_at"] = nil, nil
			}
			if err := tx.Model(&ticket).Updates(updates).Error; err != nil {
				return err
			}
			role := "user"
			if actor.IsAdmin {
				role = "admin"
			}
			if err := createTicketStatusEvent(tx, ticket.ID, user.ID, role, previous, status, now); err != nil {
				return err
			}
		}
		if actor.IsAdmin {
			return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "ticket.status.update", Target: fmt.Sprintf("ticket:%d", ticket.ID), Detail: "status=" + status}).Error
		}
		return nil
	})
}

func createTicketStatusEvent(tx *gorm.DB, ticketID, authorID uint, role, from, to string, now time.Time) error {
	return tx.Create(&model.TicketMessage{TicketID: ticketID, AuthorID: &authorID, AuthorRole: role, Type: "status", FromStatus: from, ToStatus: to, CreatedAt: now}).Error
}

func ticketView(row model.Ticket) experience.Ticket {
	out := experience.Ticket{ID: row.ID, TicketNo: row.TicketNo, UserID: row.UserID, Subject: row.Subject, Category: row.Category, Priority: row.Priority, Status: row.Status, LastMessageAt: row.LastMessageAt, ResolvedAt: row.ResolvedAt, ClosedAt: row.ClosedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if row.OrderID != nil || row.SubscriptionID != nil || row.WalletEntryID != nil {
		out.References = &experience.TicketReferences{OrderID: row.OrderID, SubscriptionID: row.SubscriptionID, WalletEntryID: row.WalletEntryID}
	}
	return out
}

func bindTicketReferences(tx *gorm.DB, ticket *model.Ticket, refs *experience.TicketReferences) error {
	if refs == nil {
		return nil
	}
	for _, ref := range []struct {
		table  string
		input  *uint
		target **uint
	}{
		{"subscriptions", refs.SubscriptionID, &ticket.SubscriptionID},
		{"wallet_entries", refs.WalletEntryID, &ticket.WalletEntryID},
	} {
		var err error
		*ref.target, err = ownedTicketReference(tx, ref.table, ticket.UserID, ref.input)
		if err != nil {
			return err
		}
	}
	return nil
}

func ownedTicketReference(tx *gorm.DB, table string, owner uint, input *uint) (*uint, error) {
	if input == nil {
		return nil, nil
	}
	id := *input
	var target struct{ ID uint }
	result := tx.Table(table).Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ? AND user_id = ?", id, owner).Limit(1).Find(&target)
	if result.Error != nil {
		return nil, result.Error
	}
	if id == 0 || result.RowsAffected != 1 {
		// Same response for a missing record and another account's record;
		// references never become an existence or authorization oracle.
		return nil, fmt.Errorf("%w: 关联记录不存在或不属于当前账户。", experience.ErrInvalid)
	}
	return &id, nil
}

func ticketMessageView(row model.TicketMessage) experience.TicketMessage {
	return experience.TicketMessage{ID: row.ID, TicketID: row.TicketID, AuthorID: row.AuthorID, AuthorRole: row.AuthorRole, Type: row.Type, Body: row.Body, FromStatus: row.FromStatus, ToStatus: row.ToStatus, CreatedAt: row.CreatedAt}
}

type ticketSummaryRow struct {
	model.Ticket
	UserEmail    string `gorm:"column:user_email"`
	MessageCount int64  `gorm:"column:message_count"`
}

func ticketSummary(row ticketSummaryRow) experience.TicketSummary {
	return experience.TicketSummary{Ticket: ticketView(row.Ticket), UserEmail: row.UserEmail, MessageCount: row.MessageCount}
}

func (s Tickets) ListTickets(ctx context.Context, input experience.TicketQuery) (experience.TicketPage, error) {
	query := s.DB.WithContext(ctx).Model(&model.Ticket{})
	if !input.Admin {
		query = query.Where("tickets.user_id = ?", input.ActorID)
	}
	if len(input.Statuses) > 0 {
		query = query.Where("tickets.status IN ?", input.Statuses)
	}
	if input.Category != "" {
		query = query.Where("tickets.category = ?", input.Category)
	}
	if input.Search != "" {
		pattern := "%" + input.Search + "%"
		query = query.Joins("JOIN users ticket_owner ON ticket_owner.id = tickets.user_id").Where("tickets.ticket_no LIKE ? OR tickets.subject LIKE ? OR ticket_owner.email LIKE ?", pattern, pattern, pattern)
	}
	page := experience.TicketPage{}
	if err := query.Count(&page.Total).Error; err != nil {
		return page, err
	}
	var rows []ticketSummaryRow
	if err := query.Select("tickets.*, (SELECT email FROM users WHERE users.id = tickets.user_id) AS user_email, (SELECT COUNT(*) FROM ticket_messages WHERE ticket_messages.ticket_id = tickets.id) AS message_count").Order("tickets.last_message_at DESC, tickets.id DESC").Offset(input.Offset).Limit(input.Limit).Scan(&rows).Error; err != nil {
		return page, err
	}
	page.Items = make([]experience.TicketSummary, 0, len(rows))
	for _, row := range rows {
		page.Items = append(page.Items, ticketSummary(row))
	}
	return page, nil
}

func (s Tickets) TicketDetail(ctx context.Context, actor experience.TicketActor, id, beforeID uint, limit int) (experience.TicketDetail, error) {
	var row ticketSummaryRow
	err := s.DB.WithContext(ctx).Model(&model.Ticket{}).Select("tickets.*, (SELECT email FROM users WHERE users.id = tickets.user_id) AS user_email, (SELECT COUNT(*) FROM ticket_messages WHERE ticket_messages.ticket_id = tickets.id) AS message_count").Where("tickets.id = ?", id).Take(&row).Error
	if err != nil {
		return experience.TicketDetail{}, announcementNotFound(err)
	}
	if !actor.IsAdmin && row.UserID != actor.ID {
		return experience.TicketDetail{}, experience.ErrTicketForbidden
	}
	messages, err := loadTicketMessages(s.DB.WithContext(ctx), id, beforeID, limit+1)
	if err != nil {
		return experience.TicketDetail{}, err
	}
	hasOlder := len(messages) > limit
	if hasOlder {
		messages = messages[:limit]
	}
	result := make([]experience.TicketMessageView, 0, len(messages))
	for index := len(messages) - 1; index >= 0; index-- {
		message := ticketMessageView(messages[index].TicketMessage)
		message.Attachments = messages[index].Attachments
		result = append(result, experience.TicketMessageView{TicketMessage: message, AuthorEmail: messages[index].AuthorEmail})
	}
	oldest := uint(0)
	if len(result) > 0 {
		oldest = result[0].ID
	}
	return experience.TicketDetail{Ticket: ticketSummary(row), Messages: result, HasOlderMessages: hasOlder, OldestMessageID: oldest}, nil
}
