package main

import (
	"flag"
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
	"github.com/zerodenet/zboard/backend/internal/server"
	"github.com/zerodenet/zboard/backend/internal/version"
)

var configFile = flag.String("f", "etc/zboard.yaml", "the config file")

func main() {
	flag.Parse()

	var c cfgpkg.Config
	conf.MustLoad(*configFile, &c)
	c.DataSource = datastore.MustDSN(c.DataSource)

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
	); err != nil {
		log.Fatalf("database migrate failed: %v", err)
	}
	createBootstrapAdminIfNeeded(db)

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
	log.Printf("config datasource: %s", datastore.QuoteDSN(c.DataSource))

	server.RegisterRoutes(srv, db, c.JwtSecret)
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

func createBootstrapAdminIfNeeded(db *gorm.DB) {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		log.Printf("bootstrap check user count failed: %v", err)
		return
	}
	if count > 0 {
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("bootstrap admin hash failed: %v", err)
		return
	}

	admin := model.User{
		Username: "admin",
		Email:    "admin@zboard.local",
		Password: string(hash),
		IsAdmin:  true,
		Status:   "active",
	}
	if err := db.Create(&admin).Error; err != nil {
		log.Printf("bootstrap admin create failed: %v", err)
		return
	}
	log.Printf("bootstrap admin created: admin / admin123")
}
