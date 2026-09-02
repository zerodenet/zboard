package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maintenanceCacheTTL = time.Second

type maintenanceState struct {
	Enabled                 bool   `json:"enabled"`
	Title                   string `json:"title"`
	Message                 string `json:"message"`
	MigrationInProgress     bool   `json:"migration_in_progress"`
	MigrationCutoverPending bool   `json:"migration_cutover_pending"`
}

type maintenanceUpdateRequest struct {
	Enabled           bool              `json:"enabled"`
	Title             string            `json:"title"`
	Message           string            `json:"message"`
	ExpectedRevisions map[string]uint64 `json:"expected_revisions"`
}

func defaultMaintenanceState() maintenanceState {
	return maintenanceState{Title: "系统维护中", Message: "系统正在维护，请稍后再试。"}
}

func maintenanceRouteAllowedWithoutAdmin(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/api/v1/version", "/api/v1/setup/status", "/api/v1/system/status", "/api/v1/system/configs",
		"/api/v1/announcements", "/api/v1/auth/login", "/api/v1/auth/me":
		return true
	default:
		return false
	}
}

func (h *handlers) invalidateMaintenanceState() {
	h.maintenanceMu.Lock()
	h.maintenanceLoadedAt = time.Time{}
	h.maintenanceMu.Unlock()
}

func (h *handlers) backgroundWorkPaused() bool {
	state, err := h.loadMaintenanceState(false)
	return err != nil || state.Enabled
}

func (h *handlers) loadMaintenanceState(force bool) (maintenanceState, error) {
	h.maintenanceMu.RLock()
	if !force && !h.maintenanceLoadedAt.IsZero() && time.Since(h.maintenanceLoadedAt) < maintenanceCacheTTL {
		state := h.maintenanceState
		h.maintenanceMu.RUnlock()
		return state, nil
	}
	h.maintenanceMu.RUnlock()

	h.maintenanceMu.Lock()
	defer h.maintenanceMu.Unlock()
	if !force && !h.maintenanceLoadedAt.IsZero() && time.Since(h.maintenanceLoadedAt) < maintenanceCacheTTL {
		return h.maintenanceState, nil
	}
	var configs []model.SystemConfig
	if err := h.db.Where("config_key IN ?", []string{"maintenance_enabled", "maintenance_title", "maintenance_message", "maintenance_task_id"}).Find(&configs).Error; err != nil {
		return maintenanceState{}, fmt.Errorf("load maintenance settings: %w", err)
	}
	state := defaultMaintenanceState()
	var taskID uint64
	for _, config := range configs {
		switch config.ConfigKey {
		case "maintenance_enabled":
			state.Enabled, _ = strconv.ParseBool(config.Value)
		case "maintenance_title":
			if value := strings.TrimSpace(config.Value); value != "" {
				state.Title = value
			}
		case "maintenance_message":
			if value := strings.TrimSpace(config.Value); value != "" {
				state.Message = value
			}
		case "maintenance_task_id":
			taskID, _ = strconv.ParseUint(config.Value, 10, 64)
		}
	}
	if taskID > 0 {
		var task model.Task
		result := h.db.Select("id", "status").Where("id = ? AND type = ?", taskID, taskTypeDatabaseMigration).First(&task)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return maintenanceState{}, fmt.Errorf("inspect maintenance migration: %w", result.Error)
		}
		state.MigrationInProgress = result.Error == nil && (task.Status == taskStatusPending || task.Status == taskStatusRunning)
		state.MigrationCutoverPending = state.Enabled && result.Error == nil && task.Status == taskStatusCompleted
		state.Enabled = state.Enabled || state.MigrationInProgress
	}
	h.maintenanceState = state
	h.maintenanceLoadedAt = time.Now()
	return state, nil
}

func (h *handlers) SystemStatusHandler(w http.ResponseWriter, r *http.Request) {
	state, err := h.loadMaintenanceState(true)
	if err != nil {
		ServerError(w, err)
		return
	}
	announcements, err := h.activeAnnouncements(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	unread, err := h.announcementUnreadCount(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"maintenance": state, "announcements": announcements, "announcement_unread_count": unread, "as_of": time.Now().UTC()})
}

func (h *handlers) AdminMaintenanceUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req maintenanceUpdateRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Message = strings.TrimSpace(req.Message)
	if req.Title == "" || len(req.Title) > 160 || req.Message == "" || len(req.Message) > 4000 {
		BadRequest(w, "maintenance title and message are required and exceed their allowed size")
		return
	}
	keys := []string{"maintenance_enabled", "maintenance_title", "maintenance_message"}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var configs []model.SystemConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key IN ?", keys).Find(&configs).Error; err != nil {
			return err
		}
		if len(configs) != len(keys) {
			return errors.New("maintenance settings are incomplete")
		}
		if !req.Enabled {
			var activeMigrations int64
			if err := tx.Model(&model.Task{}).Where("type = ? AND status IN ?", taskTypeDatabaseMigration, []int16{taskStatusPending, taskStatusRunning}).Count(&activeMigrations).Error; err != nil {
				return err
			}
			if activeMigrations > 0 {
				return errors.New("database migration is running; maintenance mode cannot be disabled")
			}
		}
		values := map[string]string{
			"maintenance_enabled": strconv.FormatBool(req.Enabled),
			"maintenance_title":   req.Title, "maintenance_message": req.Message,
		}
		for index := range configs {
			config := &configs[index]
			expected, exists := req.ExpectedRevisions[config.ConfigKey]
			if !exists || expected != config.Revision {
				return errConfigRevisionConflict
			}
			config.Value = values[config.ConfigKey]
			config.Revision++
			if err := tx.Save(config).Error; err != nil {
				return err
			}
		}
		return createAuditLog(tx, claims, "maintenance.update", "system:maintenance", fmt.Sprintf("enabled=%t", req.Enabled))
	})
	if errors.Is(err, errConfigRevisionConflict) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.invalidateMaintenanceState()
	state, err := h.loadMaintenanceState(true)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, state)
}
