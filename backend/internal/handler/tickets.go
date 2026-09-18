package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
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

type ticketView = experience.TicketSummary
type ticketMessageView = experience.TicketMessageView
type ticketDetailView = experience.TicketDetail

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

	query := experience.TicketQuery{ActorID: claims.UserID, Admin: claims.IsAdmin, Offset: offset, Limit: limit}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		statuses, valid := ticketListStatusValues(status, adminScope)
		if !valid {
			BadRequest(w, "invalid ticket status")
			return
		}
		query.Statuses = statuses
	}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		if !validTicketCategory(category) {
			BadRequest(w, "invalid ticket category")
			return
		}
		query.Category = category
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		query.Search = keyword
	}
	page, err := h.services.Tickets.List(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(page.Items, page.Total, offset, limit))
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
	ticketID, err := h.services.Tickets.Create(r.Context(), claims.UserID, experience.NewTicket{
		TicketNo: newTicketNumber(now), Subject: body.Subject, Category: body.Category, Priority: body.Priority, Body: body.Body,
	}, now)
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	detail, err := h.ticketDetail(r.Context(), ticketID, claims)
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
	detail, err := h.ticketDetailPage(r.Context(), id, claims, beforeID, messageLimit)
	if errors.Is(err, experience.ErrNotFound) {
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

	err = h.services.Tickets.Reply(r.Context(), experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}, id, body.Body, time.Now().UTC())
	if errors.Is(err, experience.ErrNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, experience.ErrPermission) {
		Forbidden(w, err.Error())
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
	detail, err := h.ticketDetail(r.Context(), id, claims)
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
	err = h.changeTicketStatus(r.Context(), id, claims, ticketStatusClosed, false)
	if handleTicketMutationError(w, err) {
		return
	}
	detail, err := h.ticketDetail(r.Context(), id, claims)
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
	if err := h.changeTicketStatus(r.Context(), id, claims, body.Status, true); handleTicketMutationError(w, err) {
		return
	}
	detail, err := h.ticketDetail(r.Context(), id, claims)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}

var (
	errTicketForbidden = experience.ErrTicketForbidden
	errTicketClosed    = experience.ErrTicketClosed
)

func (h *handlers) changeTicketStatus(ctx context.Context, id uint, claims authClaims, status string, adminOverride bool) error {
	return h.services.Tickets.ChangeStatus(ctx, experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}, id, status, adminOverride, time.Now().UTC())
}

func (h *handlers) ticketDetail(ctx context.Context, id uint, claims authClaims) (ticketDetailView, error) {
	return h.ticketDetailPage(ctx, id, claims, 0, ticketMessagePageLimit)
}

func (h *handlers) ticketDetailPage(ctx context.Context, id uint, claims authClaims, beforeID uint, limit int) (ticketDetailView, error) {
	return h.services.Tickets.Detail(ctx, experience.TicketActor{ID: claims.UserID, IsAdmin: claims.IsAdmin}, id, beforeID, limit)
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
	if errors.Is(err, experience.ErrNotFound) {
		NotFound(w)
		return true
	}
	if errors.Is(err, errTicketForbidden) || errors.Is(err, experience.ErrPermission) {
		Forbidden(w, "ticket access denied")
		return true
	}
	ServerError(w, err)
	return true
}
