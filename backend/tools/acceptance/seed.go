package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		fail(err)
	}
	return hex.EncodeToString(b)
}

func seed(dir string, nodes, subscriptions, records int) error {
	if nodes < 1 || subscriptions < nodes || records < 0 {
		return fmt.Errorf("invalid fixture sizes")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "zboard.db")
	guard, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("refuse existing database: %w", err)
	}
	_ = guard.Close()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		return err
	}
	pool, _ := db.DB()
	defer pool.Close()
	db.Logger = logger.Default.LogMode(logger.Silent)
	for _, migrate := range []func(*gorm.DB) error{datastore.RunMigrations, datastore.ReconcileCommerceSchema, datastore.ReconcileSubscriptionAccessSchema, datastore.ReconcileZeroEventSchema, datastore.ReconcileTrafficReadSchema, datastore.ReconcileOperationsSchema, datastore.ReconcileFairUseTelemetrySchema} {
		if err := migrate(db); err != nil {
			return err
		}
	}
	f := fixture{RunID: uuid.NewString(), AdminEmail: "acceptance@example.test", AdminPassword: randomHex(16), EncryptionKey: randomHex(32), JWTSecret: randomHex(32), HistoryRecords: records}
	cipher, err := security.NewCredentialCipher(f.EncryptionKey)
	if err != nil {
		return err
	}
	password, err := bcrypt.GenerateFromPassword([]byte(f.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.Installation{ID: 1, SiteName: "Isolated acceptance", SiteURL: "http://127.0.0.1", InstalledAt: now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.NodeGroup{ID: 1, Name: "Acceptance", Code: "acceptance", IsEnabled: true}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Plan{ID: 1, Name: "Acceptance", Slug: "acceptance", NodeGroupID: 1, TrafficBytes: 1 << 50, IsActive: true}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.PlanSKU{ID: 1, PlanID: 1, Code: "acceptance", Name: "Acceptance", BillingUnit: "month", BillingValue: 1, PriceCents: 100, Currency: "CNY", IsActive: true}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.PlanSKUOperation{PlanSKUID: 1, Operation: "purchase"}).Error; err != nil {
			return err
		}
		for i := 1; i <= nodes; i++ {
			token := randomHex(32)
			encrypted, err := cipher.Encrypt(token)
			if err != nil {
				return err
			}
			node := model.Node{ID: uint(i), Name: fmt.Sprintf("Acceptance %d", i), Address: "127.0.0.1", Config: "{}", NodeCredential: encrypted, NodeCredentialPrefix: token[:12], IsEnabled: true}
			if err := tx.Create(&node).Error; err != nil {
				return err
			}
			endpoint := model.ProtocolEndpoint{ID: uint(i), NodeID: node.ID, RuntimeKey: uuid.NewString(), Name: "Acceptance VLESS", Protocol: "vless", Address: "127.0.0.1", Port: 10000 + i, PublicPort: 10000 + i, MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true}
			if err := tx.Create(&endpoint).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.NodeGroupEndpoint{NodeGroupID: 1, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
				return err
			}
			f.Nodes = append(f.Nodes, nodeIdentity{node.ID, token})
		}
		for i := 1; i <= subscriptions; i++ {
			email := fmt.Sprintf("acceptance-%d@example.test", i)
			if i == 1 {
				email = f.AdminEmail
			}
			if err := tx.Create(&model.User{ID: uint(i), Email: email, Password: string(password), IsAdmin: i == 1, Status: "active"}).Error; err != nil {
				return err
			}
			initial := int64(records/subscriptions) * 3072
			if i <= records%subscriptions {
				initial += 3072
			}
			sub := model.Subscription{ID: uint(i), UserID: uint(i), PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, StartAt: now, EndAt: now.Add(7 * 24 * time.Hour), Status: "active", FlowTotal: 1 << 50, FlowUsed: initial, Config: "{}"}
			if err := tx.Create(&sub).Error; err != nil {
				return err
			}
			nodeID := uint((i-1)%nodes + 1)
			principal := fmt.Sprintf("acceptance:%d", i)
			secret, err := cipher.Encrypt(uuid.NewString())
			if err != nil {
				return err
			}
			credential := model.ProtocolCredential{ID: uint(i), SubscriptionID: sub.ID, UserID: sub.UserID, ProtocolEndpointID: nodeID, NodeID: nodeID, CredentialID: fmt.Sprintf("acceptance-%d", i), PrincipalKey: principal, Secret: secret, ListenPort: 10000 + int(nodeID), PublicPort: 10000 + int(nodeID), Status: "active", ExpiresAt: sub.EndAt}
			if err := tx.Create(&credential).Error; err != nil {
				return err
			}
			f.Subscriptions = append(f.Subscriptions, subscriptionIdentity{sub.ID, nodeID, principal, initial})
		}
		return nil
	})
	if err != nil {
		return err
	}
	historyStart := now.Add(-6 * 24 * time.Hour)
	historyStep := 6 * 24 * time.Hour / time.Duration(max(records, 1))
	for start := 0; start < records; start += 200 {
		var history []model.TrafficRecord
		var flows []model.FlowUsage
		for i := start; i < min(start+200, records); i++ {
			sub := f.Subscriptions[i%subscriptions]
			key := fmt.Sprintf("history-%d", i)
			// Divide the nanosecond duration before multiplying by the row index;
			// multiplying first overflows int64 at representative fixture sizes.
			at := historyStart.Add(time.Duration(i) * historyStep)
			history = append(history, model.TrafficRecord{UserID: sub.ID, SubscriptionID: sub.ID, NodeID: sub.NodeID, ProtocolEndpointID: sub.NodeID, ReportID: key, Nonce: key, FlowID: key, EventType: "flow.completed", RawBytes: 3072, UploadBytes: 1024, DownloadBytes: 2048, UsedBytes: 3072, ProtocolMultiplierMilli: 1000, At: at, Meta: "{}", CreatedAt: at, UpdatedAt: at})
			flows = append(flows, model.FlowUsage{NodeID: sub.NodeID, FlowID: key, ProtocolCredentialID: sub.ID, SubscriptionID: sub.ID, ProtocolEndpointID: sub.NodeID, PrincipalKey: sub.Principal, RawBytes: 3072, UploadBytes: 1024, DownloadBytes: 2048, UsedBytes: 3072, Status: "completed", LastEventID: key, LastSeenAt: at, CompletedAt: &at, CreatedAt: at, UpdatedAt: at})
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.CreateInBatches(history, 100).Error; err != nil {
				return err
			}
			return tx.CreateInBatches(flows, 100).Error
		}); err != nil {
			return err
		}
	}
	if err := verifySeedHistory(db, now, records); err != nil {
		return err
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "fixture.json"), f); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "zboard.json"), map[string]any{
		"Name": "zboard-acceptance", "Host": "0.0.0.0", "Port": 8080, "Mode": "test", "Timeout": 10000, "MaxBytes": 1048576,
		"Log":         map[string]any{"Mode": "console", "Level": "error"},
		"environment": "test", "database_driver": "sqlite", "datasource": "/data/zboard.db",
		"jwt_secret": f.JWTSecret, "credential_encryption_key": f.EncryptionKey,
		"zero_kernel_contract": "legacy", "zero_event_spool_mode": "file", "zero_event_spool_dir": "/data/events",
	})
}
