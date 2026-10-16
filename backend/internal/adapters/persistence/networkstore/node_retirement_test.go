package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type retirementRemoteFixture struct {
	store NodeRetirement
	calls map[string]int
	fail  string
}

func (r *retirementRemoteFixture) RetireResource(ctx context.Context, id uint, run string, step network.RetirementStep) (string, error) {
	m, err := r.store.Material(ctx, id, run, step)
	if err != nil {
		return "", err
	}
	r.calls[step.Kind]++
	if r.fail == step.Kind {
		return "", errors.New("private provider token must not escape")
	}
	if m.Shared {
		return "共享资源已保留", nil
	}
	return "", nil
}
func retirementFixture(t *testing.T) (*gorm.DB, NodeRetirement, *retirementRemoteFixture, network.NodeRetirement) {
	t.Helper()
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	if err := db.Model(&model.Node{}).Where("id = ?", 1).Updates(map[string]any{"ssh_host": "retire.example.test", "ssh_port": 22, "ssh_host_key_fingerprint": "trusted", "ssh_user": "root"}).Error; err != nil {
		t.Fatal(err)
	}
	store := NodeRetirement{DB: db}
	remote := &retirementRemoteFixture{store: store, calls: map[string]int{}}
	return db, store, remote, network.NodeRetirement{Repository: store, Remote: remote}
}
func executeRetirement(db *gorm.DB, service network.NodeRetirement) error {
	return (jobs.Executor{Store: jobstore.New(db), Worker: "retirement-test", Timeout: 10 * time.Minute, Handlers: map[string]jobs.Handler{network.NodeRetirementHandler: service.Execute}}).RunOne(context.Background())
}
func TestRetirementRetainsFailedResourcesAndResumesAfterRestart(t *testing.T) {
	db, store, remote, service := retirementFixture(t)
	ctx := context.Background()
	first, err := store.Start(ctx, 1, 1, "retire")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Start(ctx, 1, 1, "retire")
	if err != nil || duplicate.RunID != first.RunID {
		t.Fatal(duplicate, err)
	}
	remote.fail = "certificate"
	if err := executeRetirement(db, service); err == nil {
		t.Fatal("expected failure")
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.LifecycleStatus != "deleting" {
		t.Fatal(node, err)
	}
	var cert model.ManagedCertificate
	if err := db.First(&cert, 9).Error; err != nil {
		t.Fatal("certificate evidence lost", err)
	}
	failed, err := store.Read(ctx, 1)
	if err != nil || failed.State != "failed" || failed.LastError == "" {
		t.Fatal(failed, err)
	}
	if remote.calls["zero"] != 0 || remote.calls["dns"] != 2 {
		t.Fatal(remote.calls)
	}
	// A new domain/adapter instance simulates application restart; progress is DB-owned.
	store = NodeRetirement{DB: db}
	service.Repository = store
	remote.fail = ""
	next, err := store.Start(ctx, 1, 1, "retire")
	if err != nil || next.RunID == first.RunID {
		t.Fatal(next, err)
	}
	if err := executeRetirement(db, service); err != nil {
		t.Fatal(err)
	}
	if remote.calls["dns"] != 2 || remote.calls["zero"] != 1 {
		t.Fatal("completed steps replayed", remote.calls)
	}
	if err := db.First(&node, 1).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("node not removed", err)
	}
	done, err := store.Read(ctx, 1)
	if err != nil || done.State != "succeeded" {
		t.Fatal(done, err)
	}
	if _, err := store.Start(ctx, 1, 1, "retire"); err != nil {
		t.Fatal("completed request is not idempotent", err)
	}
}
func TestRetirementDetachNeverCallsRemoteAndKeepsHistory(t *testing.T) {
	db, store, remote, service := retirementFixture(t)
	if err := db.Model(&model.Node{}).Where("id = ?", 1).Updates(map[string]any{"ssh_host": "", "ssh_host_key_fingerprint": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(context.Background(), 1, 1, "detach"); err != nil {
		t.Fatal(err)
	}
	if err := executeRetirement(db, service); err != nil {
		t.Fatal(err)
	}
	if len(remote.calls) != 0 {
		t.Fatal("detach performed external work", remote.calls)
	}
	var count int64
	db.Model(&jobstore.Record{}).Where("handler = ?", network.NodeCleanupHandler).Count(&count)
	if count != 0 {
		t.Fatal("detach queued legacy uninstall")
	}
	if _, err := store.Read(context.Background(), 1); err != nil {
		t.Fatal("history removed", err)
	}
}
func TestRetirementPreservesSharedDNSAndOtherNode(t *testing.T) {
	db, store, remote, service := retirementFixture(t)
	if err := db.Create(&model.Node{ID: 2, Name: "surviving", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	var dns model.ManagedDNSRecord
	if err := db.First(&dns, 7).Error; err != nil {
		t.Fatal(err)
	}
	shared := dns
	shared.ID = 0
	shared.NodeID = 2
	if err := db.Create(&shared).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err != nil {
		t.Fatal(err)
	}
	if err := executeRetirement(db, service); err != nil {
		t.Fatal(err)
	}
	if remote.calls["zero"] != 1 {
		t.Fatal(remote.calls)
	}
	if err := db.First(&shared, shared.ID).Error; err != nil {
		t.Fatal("shared DNS removed", err)
	}
	var surviving model.Node
	if err := db.First(&surviving, 2).Error; err != nil || surviving.Name != "surviving" {
		t.Fatal("other node changed", surviving, err)
	}
	item, _ := store.Read(context.Background(), 1)
	retained := false
	for _, step := range item.Steps {
		if step.ID == 7 && step.Kind == "dns" {
			retained = step.State == "retained"
		}
	}
	if !retained {
		t.Fatal("shared DNS not marked retained", item)
	}
}

func TestRetirementSharedCertificateBlocksBeforeAnyCleanup(t *testing.T) {
	db, store, remote, _ := retirementFixture(t)
	endpoint := model.ProtocolEndpoint{NodeID: 2, Name: "shared", Protocol: "vless", Address: "example.test", Port: 443, RuntimeKey: "shared-cert-retirement", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: 9, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"retire", "detach"} {
		if _, err := store.Start(context.Background(), 1, 1, mode); err == nil {
			t.Fatal("shared certificate accepted", mode)
		}
	}
	if len(remote.calls) != 0 {
		t.Fatal("remote calls made")
	}
	var certificate model.ManagedCertificate
	if err := db.First(&certificate, 9).Error; err != nil {
		t.Fatal(err)
	}
}
func TestRetirementRejectsSharedSSHAndNonAdminWithoutChangingNode(t *testing.T) {
	db, store, _, _ := retirementFixture(t)
	if _, err := store.Start(context.Background(), 0, 1, "detach"); !errors.Is(err, network.ErrResourcePermission) {
		t.Fatal(err)
	}
	if err := db.Create(&model.Node{Name: "shared", Config: "{}", SSHHost: "retire.example.test", SSHPort: 22}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err == nil {
		t.Fatal("shared host accepted")
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.LifecycleStatus == "deleting" {
		t.Fatal("failed preflight changed node", node, err)
	}
	if _, err := store.Start(context.Background(), 1, 1, "detach"); err != nil {
		t.Fatal("detach should remain available", err)
	}
}
func TestRetirementFencesStaleWorkersAndUnconfirmedRuns(t *testing.T) {
	db, store, _, _ := retirementFixture(t)
	ctx := context.Background()
	item, err := store.Start(ctx, 1, 1, "retire")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Advance(ctx, 1, "stale", 0, "succeeded", ""); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&jobstore.Record{}).Where("id = ?", item.RunID).Updates(map[string]any{"state": "unknown", "expires_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, 1, 1, "retire"); err == nil {
		t.Fatal("uncertain command replayed without timeout fence")
	}
	if err := (JobReviews{DB: db}).RecordVerifiedOutcome(ctx, jobs.Reviewer{AccountID: 1}, jobs.Review{RunID: item.RunID, Outcome: jobs.Succeeded, Reason: "not actually verified"}); !errors.Is(err, jobs.ErrOutcomeUnverified) {
		t.Fatal("generic review bypassed retirement", err)
	}
	if _, err := (NodeRemoval{DB: db, SkipRemote: true}).RemoveNode(ctx, 1, 1); err == nil {
		t.Fatal("active retirement bypassed by legacy deletion")
	}
	old := now.Add(-4 * time.Minute)
	if err := db.Model(&jobstore.Record{}).Where("id = ?", item.RunID).Update("expires_at", old).Error; err != nil {
		t.Fatal(err)
	}
	next, err := store.Start(ctx, 1, 1, "retire")
	if err != nil || next.RunID == item.RunID {
		t.Fatal(next, err)
	}
}

func TestRetirementRejectsCorruptPlanInsteadOfDeletingNode(t *testing.T) {
	db, store, _, service := retirementFixture(t)
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NodeRetirement{}).Where("node_id = ?", 1).Update("steps", "null").Error; err != nil {
		t.Fatal(err)
	}
	if err := executeRetirement(db, service); err == nil {
		t.Fatal("invalid plan accepted")
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil {
		t.Fatal("node lost after corrupt progress", err)
	}
}

func TestRetirementAllowsCredentialRepairOnlyAfterFailureOnSameHost(t *testing.T) {
	db, store, remote, service := retirementFixture(t)
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err != nil {
		t.Fatal(err)
	}
	change := network.NodeSSHConfigurationChange{SSHHost: "retire.example.test", SSHPort: 22, SSHUser: "root", SSHAuthMethod: "password", SSHPwdCiphertext: "new-encrypted-credential", SSHPrivilegeMode: "none"}
	admin := NodeAdministration{DB: db}
	if _, err := admin.UpdateNodeSSHConfiguration(context.Background(), 1, 1, change); err == nil {
		t.Fatal("active task credentials changed")
	}
	remote.fail = "certificate"
	_ = executeRetirement(db, service)
	if _, err := admin.UpdateNodeSSHConfiguration(context.Background(), 1, 1, change); err != nil {
		t.Fatal("failed credentials cannot be repaired", err)
	}
	change.SSHHost = "other.example.test"
	if _, err := admin.UpdateNodeSSHConfiguration(context.Background(), 1, 1, change); err == nil {
		t.Fatal("retirement rebound to another host")
	}
}

func TestRetirementMayExplicitlyDetachAfterPartialFailure(t *testing.T) {
	db, store, remote, service := retirementFixture(t)
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err != nil {
		t.Fatal(err)
	}
	remote.fail = "certificate"
	_ = executeRetirement(db, service)
	before := remote.calls["certificate"]
	if _, err := store.Start(context.Background(), 1, 1, "detach"); err != nil {
		t.Fatal(err)
	}
	if err := executeRetirement(db, service); err != nil {
		t.Fatal(err)
	}
	if remote.calls["certificate"] != before || remote.calls["zero"] != 0 {
		t.Fatal("detach performed remote work")
	}
}

func TestRetirementProtectsChildrenAndProviderUntilFinalization(t *testing.T) {
	db, store, _, _ := retirementFixture(t)
	if _, err := store.Start(context.Background(), 1, 1, "retire"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func() error{
		func() error { return (ResourceRemoval{DB: db}).RemoveDNS(context.Background(), 1, 7) },
		func() error { return (ResourceRemoval{DB: db}).RemoveCertificate(context.Background(), 1, 9) },
	} {
		var blocked *network.ResourceRemovalBlocked
		if err := action(); !errors.As(err, &blocked) || blocked.Blockers["node_retirement"] == 0 {
			t.Fatal("retirement evidence removable", err)
		}
	}
	blockers, err := providerDeletionBlockers(db, 1)
	if err != nil || blockers["retiring_dns"] == 0 || blockers["retiring_certificates"] == 0 {
		t.Fatal("provider removable during retirement", blockers, err)
	}
}
