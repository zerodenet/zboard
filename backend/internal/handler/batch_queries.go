package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"net/http"
	"strconv"
	"strings"
)

func batchQueryError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, jobs.ErrBatchPermission):
		Forbidden(w, "current administrator required")
	case errors.Is(err, jobs.ErrBatchNotFound):
		NotFound(w)
	case errors.Is(err, jobs.ErrBatchQuery):
		BadRequest(w, "invalid task query")
	default:
		writeJSON(w, http.StatusInternalServerError, "task query unavailable", nil)
	}
	return true
}
func batchStatus(r *http.Request) (*int16, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("status"))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 16)
	if err != nil || value < 0 || value > 3 {
		return nil, jobs.ErrBatchQuery
	}
	status := int16(value)
	return &status, nil
}
func (h *handlers) AdminTasksListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	status, err := batchStatus(r)
	if batchQueryError(w, err) {
		return
	}
	out, err := h.services.BatchQueries().List(r.Context(), claims.UserID, jobs.BatchFilter{Type: strings.TrimSpace(r.URL.Query().Get("type")), Status: status, Limit: queryInt(r, "limit", 50, 1, 100), Offset: queryInt(r, "offset", 0, 0, 1000000)})
	if batchQueryError(w, err) {
		return
	}
	if r.URL.Query().Get("paged") == "true" {
		OK(w, out)
	} else {
		OK(w, out.Items)
	}
}
func (h *handlers) AdminTaskSummaryHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	out, err := h.services.BatchQueries().Summary(r.Context(), claims.UserID)
	if !batchQueryError(w, err) {
		OK(w, out)
	}
}
func (h *handlers) AdminTaskGetHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/tasks/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.BatchQueries().Detail(r.Context(), claims.UserID, id, !parseBoolQuery(r.URL.Query().Get("summary")))
	if !batchQueryError(w, err) {
		OK(w, out)
	}
}
func (h *handlers) AdminTaskItemsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/tasks/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	status, err := batchStatus(r)
	if batchQueryError(w, err) {
		return
	}
	out, err := h.services.BatchQueries().Items(r.Context(), claims.UserID, id, jobs.BatchFilter{Status: status, Offset: offset, Limit: limit})
	if !batchQueryError(w, err) {
		OK(w, out)
	}
}
