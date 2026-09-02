package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type databaseMigrationRequest struct {
	TargetDriver     string `json:"target_driver"`
	TargetDataSource string `json:"target_datasource"`
	Confirm          bool   `json:"confirm"`
}

type databaseMigrationSecret struct {
	TargetDriver     string `json:"target_driver"`
	TargetDataSource string `json:"target_datasource"`
}

type databaseMigrationStatus struct {
	SourceDriver string           `json:"source_driver"`
	Task         *model.Task      `json:"task,omitempty"`
	Maintenance  maintenanceState `json:"maintenance"`
	NextStep     string           `json:"next_step,omitempty"`
}

// ReconcileInterruptedDatabaseMigrations converts crash-interrupted work into
// an explicit failed result. Maintenance intentionally remains enabled: an
// operator must inspect the source/target and choose retry or recovery.
func (h *handlers) ReconcileInterruptedDatabaseMigrations() error {
	now := time.Now().UTC()
	return h.db.Transaction(func(tx *gorm.DB) error {
		var taskIDs []uint
		if err := tx.Model(&model.Task{}).
			Where("type = ? AND status IN ?", taskTypeDatabaseMigration, []int16{taskStatusPending, taskStatusRunning}).
			Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) == 0 {
			return nil
		}
		message := "database migration was interrupted by service restart; inspect the target and run preflight again"
		if err := tx.Model(&model.Task{}).Where("id IN ?", taskIDs).Updates(map[string]interface{}{
			"status": taskStatusFailed, "errors": message, "finished_at": now, "content": "",
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.TaskItem{}).Where("task_id IN ? AND status IN ?", taskIDs, []int16{taskStatusPending, taskStatusRunning}).Updates(map[string]interface{}{
			"status": taskStatusFailed, "error": message, "finished_at": now,
		}).Error
	})
}

func (h *handlers) normalizeDatabaseMigrationRequest(req *databaseMigrationRequest) error {
	req.TargetDriver = strings.ToLower(strings.TrimSpace(req.TargetDriver))
	req.TargetDataSource = strings.TrimSpace(req.TargetDataSource)
	if req.TargetDriver != datastore.DriverMySQL && req.TargetDriver != datastore.DriverSQLite {
		return errors.New("target_driver must be mysql or sqlite")
	}
	if req.TargetDriver == h.databaseDriver() {
		return errors.New("target database driver must differ from the active driver")
	}
	return datastore.ValidateDataSource(req.TargetDriver, req.TargetDataSource, false)
}

func (h *handlers) databaseDriver() string {
	if datastore.IsSQLite(h.db) {
		return datastore.DriverSQLite
	}
	return datastore.DriverMySQL
}

func openMigrationTarget(driver, dataSource string) (*gorm.DB, error) {
	db, err := datastore.OpenWithDriver(driver, dataSource, datastore.PoolConfig{
		MaxOpenConnections: 1, MaxIdleConnections: 1, ConnectionLifetime: 0,
	})
	if err != nil {
		return nil, err
	}
	if err := datastore.Ping(db); err != nil {
		if sqlDB, openErr := db.DB(); openErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	return db, nil
}

func ensureMigrationTargetEmpty(db *gorm.DB) error {
	tables, err := datastore.MigrationTables(db)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect target table %s: %w", table, err)
		}
		if count > 0 {
			return fmt.Errorf("target database is not empty: %s contains %d rows", table, count)
		}
	}
	return nil
}

func (h *handlers) ensureMigrationSourceQuiescent() error {
	checks := []struct {
		name  string
		model interface{}
		where string
		args  []interface{}
	}{
		{name: "background tasks", model: &model.Task{}, where: "type <> ? AND status IN ?", args: []interface{}{taskTypeDatabaseMigration, []int16{taskStatusPending, taskStatusRunning}}},
		{name: "node operations", model: &model.NodeOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "provider operations", model: &model.ProviderOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "certificate operations", model: &model.CertificateOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "protocol deployments", model: &model.ProtocolDeployment{}, where: "status = ?", args: []interface{}{"running"}},
	}
	for _, check := range checks {
		var count int64
		if err := h.db.Model(check.model).Where(check.where, check.args...).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect %s: %w", check.name, err)
		}
		if count > 0 {
			return fmt.Errorf("source database is busy: %d %s must finish before migration", count, check.name)
		}
	}
	return nil
}

func (h *handlers) AdminDatabaseMigrationPreflightHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var req databaseMigrationRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.normalizeDatabaseMigrationRequest(&req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.ensureMigrationSourceQuiescent(); err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	target, err := openMigrationTarget(req.TargetDriver, req.TargetDataSource)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "target database connection failed", map[string]string{"error": err.Error()})
		return
	}
	defer closeGormDatabase(target)
	if err := ensureMigrationTargetEmpty(target); err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	_, unlock, err := h.lockMigrationSource()
	if err != nil {
		writeJSON(w, http.StatusConflict, "source database cannot provide a consistent migration lock", map[string]string{"error": err.Error()})
		return
	}
	unlock()
	tables, err := datastore.MigrationTables(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"source_driver": h.databaseDriver(), "target_driver": req.TargetDriver,
		"target": datastore.QuoteDataSource(req.TargetDriver, req.TargetDataSource),
		"tables": len(tables), "ready": true,
	})
}

func (h *handlers) AdminDatabaseMigrationStatusHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var task model.Task
	result := h.db.Where("type = ?", taskTypeDatabaseMigration).Order("id DESC").Limit(1).Find(&task)
	if result.Error != nil {
		ServerError(w, result.Error)
		return
	}
	state, err := h.loadMaintenanceState(true)
	if err != nil {
		ServerError(w, err)
		return
	}
	response := databaseMigrationStatus{SourceDriver: h.databaseDriver(), Maintenance: state}
	if result.RowsAffected > 0 {
		task.Content = ""
		task.Scope = ""
		response.Task = &task
		if task.Status == taskStatusCompleted {
			response.NextStep = "更新 ZBOARD_DATABASE_DRIVER 和 ZBOARD_DATA_SOURCE，重启服务并验证后再关闭维护模式"
		}
	}
	OK(w, response)
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
	if err := h.normalizeDatabaseMigrationRequest(&req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if !req.Confirm {
		writeJSON(w, http.StatusPreconditionRequired, "confirm must be true after reviewing the migration warning", nil)
		return
	}
	var active int64
	if err := h.db.Model(&model.Task{}).Where("type = ? AND status IN ?", taskTypeDatabaseMigration, []int16{taskStatusPending, taskStatusRunning}).Count(&active).Error; err != nil {
		ServerError(w, err)
		return
	}
	if active > 0 {
		writeJSON(w, http.StatusConflict, "a database migration is already running", nil)
		return
	}
	if err := h.ensureMigrationSourceQuiescent(); err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	target, err := openMigrationTarget(req.TargetDriver, req.TargetDataSource)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "target database connection failed", map[string]string{"error": err.Error()})
		return
	}
	if err := ensureMigrationTargetEmpty(target); err != nil {
		closeGormDatabase(target)
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	closeGormDatabase(target)
	secretJSON, _ := json.Marshal(databaseMigrationSecret{TargetDriver: req.TargetDriver, TargetDataSource: req.TargetDataSource})
	ciphertext, err := h.credentialCipher.Encrypt(string(secretJSON))
	if err != nil {
		ServerError(w, err)
		return
	}
	now := time.Now().UTC()
	tables, err := datastore.MigrationTables(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: `{}`, Content: ciphertext, Status: taskStatusPending, Total: int64(len(tables)), MaxAttempts: 1, Priority: 100, ScheduledAt: &now}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		item := model.TaskItem{TaskID: task.ID, TargetType: "database", TargetID: req.TargetDriver, Status: taskStatusPending}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if err := setSystemConfigValue(tx, "maintenance_enabled", "true"); err != nil {
			return err
		}
		if err := setSystemConfigValue(tx, "maintenance_task_id", fmt.Sprint(task.ID)); err != nil {
			return err
		}
		return createAuditLog(tx, claims, "database.migration.start", fmt.Sprintf("task:%d", task.ID), "target_driver="+req.TargetDriver)
	})
	if err != nil {
		ServerError(w, err)
		return
	}
	h.invalidateMaintenanceState()
	go h.runDatabaseMigration(task.ID)
	task.Content = ""
	task.Scope = ""
	writeJSON(w, http.StatusAccepted, "database migration started; maintenance mode is enabled", task)
}

func setSystemConfigValue(tx *gorm.DB, key, value string) error {
	return tx.Model(&model.SystemConfig{}).Where("config_key = ?", key).Updates(map[string]interface{}{
		"value": value, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC(),
	}).Error
}

func closeGormDatabase(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func (h *handlers) runDatabaseMigration(taskID uint) {
	now := time.Now().UTC()
	if result := h.db.Model(&model.Task{}).Where("id = ? AND status = ?", taskID, taskStatusPending).Updates(map[string]interface{}{"status": taskStatusRunning, "started_at": now}); result.Error != nil || result.RowsAffected != 1 {
		return
	}
	h.db.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{"status": taskStatusRunning, "started_at": now})
	var task model.Task
	if err := h.db.First(&task, taskID).Error; err != nil {
		return
	}
	plaintext, err := h.credentialCipher.Decrypt(task.Content)
	if err != nil {
		h.finishDatabaseMigration(taskID, err)
		return
	}
	var secret databaseMigrationSecret
	if err := json.Unmarshal([]byte(plaintext), &secret); err != nil {
		h.finishDatabaseMigration(taskID, err)
		return
	}
	err = h.copyDatabase(secret, taskID)
	h.finishDatabaseMigration(taskID, err)
}

func (h *handlers) finishDatabaseMigration(taskID uint, migrationErr error) {
	status := taskStatusCompleted
	errorText := ""
	if migrationErr != nil {
		status = taskStatusFailed
		errorText = migrationErr.Error()
	}
	now := time.Now().UTC()
	current := interface{}(gorm.Expr("current"))
	if migrationErr == nil {
		current = gorm.Expr("total")
	}
	h.db.Model(&model.Task{}).Where("id = ?", taskID).Updates(map[string]interface{}{"status": status, "current": current, "errors": errorText, "finished_at": now, "content": ""})
	h.db.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{"status": status, "error": errorText, "finished_at": now})
	h.invalidateMaintenanceState()
}

func (h *handlers) copyDatabase(secret databaseMigrationSecret, taskID uint) error {
	target, err := openMigrationTarget(secret.TargetDriver, secret.TargetDataSource)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer closeGormDatabase(target)
	if err := ensureMigrationTargetEmpty(target); err != nil {
		return err
	}
	if err := datastore.RunMigrations(target); err != nil {
		return fmt.Errorf("prepare target schema: %w", err)
	}
	tables, err := datastore.MigrationTables(h.db)
	if err != nil {
		return err
	}
	source, unlock, err := h.lockMigrationSource()
	if err != nil {
		return err
	}
	defer unlock()
	if datastore.IsSQLite(target) {
		if err := target.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
			return err
		}
		defer target.Exec("PRAGMA foreign_keys = ON")
	}
	if err := clearMigrationTarget(target, tables); err != nil {
		return err
	}
	return target.Transaction(func(tx *gorm.DB) error {
		if !datastore.IsSQLite(target) {
			if err := tx.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
				return err
			}
		}
		for _, table := range tables {
			if !source.Migrator().HasTable(table) || !tx.Migrator().HasTable(table) {
				continue
			}
			if err := copyTableRows(source, tx, table); err != nil {
				return err
			}
		}
		completedAt := time.Now().UTC()
		if err := tx.Model(&model.Task{}).Where("id = ? AND type = ?", taskID, taskTypeDatabaseMigration).Updates(map[string]interface{}{
			"status": taskStatusCompleted, "current": gorm.Expr("total"), "errors": "", "content": "", "finished_at": completedAt,
		}).Error; err != nil {
			return fmt.Errorf("finalize target migration task: %w", err)
		}
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
			"status": taskStatusCompleted, "error": "", "finished_at": completedAt,
		}).Error; err != nil {
			return fmt.Errorf("finalize target migration task item: %w", err)
		}
		if !datastore.IsSQLite(target) {
			return tx.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
		}
		return nil
	})
}

func clearMigrationTarget(target *gorm.DB, tables []string) error {
	return target.Transaction(func(tx *gorm.DB) error {
		if !datastore.IsSQLite(target) {
			if err := tx.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
				return err
			}
		}
		for index := len(tables) - 1; index >= 0; index-- {
			if tx.Migrator().HasTable(tables[index]) {
				if err := tx.Exec("DELETE FROM " + quoteIdentifier(target, tables[index])).Error; err != nil {
					return fmt.Errorf("clear target table %s: %w", tables[index], err)
				}
			}
		}
		if !datastore.IsSQLite(target) {
			return tx.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
		}
		return nil
	})
}

func (h *handlers) lockMigrationSource() (*gorm.DB, func(), error) {
	if datastore.IsSQLite(h.db) {
		tx := h.db.Begin()
		if tx.Error != nil {
			return nil, nil, fmt.Errorf("acquire SQLite migration snapshot: %w", tx.Error)
		}
		return tx, func() { _ = tx.Rollback().Error }, nil
	}
	sqlDB, err := h.db.DB()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reserve source lock connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "FLUSH TABLES WITH READ LOCK"); err != nil {
		connection.Close()
		return nil, nil, fmt.Errorf("acquire MySQL migration read lock (grant RELOAD or stop external writers): %w", err)
	}
	return h.db, func() {
		_, _ = connection.ExecContext(context.Background(), "UNLOCK TABLES")
		_ = connection.Close()
	}, nil
}

func copyTableRows(source, target *gorm.DB, table string) error {
	rows, err := source.Table(table).Rows()
	if err != nil {
		return fmt.Errorf("read source table %s: %w", table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read source columns %s: %w", table, err)
	}
	batch := make([]map[string]interface{}, 0, 200)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return fmt.Errorf("scan source table %s: %w", table, err)
		}
		record := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			if raw, ok := values[index].([]byte); ok {
				record[column] = append([]byte(nil), raw...)
			} else {
				record[column] = values[index]
			}
		}
		batch = append(batch, record)
		if len(batch) == cap(batch) {
			if err := target.Table(table).Create(&batch).Error; err != nil {
				return fmt.Errorf("write target table %s: %w", table, err)
			}
			batch = make([]map[string]interface{}, 0, 200)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source table %s: %w", table, err)
	}
	if len(batch) > 0 {
		if err := target.Table(table).Create(&batch).Error; err != nil {
			return fmt.Errorf("write target table %s: %w", table, err)
		}
	}
	var sourceCount, targetCount int64
	if err := source.Table(table).Count(&sourceCount).Error; err != nil {
		return err
	}
	if err := target.Table(table).Count(&targetCount).Error; err != nil {
		return err
	}
	if sourceCount != targetCount {
		return fmt.Errorf("verify table %s: source=%d target=%d", table, sourceCount, targetCount)
	}
	return nil
}

func quoteIdentifier(db *gorm.DB, value string) string {
	if datastore.IsSQLite(db) {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
