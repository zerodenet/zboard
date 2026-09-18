package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

func (h *handlers) accountAdministration() identity.Administration {
	return h.services.Identity.Administration
}
func writeAccountAdministrationError(w http.ResponseWriter, err error) {
	var fields *identity.AccountValidation
	switch {
	case errors.As(err, &fields):
		BadRequestFields(w, "账户信息校验失败。", fields.Fields)
	case errors.Is(err, identity.ErrEmailConflict):
		BadRequestFields(w, "用户信息校验失败。", map[string]string{"email": "该邮箱已存在。"})
	case errors.Is(err, identity.ErrAccountNotFound):
		NotFound(w)
	case errors.Is(err, identity.ErrPermission), errors.Is(err, identity.ErrLastAdministrator):
		Forbidden(w, err.Error())
	default:
		ServerError(w, err)
	}
}
func (h *handlers) AdminUserCreateHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var input adminUserCreateReq
	if err := decodeBody(r, &input); err != nil {
		BadRequest(w, err.Error())
		return
	}
	user, err := h.accountAdministration().Create(r.Context(), identity.Principal{ID: actor.UserID, Actor: actor.Email}, identity.NewAccount{Email: input.Email, Password: input.Password, Status: input.Status, IsAdmin: input.IsAdmin})
	if err != nil {
		writeAccountAdministrationError(w, err)
		return
	}
	OK(w, user)
}
func (h *handlers) AdminUserUpdateHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var input adminUserUpdateReq
	if err := decodeBody(r, &input); err != nil {
		BadRequest(w, err.Error())
		return
	}
	user, err := h.accountAdministration().Update(r.Context(), identity.Principal{ID: actor.UserID, Actor: actor.Email}, id, identity.AccountChange{Status: input.Status, IsAdmin: input.IsAdmin, Password: input.Password})
	if err != nil {
		writeAccountAdministrationError(w, err)
		return
	}
	OK(w, user)
}
