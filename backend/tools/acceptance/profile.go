package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/handler"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm/logger"
)

type readQueryProfile struct {
	SQL string  `json:"sql"`
	MS  float64 `json:"ms"`
}
type readProfileLogger struct {
	logger.Interface
	Queries []readQueryProfile
}

func (l *readProfileLogger) Trace(_ context.Context, begin time.Time, sql func() (string, int64), _ error) {
	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	query, _ := sql()
	l.Queries = append(l.Queries, readQueryProfile{query, elapsed})
}

// Runs real read handlers without an event consumer or concurrent requests.
// The runner uses a disposable copy of the fixture, never a live panel DB.
func profileReads(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		return err
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil || f.RunID == "" {
		return fmt.Errorf("invalid isolated fixture")
	}
	path := filepath.Join(dir, "zboard.db")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		return err
	}
	pool, _ := db.DB()
	defer pool.Close()
	// Match production startup on this disposable database copy, including read
	// indexes and ledger invalidation metadata introduced after the source run.
	if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
		return err
	}
	cipher, err := security.NewCredentialCipher(f.EncryptionKey)
	if err != nil {
		return err
	}
	h, err := handler.NewHandlers(db, f.JWTSecret, cipher, "", "legacy", "")
	if err != nil {
		return err
	}
	loginBody, _ := json.Marshal(map[string]string{"email": f.AdminEmail, "password": f.AdminPassword})
	login := httptest.NewRecorder()
	h.LoginHandler(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(loginBody))))
	if login.Code != http.StatusOK {
		return fmt.Errorf("profile fixture login failed: %d", login.Code)
	}
	var auth struct {
		Data struct {
			Auth struct{ Token string }
		}
	}
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || auth.Data.Auth.Token == "" {
		return fmt.Errorf("profile fixture has no auth token")
	}
	trace := &readProfileLogger{Interface: logger.Discard}
	db.Logger = trace
	closeReads, err := h.ConfigureTrafficReads()
	if err != nil {
		return err
	}
	defer closeReads()
	var results []map[string]any
	for _, endpoint := range []struct {
		Path string
		Run  http.HandlerFunc
	}{
		{"/api/v1/admin/traffic/records?paged=true&bucket=hour&limit=50", h.TrafficUsageRecordsHandler},
		{"/api/v1/admin/traffic/records?paged=true&bucket=hour&limit=50&include_totals=false", h.TrafficUsageRecordsHandler},
		{"/api/v1/admin/traffic/records?view=usage_summary&bucket=hour", h.TrafficUsageRecordsHandler},
		{"/api/v1/admin/traffic/trends", h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler},
		{"/api/v1/admin/traffic/reconciliation?paged=true&limit=50", h.TrafficReconciliationHandler},
	} {
		for iteration := 0; iteration < 3; iteration++ {
			trace.Queries = nil
			r := httptest.NewRequest(http.MethodGet, endpoint.Path, nil)
			r.Header.Set("Authorization", "Bearer "+auth.Data.Auth.Token)
			w := httptest.NewRecorder()
			started := time.Now()
			endpoint.Run(w, r)
			results = append(results, map[string]any{"path": endpoint.Path, "iteration": iteration, "status": w.Code,
				"ms": float64(time.Since(started).Nanoseconds()) / 1e6, "queries": trace.Queries})
			if w.Code != http.StatusOK {
				return fmt.Errorf("profile read failed: %s status=%d", endpoint.Path, w.Code)
			}
		}
	}
	return writeJSON(filepath.Join(dir, "read-profile.json"), map[string]any{"run_id": f.RunID, "read_handlers": results,
		"note": "Isolated handler execution with no network, middleware or competing consumer; not mixed-load acceptance."})
}
