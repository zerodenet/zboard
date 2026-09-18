package networkstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func seedNodeAdministrationAdmin(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.User{ID: 1, Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true}).Error; err != nil {
		t.Fatal(err)
	}
}

func nodeAdministrationService(db *gorm.DB) network.NodeAdministration {
	return network.NodeAdministration{Repository: NodeAdministration{DB: db}}
}

func TestNodeAdministrationCommitsCreateUpdateSSHCredentialsAndAudits(t *testing.T) {
	db, _ := administrationFixture(t)
	seedNodeAdministrationAdmin(t, db)
	service := nodeAdministrationService(db)
	created, err := service.Create(context.Background(), 1, network.NodeCreateChange{
		Name: " node ", Region: " west ", Address: "192.0.2.10", NodeCredentialCiphertext: "enc:connector",
		NodeCredentialPrefix: "connector-12", Config: "{}", IsEnabled: true, SSHPort: 22,
		SSHAuthMethod: "password", SSHPwdCiphertext: "enc:ssh", SSHPrivilegeMode: "none",
	})
	if err != nil || created.ID == 0 || created.Name != "node" || created.LifecycleStatus != "active" {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	var kernel model.NodeKernelState
	if err := db.First(&kernel, "node_id = ?", created.ID).Error; err != nil || kernel.Status != "unknown" || kernel.RecommendedAction != "detect" {
		t.Fatalf("kernel=%+v error=%v", kernel, err)
	}
	maintenance, enabled := "maintenance", false
	updated, err := service.Update(context.Background(), 1, network.NodeUpdateRequest{ID: created.ID, LifecycleStatus: &maintenance, IsEnabled: &enabled})
	if err != nil || updated.LifecycleStatus != "maintenance" || updated.IsEnabled {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	ssh, err := service.SaveSSH(context.Background(), 1, created.ID, network.NodeSSHConfigurationChange{
		SSHHost: "host", SSHPort: 2222, SSHUser: "root", SSHAuthMethod: "private_key",
		SSHPwdCiphertext: "enc:key", SSHPassphraseCiphertext: "enc:passphrase",
		SSHPrivilegeMode: "sudo", SSHPrivilegeCiphertext: "enc:sudo", ResetHostKey: true,
	})
	if err != nil || ssh.SSHHost != "host" || ssh.SSHPort != 2222 || !ssh.SSHPrivilegeConfigured || ssh.SSHHostKeyFingerprint != "" {
		t.Fatalf("ssh=%+v error=%v", ssh, err)
	}
	if err := service.RotateCredential(context.Background(), 1, created.ID, network.NodeCredentialConnector, network.NodeCredentialChange{Ciphertext: "enc:new-connector", Prefix: "new-connecto"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RotateCredential(context.Background(), 1, created.ID, network.NodeCredentialTraffic, network.NodeCredentialChange{Ciphertext: "enc:traffic", Prefix: "traffic-pref"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCredential(context.Background(), 1, created.ID, network.NodeCredentialConnector); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCredential(context.Background(), 1, created.ID, network.NodeCredentialTraffic); err != nil {
		t.Fatal(err)
	}
	verifiedAt := time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC)
	verified, err := service.RecordSSHVerification(context.Background(), 1, created.ID, verifiedAt, true)
	if err != nil || verified.SSHVerifiedAt == nil || !verified.SSHVerifiedAt.Equal(verifiedAt) || verified.LastSyncAt == nil || !verified.LastSyncAt.Equal(verifiedAt) {
		t.Fatalf("verified=%+v error=%v", verified, err)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("target = ?", "node:"+formatUint(created.ID)).Count(&audits).Error; err != nil || audits != 7 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}
}

func TestNodeAdministrationRejectsPermissionDeletingInvalidEnableAndMissingCredential(t *testing.T) {
	for _, scenario := range []string{"permission", "deleting", "invalid enable", "missing credential"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			seedNodeAdministrationAdmin(t, db)
			service := nodeAdministrationService(db)
			created, err := service.Create(context.Background(), 1, network.NodeCreateChange{Name: "node", Config: "{}", IsEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "permission":
				if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
					t.Fatal(err)
				}
				_, err = service.Update(context.Background(), 1, network.NodeUpdateRequest{ID: created.ID, Name: stringPointerStore("blocked")})
				if !errors.Is(err, network.ErrNodeAdministrationPermission) {
					t.Fatalf("error=%v", err)
				}
			case "deleting":
				if err := db.Model(&model.Node{}).Where("id = ?", created.ID).Update("lifecycle_status", "deleting").Error; err != nil {
					t.Fatal(err)
				}
				err = service.RotateCredential(context.Background(), 1, created.ID, network.NodeCredentialTraffic, network.NodeCredentialChange{Ciphertext: "enc:new", Prefix: "new-prefix-12"})
				if !errors.Is(err, network.ErrNodeAdministrationDeleting) {
					t.Fatalf("error=%v", err)
				}
			case "invalid enable":
				if err := db.Model(&model.Node{}).Where("id = ?", created.ID).Update("lifecycle_status", "retired").Error; err != nil {
					t.Fatal(err)
				}
				enabled := true
				_, err = service.Update(context.Background(), 1, network.NodeUpdateRequest{ID: created.ID, IsEnabled: &enabled})
				var validation *network.NodeAdministrationValidation
				if !errors.As(err, &validation) {
					t.Fatalf("error=%v", err)
				}
			case "missing credential":
				err = service.RevokeCredential(context.Background(), 1, created.ID, network.NodeCredentialTraffic)
				if !errors.Is(err, network.ErrNodeCredentialNotFound) {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}

func TestNodeAdministrationAuditFailureRollsBackNodeMutation(t *testing.T) {
	db, _ := administrationFixture(t)
	seedNodeAdministrationAdmin(t, db)
	service := nodeAdministrationService(db)
	created, err := service.Create(context.Background(), 1, network.NodeCreateChange{Name: "node", Config: "{}", IsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_node_administration_audit", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "node.update" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("fail_node_administration_audit")
	_, err = service.Update(context.Background(), 1, network.NodeUpdateRequest{ID: created.ID, Name: stringPointerStore("changed")})
	if err == nil {
		t.Fatal("audit failure committed")
	}
	var row model.Node
	if err := db.First(&row, created.ID).Error; err != nil || row.Name != "node" {
		t.Fatalf("node=%+v error=%v", row, err)
	}
}

func formatUint(value uint) string {
	return fmt.Sprintf("%d", value)
}
