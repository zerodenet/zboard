package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/application"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/server"
	"github.com/zerodenet/zboard/backend/internal/version"
)

var configFile = flag.String("f", "etc/zboard.yaml", "the config file")
var migrateOnly = flag.Bool("migrate-only", false, "prepare the complete database schema and exit")
var checkProjectionDatabaseOnly = flag.Bool("check-projection-database", false, "read-only validation of prepared projection database dependencies and exit; does not start a worker")
var projectionWorker = flag.Bool("projection-worker", false, "run prepared usage and node observation projections without HTTP, plugins or a file spool")
var projectionWorkerDrainTimeout = flag.Duration("projection-worker-drain-timeout", 30*time.Second, "projection worker graceful drain budget (1s–5m)")

var prepareHourlyRollback = flag.Bool("prepare-hourly-rollback", false, "after stopping all writers and draining usage projections, prepare hourly statistics for a pre-0054 binary and exit")

func main() {
	flag.Parse()
	if err := validateProjectionWorkerMode(*projectionWorker, *migrateOnly, *prepareHourlyRollback, *checkProjectionDatabaseOnly, *projectionWorkerDrainTimeout); err != nil {
		log.Fatal(err)
	}
	if err := validateDatabaseModes(*migrateOnly, *prepareHourlyRollback, *checkProjectionDatabaseOnly); err != nil {
		log.Fatal(err)
	}

	var c cfgpkg.Config
	if *checkProjectionDatabaseOnly {
		if err := conf.Load(*configFile, &c); err != nil {
			log.Fatal("projection database check failed: configuration load")
		}
	} else {
		conf.MustLoad(*configFile, &c)
	}
	c.ApplyEnvironment(os.Getenv)
	if err := c.Validate(); err != nil {
		if *checkProjectionDatabaseOnly {
			log.Fatal("projection database check failed: configuration validation")
		}
		log.Fatalf("config validation failed: %v", err)
	}
	if err := datastore.ValidateDataSource(c.DatabaseDriver, c.DataSource, c.Environment == cfgpkg.EnvironmentProduction); err != nil {
		if *checkProjectionDatabaseOnly {
			log.Fatal("projection database check failed: datasource validation")
		}
		log.Fatalf("config validation failed: %v", err)
	}
	if *checkProjectionDatabaseOnly {
		if err := checkProjectionDatabase(context.Background(), c); err != nil {
			log.Fatalf("projection database check failed: %v", err)
		}
		log.Printf("projection database dependencies verified (read-only); no worker started")
		return
	}
	if *projectionWorker {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		log.Printf("starting projection worker (prepared database; no HTTP, plugins or spool)")
		if err := runProjectionWorker(ctx, c, *projectionWorkerDrainTimeout); err != nil {
			log.Fatalf("projection worker stopped: %v", err)
		}
		log.Printf("projection worker drained")
		return
	}
	credentialCipher, err := security.NewCredentialCipher(c.CredentialEncryptionKey)
	if err != nil {
		log.Fatalf("credential encryption init failed: %v", err)
	}

	db, err := datastore.OpenWithDriver(c.DatabaseDriver, c.DataSource, datastore.PoolConfig{
		MaxOpenConnections: c.DatabaseMaxOpenConnections,
		MaxIdleConnections: c.DatabaseMaxIdleConnections,
		ConnectionLifetime: time.Duration(c.DatabaseConnectionMaxLifetimeSecs) * time.Second,
	})
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := datastore.Ping(db); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}
	if *prepareHourlyRollback {
		if err := datastore.PrepareHourlyProjectionRollback(db); err != nil {
			log.Fatalf("hourly projection rollback preparation failed: %v", err)
		}
		log.Printf("hourly projection rollback prepared; keep writers stopped until the pre-0054 binary is installed")
		return
	}
	if err := application.PrepareDatabaseSchema(db); err != nil {
		log.Fatalf("database migrate failed: %v", err)
	}
	migratedCredentials, err := datastore.MigrateNodeCredentials(db, credentialCipher)
	if err != nil {
		log.Fatalf("node credential migration failed: %v", err)
	}
	if migratedCredentials > 0 {
		log.Printf("encrypted legacy node credentials: count=%d", migratedCredentials)
	}
	migratedProtocolConfigs, err := datastore.MigrateProtocolEndpointConfigs(db, credentialCipher)
	if err != nil {
		log.Fatalf("protocol endpoint config migration failed: %v", err)
	}
	if migratedProtocolConfigs > 0 {
		log.Printf("encrypted legacy protocol endpoint configs: count=%d", migratedProtocolConfigs)
	}
	if *migrateOnly {
		log.Printf("database migrations completed")
		return
	}
	if err := createBootstrapAdminIfNeeded(db, c.BootstrapAdmin(), c.Environment); err != nil {
		log.Fatalf("bootstrap admin failed: %v", err)
	}

	webDir := os.Getenv("ZBOARD_WEB_DIR")
	serverOptions := make([]rest.RunOption, 0, 1)
	if webDir != "" {
		serverOptions = append(serverOptions, rest.WithNotFoundHandler(staticFallbackHandler(webDir)))
	}
	server.ConfigureSafeHTTPLogging(&c.RestConf)
	pluginHTTPTransport := server.ConfigurePluginHTTPTransport(&c.RestConf)
	srv := rest.MustNewServer(c.RestConf, serverOptions...)
	defer srv.Stop()
	srv.Use(server.SafeAccessLogMiddleware)
	srv.Use(pluginHTTPTransport)

	log.Printf("starting zboard service")
	log.Printf("version: %s", version.FullVersion())
	log.Printf("environment: %s", c.Environment)
	log.Printf("config database driver: %s", c.DatabaseDriver)
	log.Printf("config datasource: %s", datastore.QuoteDataSource(c.DatabaseDriver, c.DataSource))

	closeZeroEventSpool, err := server.RegisterRoutes(
		srv,
		db,
		c.JwtSecret,
		credentialCipher,
		c.ZeroArtifactDir,
		c.ZeroKernelContract,
		c.ZeroLocalVersion,
		c.ZeroEventSpoolConfig(),
		c.Plugins,
		c.FileStorageDir,
	)
	if err != nil {
		log.Fatalf("route registration failed: %v", err)
	}
	if closeZeroEventSpool != nil {
		defer func() {
			if err := closeZeroEventSpool(); err != nil {
				log.Printf("Zero event spool shutdown failed: %v", err)
			}
		}()
	}

	srv.Start()
}

func createBootstrapAdminIfNeeded(db *gorm.DB, adminConfig cfgpkg.BootstrapAdmin, environment string) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return fmt.Errorf("check user count: %w", err)
	}
	if count > 0 {
		return ensureInstallationMarker(db)
	}
	if !adminConfig.Configured() {
		// An empty database is a supported state: the one-time web installer will
		// collect the site settings and first administrator.
		return nil
	}
	admin, err := buildBootstrapAdmin(adminConfig, environment)
	if err != nil {
		return err
	}
	if err := db.Create(&admin).Error; err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	if err := ensureInstallationMarker(db); err != nil {
		return err
	}
	log.Printf("bootstrap admin created: email=%s", admin.Email)
	return nil
}

func ensureInstallationMarker(db *gorm.DB) error {
	installation := model.Installation{
		ID:                1,
		SiteName:          "zboard",
		AllowRegistration: true,
		InstalledAt:       time.Now().UTC(),
	}
	if err := db.FirstOrCreate(&installation, model.Installation{ID: 1}).Error; err != nil {
		return fmt.Errorf("create installation marker: %w", err)
	}
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

	email := strings.ToLower(strings.TrimSpace(adminConfig.Email))
	return model.User{
		AccountName: email,
		Email:       email, Password: string(hash),
		IsAdmin: true, Status: "active",
	}, nil
}
