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
		user, err := currentExperienceUser(tx, actor, false)
		if err != nil {
			return err
		}
		row := model.Ticket{TicketNo: input.TicketNo, UserID: user.ID, Subject: input.Subject, Category: input.Category, Priority: input.Priority, Status: "open", LastMessageAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.TicketMessage{TicketID: row.ID, AuthorID: &user.ID, AuthorRole: "user", Type: "message", Body: input.Body, CreatedAt: now}).Error; err != nil {
			return err
		}
		id = row.ID
		return nil
	})
	return id, err
}

func (s Tickets) ReplyTicket(ctx context.Context, actor experience.TicketActor, id uint, body string, now time.Time) error {
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
		if err := tx.Create(&model.TicketMessage{TicketID: ticket.ID, AuthorID: &user.ID, AuthorRole: role, Type: "message", Body: body, CreatedAt: now}).Error; err != nil {
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
	return experience.Ticket{ID: row.ID, TicketNo: row.TicketNo, UserID: row.UserID, Subject: row.Subject, Category: row.Category, Priority: row.Priority, Status: row.Status, LastMessageAt: row.LastMessageAt, ResolvedAt: row.ResolvedAt, ClosedAt: row.ClosedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
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
	type messageRow struct {
		model.TicketMessage
		AuthorEmail string `gorm:"column:author_email"`
	}
	query := s.DB.WithContext(ctx).Table("ticket_messages").Select("ticket_messages.*, COALESCE(users.email, '') AS author_email").Joins("LEFT JOIN users ON users.id = ticket_messages.author_id").Where("ticket_messages.ticket_id = ?", id)
	if beforeID > 0 {
		query = query.Where("ticket_messages.id < ?", beforeID)
	}
	var messages []messageRow
	if err := query.Order("ticket_messages.created_at DESC, ticket_messages.id DESC").Limit(limit + 1).Scan(&messages).Error; err != nil {
		return experience.TicketDetail{}, err
	}
	hasOlder := len(messages) > limit
	if hasOlder {
		messages = messages[:limit]
	}
	result := make([]experience.TicketMessageView, 0, len(messages))
	for index := len(messages) - 1; index >= 0; index-- {
		result = append(result, experience.TicketMessageView{TicketMessage: ticketMessageView(messages[index].TicketMessage), AuthorEmail: messages[index].AuthorEmail})
	}
	oldest := uint(0)
	if len(result) > 0 {
		oldest = result[0].ID
	}
	return experience.TicketDetail{Ticket: ticketSummary(row), Messages: result, HasOlderMessages: hasOlder, OldestMessageID: oldest}, nil
}
