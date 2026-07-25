package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ticketStatusOpen         = "open"
	ticketStatusPendingAdmin = "pending_admin"
	ticketStatusPendingUser  = "pending_user"
	ticketStatusResolved     = "resolved"
	ticketStatusClosed       = "closed"
	ticketMessageReply       = "message"
	ticketMessageStatus      = "status"
	ticketMessagePageLimit   = 100
)

var ticketCategories = map[string]struct{}{
	"connection": {},
	"billing":    {},
	"account":    {},
	"other":      {},
}

var ticketStatuses = map[string]struct{}{
	ticketStatusOpen:         {},
	ticketStatusPendingAdmin: {},
	ticketStatusPendingUser:  {},
	ticketStatusResolved:     {},
	ticketStatusClosed:       {},
}

type ticketCreateRequest struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
	Priority int16  `json:"priority"`
	Body     string `json:"body"`
}

type ticketReplyRequest struct {
	Body string `json:"body"`
}

type ticketStatusRequest struct {
	Status string `json:"status"`
}

type ticketView struct {
	model.Ticket
	UserEmail    string `json:"user_email"`
	MessageCount int64  `json:"message_count"`
}

type ticketMessageView struct {
	model.TicketMessage
	AuthorEmail string `json:"author_email"`
}

type ticketDetailView struct {
	Ticket           ticketView          `json:"ticket"`
	Messages         []ticketMessageView `json:"messages"`
	HasOlderMessages bool                `json:"has_older_messages"`
	OldestMessageID  uint                `json:"oldest_message_id,omitempty"`
}

func (h *handlers) TicketListHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/tickets")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
		claims.IsAdmin = false
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	query := h.db.Model(&model.Ticket{})
	if !claims.IsAdmin {
		query = query.Where("tickets.user_id = ?", claims.UserID)
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		statuses, valid := ticketListStatusValues(status, adminScope)
		if !valid {
			BadRequest(w, "invalid ticket status")
			return
		}
		query = query.Where("tickets.status IN ?", statuses)
	}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		if !validTicketCategory(category) {
			BadRequest(w, "invalid ticket category")
			return
		}
		query = query.Where("tickets.category = ?", category)
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		pattern := "%" + keyword + "%"
		query = query.Joins("JOIN users ticket_owner ON ticket_owner.id = tickets.user_id").
			Where("tickets.ticket_no LIKE ? OR tickets.subject LIKE ? OR ticket_owner.email LIKE ?", pattern, pattern, pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	var tickets []model.Ticket
	if err := query.Order("tickets.last_message_at DESC, tickets.id DESC").Offset(offset).Limit(limit).Find(&tickets).Error; err != nil {
		ServerError(w, err)
		return
	}
	views, err := h.buildTicketViews(tickets)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(views, total, offset, limit))
}

func (h *handlers) TicketCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	claims.IsAdmin = false
	var body ticketCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		BadRequest(w, "invalid request body")
		return
	}
	if err := normalizeTicketCreateRequest(&body); err != nil {
		BadRequest(w, err.Error())
		return
	}

	now := time.Now().UTC()
	ticket := model.Ticket{
		TicketNo:      newTicketNumber(now),
		UserID:        claims.UserID,
		Subject:       body.Subject,
		Category:      body.Category,
		Priority:      body.Priority,
		Status:        ticketStatusOpen,
		LastMessageAt: now,
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ticket).Error; err != nil {
			return err
		}
		authorID := claims.UserID
		return tx.Create(&model.TicketMessage{
			TicketID: ticket.ID, AuthorID: &authorID, AuthorRole: ticketAuthorRole(claims),
			Type: ticketMessageReply, Body: body.Body, CreatedAt: now,
		}).Error
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	detail, err := h.ticketDetail(ticket.ID, claims)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

func (h *handlers) TicketGetHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/tickets/")
	var claims authClaims
	var err error
	prefix := "/api/v1/tickets/"
	if adminScope {
		prefix = "/api/v1/admin/tickets/"
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
		claims.IsAdmin = false
	}
	id, err := parsePathID(r.URL.Path, prefix)
	if err != nil {
		BadRequest(w, "invalid ticket id")
		return
	}
	beforeID, err := optionalUintQuery(r, "before_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	messageLimit, err := parseTicketMessageLimit(r.URL.Query().Get("message_limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	detail, err := h.ticketDetailPage(id, claims, beforeID, messageLimit)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errTicketForbidden) {
		Forbidden(w, "ticket access denied")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

func (h *handlers) TicketReplyHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/tickets/")
	var claims authClaims
	var err error
	prefix := "/api/v1/tickets/"
	if adminScope {
		prefix = "/api/v1/admin/tickets/"
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
		claims.IsAdmin = false
	}
	id, err := parsePathID(r.URL.Path, prefix)
	if err != nil {
		BadRequest(w, "invalid ticket id")
		return
	}
	var body ticketReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		BadRequest(w, "invalid request body")
		return
	}
	body.Body = strings.TrimSpace(body.Body)
	if err := validateTicketText("message", body.Body, 1, 5000); err != nil {
		BadRequest(w, err.Error())
		return
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, id).Error; err != nil {
			return err
		}
		if !claims.IsAdmin && ticket.UserID != claims.UserID {
			return errTicketForbidden
		}
		if ticket.Status == ticketStatusClosed {
			return errTicketClosed
		}
		now := time.Now().UTC()
		authorID := claims.UserID
		if err := tx.Create(&model.TicketMessage{
			TicketID: ticket.ID, AuthorID: &authorID, AuthorRole: ticketAuthorRole(claims),
			Type: ticketMessageReply, Body: body.Body, CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		fromStatus := ticket.Status
		toStatus := ticketStatusPendingAdmin
		if claims.IsAdmin {
			toStatus = ticketStatusPendingUser
		}
		updates := map[string]interface{}{"last_message_at": now, "status": toStatus, "resolved_at": nil, "closed_at": nil}
		if err := tx.Model(&ticket).Updates(updates).Error; err != nil {
			return err
		}
		if fromStatus != toStatus {
			return createTicketStatusEvent(tx, ticket.ID, &authorID, ticketAuthorRole(claims), fromStatus, toStatus, now)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errTicketForbidden) {
		Forbidden(w, "ticket access denied")
		return
	}
	if errors.Is(err, errTicketClosed) {
		BadRequest(w, "closed ticket cannot receive replies")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	detail, err := h.ticketDetail(id, claims)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

func (h *handlers) TicketCloseHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	claims.IsAdmin = false
	id, err := parsePathID(r.URL.Path, "/api/v1/tickets/")
	if err != nil {
		BadRequest(w, "invalid ticket id")
		return
	}
	err = h.changeTicketStatus(id, claims, ticketStatusClosed, false)
	if handleTicketMutationError(w, err) {
		return
	}
	detail, err := h.ticketDetail(id, claims)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

func (h *handlers) AdminTicketStatusHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/tickets/")
	if err != nil {
		BadRequest(w, "invalid ticket id")
		return
	}
	var body ticketStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		BadRequest(w, "invalid request body")
		return
	}
	body.Status = strings.TrimSpace(body.Status)
	if !validTicketStatus(body.Status) {
		BadRequest(w, "invalid ticket status")
		return
	}
	if err := h.changeTicketStatus(id, claims, body.Status, true); handleTicketMutationError(w, err) {
		return
	}
	if err := createAuditLog(h.db, claims, "ticket.status.update", fmt.Sprintf("ticket:%d", id), "status="+body.Status); err != nil {
		ServerError(w, err)
		return
	}
	detail, err := h.ticketDetail(id, claims)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

var (
	errTicketForbidden = errors.New("ticket access denied")
	errTicketClosed    = errors.New("ticket is closed")
)

func (h *handlers) changeTicketStatus(id uint, claims authClaims, status string, adminOverride bool) error {
	return h.db.Transaction(func(tx *gorm.DB) error {
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, id).Error; err != nil {
			return err
		}
		if !claims.IsAdmin && ticket.UserID != claims.UserID {
			return errTicketForbidden
		}
		if !adminOverride && status != ticketStatusClosed {
			return errTicketForbidden
		}
		if ticket.Status == status {
			return nil
		}
		fromStatus := ticket.Status
		now := time.Now().UTC()
		updates := map[string]interface{}{"status": status, "last_message_at": now}
		switch status {
		case ticketStatusResolved:
			updates["resolved_at"] = now
			updates["closed_at"] = nil
		case ticketStatusClosed:
			updates["closed_at"] = now
		default:
			updates["resolved_at"] = nil
			updates["closed_at"] = nil
		}
		if err := tx.Model(&ticket).Updates(updates).Error; err != nil {
			return err
		}
		authorID := claims.UserID
		return createTicketStatusEvent(tx, ticket.ID, &authorID, ticketAuthorRole(claims), fromStatus, status, now)
	})
}

func (h *handlers) ticketDetail(id uint, claims authClaims) (ticketDetailView, error) {
	return h.ticketDetailPage(id, claims, 0, ticketMessagePageLimit)
}

func (h *handlers) ticketDetailPage(id uint, claims authClaims, beforeID uint, limit int) (ticketDetailView, error) {
	var ticket model.Ticket
	if err := h.db.First(&ticket, id).Error; err != nil {
		return ticketDetailView{}, err
	}
	if !claims.IsAdmin && ticket.UserID != claims.UserID {
		return ticketDetailView{}, errTicketForbidden
	}
	views, err := h.buildTicketViews([]model.Ticket{ticket})
	if err != nil {
		return ticketDetailView{}, err
	}
	var messages []ticketMessageView
	query := h.db.Table("ticket_messages").
		Select("ticket_messages.*, COALESCE(users.email, '') AS author_email").
		Joins("LEFT JOIN users ON users.id = ticket_messages.author_id").
		Where("ticket_messages.ticket_id = ?", id)
	if beforeID > 0 {
		query = query.Where("ticket_messages.id < ?", beforeID)
	}
	if err := query.Order("ticket_messages.created_at DESC, ticket_messages.id DESC").Limit(limit + 1).Scan(&messages).Error; err != nil {
		return ticketDetailView{}, err
	}
	hasOlderMessages := len(messages) > limit
	if hasOlderMessages {
		messages = messages[:limit]
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	oldestMessageID := uint(0)
	if len(messages) > 0 {
		oldestMessageID = messages[0].ID
	}
	return ticketDetailView{
		Ticket:           views[0],
		Messages:         messages,
		HasOlderMessages: hasOlderMessages,
		OldestMessageID:  oldestMessageID,
	}, nil
}

func (h *handlers) buildTicketViews(tickets []model.Ticket) ([]ticketView, error) {
	if len(tickets) == 0 {
		return []ticketView{}, nil
	}
	userIDs := make([]uint, 0, len(tickets))
	ticketIDs := make([]uint, 0, len(tickets))
	seenUsers := make(map[uint]struct{}, len(tickets))
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
		if _, ok := seenUsers[ticket.UserID]; !ok {
			seenUsers[ticket.UserID] = struct{}{}
			userIDs = append(userIDs, ticket.UserID)
		}
	}
	var users []model.User
	if err := h.db.Select("id", "email").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	emails := make(map[uint]string, len(users))
	for _, user := range users {
		emails[user.ID] = user.Email
	}
	type ticketMessageCount struct {
		TicketID uint  `gorm:"column:ticket_id"`
		Count    int64 `gorm:"column:message_count"`
	}
	var countRows []ticketMessageCount
	if err := h.db.Model(&model.TicketMessage{}).
		Select("ticket_id, COUNT(*) AS message_count").
		Where("ticket_id IN ?", ticketIDs).
		Group("ticket_id").
		Scan(&countRows).Error; err != nil {
		return nil, err
	}
	counts := make(map[uint]int64, len(countRows))
	for _, row := range countRows {
		counts[row.TicketID] = row.Count
	}
	views := make([]ticketView, 0, len(tickets))
	for _, ticket := range tickets {
		views = append(views, ticketView{
			Ticket: ticket, UserEmail: emails[ticket.UserID], MessageCount: counts[ticket.ID],
		})
	}
	return views, nil
}

func parseTicketMessageLimit(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return ticketMessagePageLimit, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit < 20 || limit > ticketMessagePageLimit {
		return 0, fmt.Errorf("message_limit must be an integer between 20 and %d", ticketMessagePageLimit)
	}
	return limit, nil
}

func createTicketStatusEvent(tx *gorm.DB, ticketID uint, authorID *uint, authorRole, fromStatus, toStatus string, at time.Time) error {
	return tx.Create(&model.TicketMessage{
		TicketID: ticketID, AuthorID: authorID, AuthorRole: authorRole, Type: ticketMessageStatus,
		Body: "", FromStatus: fromStatus, ToStatus: toStatus, CreatedAt: at,
	}).Error
}

func normalizeTicketCreateRequest(body *ticketCreateRequest) error {
	body.Subject = strings.TrimSpace(body.Subject)
	body.Category = strings.TrimSpace(body.Category)
	body.Body = strings.TrimSpace(body.Body)
	if err := validateTicketText("subject", body.Subject, 1, 160); err != nil {
		return err
	}
	if !validTicketCategory(body.Category) {
		return errors.New("invalid ticket category")
	}
	if body.Priority == 0 {
		body.Priority = 1
	}
	if body.Priority != 1 && body.Priority != 2 {
		return errors.New("priority must be 1 or 2")
	}
	return validateTicketText("message", body.Body, 1, 5000)
}

func validateTicketText(field, value string, minRunes, maxRunes int) error {
	length := utf8.RuneCountInString(value)
	if length < minRunes || length > maxRunes {
		return fmt.Errorf("%s must contain between %d and %d characters", field, minRunes, maxRunes)
	}
	return nil
}

func validTicketCategory(category string) bool {
	_, ok := ticketCategories[category]
	return ok
}

func validTicketStatus(status string) bool {
	_, ok := ticketStatuses[status]
	return ok
}

func ticketListStatusValues(status string, adminScope bool) ([]string, bool) {
	if adminScope && status == adminAttentionStatus {
		return []string{ticketStatusOpen, ticketStatusPendingAdmin}, true
	}
	if !validTicketStatus(status) {
		return nil, false
	}
	return []string{status}, true
}

func ticketAuthorRole(claims authClaims) string {
	if claims.IsAdmin {
		return "admin"
	}
	return "user"
}

func newTicketNumber(now time.Time) string {
	random := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	return "T" + now.UTC().Format("20060102") + "-" + random[:8]
}

func handleTicketMutationError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return true
	}
	if errors.Is(err, errTicketForbidden) {
		Forbidden(w, "ticket access denied")
		return true
	}
	ServerError(w, err)
	return true
}
