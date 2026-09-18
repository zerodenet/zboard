package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanSKUListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	q, err := parseSKUQuery(r, false)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	page, err := h.services.SKUQueries.Administrative(r.Context(), claims.UserID, q)
	if err != nil {
		skuQueryError(w, err)
		return
	}
	OK(w, pagedData(page.Items, page.Total, page.Offset, page.Limit))
}
func (h *handlers) PublicPlanSKUListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	q, err := parseSKUQuery(r, true)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	page, err := h.services.SKUQueries.Public(r.Context(), q)
	if err != nil {
		skuQueryError(w, err)
		return
	}
	OK(w, pagedData(page.Items, page.Total, page.Offset, page.Limit))
}
func (h *handlers) PlanSKUGetCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.SKUQueries.Get(r.Context(), claims.UserID, id)
	if err != nil {
		skuQueryError(w, err)
		return
	}
	OK(w, view)
}
func parseSKUQuery(r *http.Request, public bool) (commerce.SKUQuery, error) {
	prefix := "/api/v1/admin/plans/"
	if public {
		prefix = "/api/v1/plans/"
	}
	var q commerce.SKUQuery
	var err error
	q.PlanID, err = parsePathID(r.URL.Path, prefix)
	if err != nil {
		return q, err
	}
	values := r.URL.Query()
	q.Offset, q.Limit, err = parsePagination(values.Get("offset"), values.Get("limit"))
	if err != nil {
		return q, err
	}
	q.Search, q.Operation, q.LegacyType = values.Get("q"), values.Get("operation"), values.Get("sku_type")
	if public {
		q.AnchorID, err = positiveQueryID(values, "anchor_id")
	} else if raw := strings.TrimSpace(values.Get("active")); raw != "" {
		active, parseErr := parseStrictBool(raw)
		if parseErr != nil {
			return q, errors.New("invalid active")
		}
		q.Active = &active
	}
	return q, err
}
func skuQueryError(w http.ResponseWriter, err error) {
	var invalid *commerce.ValidationError
	switch {
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	case errors.Is(err, commerce.ErrPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	case errors.As(err, &invalid):
		BadRequestError(w, commerceValidationError(err))
	default:
		ServerError(w, err)
	}
}
