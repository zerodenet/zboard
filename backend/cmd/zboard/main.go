package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/server"
	"github.com/zerodenet/zboard/backend/internal/version"
)

var configFile = flag.String("f", "etc/zboard.yaml", "the config file")

func main() {
	flag.Parse()

	var c cfgpkg.Config
	conf.MustLoad(*configFile, &c)
	c.ApplyEnvironment(os.Getenv)
	if err := c.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}
	if err := datastore.ValidateDSN(c.DataSource, c.Environment == cfgpkg.EnvironmentProduction); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}
	credentialCipher, err := security.NewCredentialCipher(c.CredentialEncryptionKey)
	if err != nil {
		log.Fatalf("credential encryption init failed: %v", err)
	}

	db, err := datastore.Open(c.DataSource)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := datastore.Ping(db); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Plan{},
		&model.Subscription{},
		&model.Order{},
		&model.Node{},
		&model.TrafficRecord{},
		&model.AuditLog{},
		&model.SubscriptionToken{},
	); err != nil {
		log.Fatalf("database migrate failed: %v", err)
	}
	migratedCredentials, err := datastore.MigrateNodeCredentials(db, credentialCipher)
	if err != nil {
		log.Fatalf("node credential migration failed: %v", err)
	}
	if migratedCredentials > 0 {
		log.Printf("encrypted legacy node credentials: count=%d", migratedCredentials)
	}
	if err := createBootstrapAdminIfNeeded(db, c.BootstrapAdmin(), c.Environment); err != nil {
		log.Fatalf("bootstrap admin failed: %v", err)
	}

	srv := rest.MustNewServer(c.RestConf)
	defer srv.Stop()
	webDir := os.Getenv("ZBOARD_WEB_DIR")
	var fileServer http.Handler
	if webDir != "" {
		webDir = filepath.Clean(webDir)
		fileServer = http.FileServer(http.Dir(webDir))
	}

	log.Printf("starting zboard service")
	log.Printf("version: %s", version.FullVersion())
	log.Printf("environment: %s", c.Environment)
	log.Printf("config datasource: %s", datastore.QuoteDSN(c.DataSource))

	if err := server.RegisterRoutes(srv, db, c.JwtSecret, credentialCipher); err != nil {
		log.Fatalf("route registration failed: %v", err)
	}
	if webDir != "" {
		// Keep static route fallback after api route registration for deterministic precedence.
		srv.AddRoutes([]rest.Route{{
			Method: http.MethodGet,
			Path:   "/*",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					http.NotFound(w, r)
					return
				}

				if strings.HasSuffix(r.URL.Path, "/") {
					r.URL.Path = "/"
				} else {
					cleaned := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
					candidate := filepath.Join(webDir, cleaned)
					if !fileExists(candidate) {
						r.URL.Path = "/"
					}
				}

				if r.URL.Path == "" {
					r.URL.Path = "/"
				}
				fileServer.ServeHTTP(w, r)
			}),
		}})
	}

	srv.Start()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func createBootstrapAdminIfNeeded(db *gorm.DB, adminConfig cfgpkg.BootstrapAdmin, environment string) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return fmt.Errorf("check user count: %w", err)
	}
	if count > 0 {
		return nil
	}
	if !adminConfig.Configured() {
		return errors.New("database has no users; configure ZBOARD_BOOTSTRAP_ADMIN_USERNAME, ZBOARD_BOOTSTRAP_ADMIN_EMAIL, and ZBOARD_BOOTSTRAP_ADMIN_PASSWORD")
	}
	admin, err := buildBootstrapAdmin(adminConfig, environment)
	if err != nil {
		return err
	}
	if err := db.Create(&admin).Error; err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	log.Printf("bootstrap admin created: username=%s", admin.Username)
	return nil
}

func buildBootstrapAdmin(adminConfig cfgpkg.BootstrapAdmin, environment string) (model.User, error) {
	if err := adminConfig.Validate(environment); err != nil {
		return model.User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(adminConfig.Password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, fmt.Errorf("hash bootstrap admin password: %w", err)
	}

	return model.User{
		Username: adminConfig.Username,
		Email:    adminConfig.Email,
		Password: string(hash),
		IsAdmin:  true,
		Status:   "active",
	}, nil
}
