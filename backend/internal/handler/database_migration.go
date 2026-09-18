package handler

import (
	"errors"
	"net/http"

	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

type databaseMigrationRequest struct {
	TargetDriver     string `json:"target_driver"`
	TargetDataSource string `json:"target_datasource"`
	Confirm          bool   `json:"confirm"`
}

func (h *handlers) AdminDatabaseMigrationPreflightHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req databaseMigrationRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.DatabaseMigrationInspection().Preflight(r.Context(), claims.UserID, platform.MigrationTarget{TargetDriver: req.TargetDriver, TargetDataSource: req.TargetDataSource})
	if err != nil {
		databaseMigrationError(w, err)
		return
	}
	OK(w, result)
}
func (h *handlers) AdminDatabaseMigrationStatusHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	result, err := h.services.DatabaseMigrationInspection().Status(r.Context(), claims.UserID)
	if err != nil {
		databaseMigrationError(w, err)
		return
	}
	OK(w, result)
}

func (h *handlers) AdminDatabaseMigrationStartHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req databaseMigrationRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.DatabaseMigration(h.credentialCipher).Start(r.Context(), claims.UserID, platform.MigrationStart{Target: platform.MigrationTarget{TargetDriver: req.TargetDriver, TargetDataSource: req.TargetDataSource}, Confirm: req.Confirm})
	if err != nil {
		databaseMigrationError(w, err)
		return
	}
	h.invalidateMaintenanceState()
	writeJSON(w, http.StatusAccepted, "database migration started; maintenance mode is enabled", result.Task())
}

func databaseMigrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrMaintenancePermission):
		Forbidden(w, "migration administrator authorization changed")
	case errors.Is(err, platform.ErrMigrationConfirmation):
		writeJSON(w, http.StatusPreconditionRequired, err.Error(), nil)
	case errors.Is(err, platform.ErrMigrationInvalid), errors.Is(err, platform.ErrMigrationTargetConnection):
		BadRequest(w, err.Error())
	case errors.Is(err, platform.ErrMigrationBusy), errors.Is(err, platform.ErrMigrationTargetOccupied), errors.Is(err, platform.ErrMigrationSourceSnapshot):
		writeJSON(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, capabilityjobs.ErrConflict):
		writeJSON(w, http.StatusConflict, "a database migration is already running", nil)
	default:
		ServerError(w, err)
	}
}
