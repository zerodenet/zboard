package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
)

func (h *handlers) AdminMailDeliveryReviewHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 8 || parts[5] != "items" || parts[7] != "review" {
		BadRequest(w, "invalid delivery review path")
		return
	}
	taskID, err := strconv.ParseUint(parts[4], 10, 32)
	if err != nil {
		BadRequest(w, "invalid task ID")
		return
	}
	itemID, err := strconv.ParseUint(parts[6], 10, 32)
	if err != nil {
		BadRequest(w, "invalid item ID")
		return
	}
	var in messaging.DeliveryReviewInput
	if err := decodeBody(r, &in); err != nil {
		BadRequest(w, "invalid review input")
		return
	}
	err = h.services.DeliveryReview().Review(r.Context(), claims.UserID, uint(taskID), uint(itemID), in)
	switch {
	case errors.Is(err, messaging.ErrTemplatePermission):
		Forbidden(w, "current administrator required")
	case errors.Is(err, messaging.ErrDeliveryReviewConflict):
		writeJSON(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, messaging.ErrInvalidMessage):
		BadRequest(w, "请选择接收结果并填写 5–2000 字节的核验依据")
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, "delivery review failed", nil)
	default:
		OK(w, map[string]any{"task_id": taskID, "item_id": itemID, "reviewed": true})
	}
}

func (h *handlers) AdminMailDeliveryHistoryHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 8 || parts[5] != "items" || parts[7] != "attempts" {
		BadRequest(w, "invalid delivery history path")
		return
	}
	taskID, e1 := strconv.ParseUint(parts[4], 10, 32)
	itemID, e2 := strconv.ParseUint(parts[6], 10, 32)
	if e1 != nil || e2 != nil {
		BadRequest(w, "invalid delivery IDs")
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	page, err := h.services.DeliveryHistory().List(r.Context(), claims.UserID, uint(taskID), uint(itemID), limit, offset)
	switch {
	case errors.Is(err, messaging.ErrTemplatePermission):
		Forbidden(w, "current administrator required")
	case errors.Is(err, messaging.ErrDeliveryNotFound):
		NotFound(w)
	case errors.Is(err, messaging.ErrInvalidMessage):
		BadRequest(w, "invalid delivery history parameters")
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, "delivery history unavailable", nil)
	default:
		OK(w, page)
	}
}
