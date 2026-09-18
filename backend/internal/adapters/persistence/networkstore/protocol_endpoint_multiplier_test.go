package networkstore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProtocolEndpointMultiplierChecksAuthorityAndCommitsAuditAtomically(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{
		NodeID: 1, Name: "vless", RuntimeKey: "multiplier-fixture", Protocol: "vless", Address: "node.example.test",
		Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true,
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	service := network.ProtocolEndpointMultiplier{Repository: ProtocolEndpointMultiplier{DB: db}}
	updated, err := service.Update(context.Background(), 1, endpoint.ID, 2500)
	if err != nil || updated.MultiplierMilli != 2500 || updated.Name != endpoint.Name {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "protocol_endpoint.multiplier.update", "protocol_endpoint:1").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d error=%v", auditCount, err)
	}
	if _, err := service.Update(context.Background(), 1, endpoint.ID, 2500); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "protocol_endpoint.multiplier.update").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("no-op audit count=%d error=%v", auditCount, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), 1, endpoint.ID, 3000); !errors.Is(err, network.ErrProtocolEndpointMultiplierPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
}

func TestProtocolEndpointMultiplierAuditFailureRollsBackValue(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: 1, Name: "vless", RuntimeKey: "multiplier-audit", Protocol: "vless", Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_multiplier_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("fail_multiplier_audit") })
	service := network.ProtocolEndpointMultiplier{Repository: ProtocolEndpointMultiplier{DB: db}}
	if _, err := service.Update(context.Background(), 1, endpoint.ID, 3000); err == nil {
		t.Fatal("audit failure accepted")
	}
	var stored model.ProtocolEndpoint
	if err := db.First(&stored, endpoint.ID).Error; err != nil || stored.MultiplierMilli != 1000 {
		t.Fatalf("stored=%+v error=%v", stored, err)
	}
}
