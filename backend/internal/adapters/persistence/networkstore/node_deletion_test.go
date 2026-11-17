package networkstore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type nodeDeleteRemote struct {
	store  NodeRetirement
	dns    int
	denied bool
}

func (r *nodeDeleteRemote) RetireResource(ctx context.Context, id uint, run string, step network.RetirementStep) (string, error) {
	m, err := r.store.Material(ctx, id, run, step)
	if err != nil {
		return "", err
	}
	if step.Kind == "dns" {
		r.dns++
		if r.denied {
			return "", errors.New("provider rejected deletion")
		}
		return "", nil
	}
	if !m.RemoteCleanupPossible {
		return "", &network.RemoteCleanupUnconfirmed{Reason: m.RemoteCleanupReason}
	}
	return "", errors.New("test never claims native SSH cleanup")
}
func TestNodeDeleteUpgradesLegacyDetachAndNeverRetainsProviderDNS(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "completed"}[completed], func(t *testing.T) {
			db, store, _, legacy := retirementFixture(t)
			finished := time.Now().UTC()
			operation := model.CertificateOperation{ManagedCertificateID: 9, NodeID: 1, OperationType: "issue", Status: "succeeded", Phase: "complete", FinishedAt: &finished}
			if err := db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			first, err := store.Start(t.Context(), 1, 1, "detach")
			if err != nil {
				t.Fatal(err)
			}
			if completed {
				if err := executeRetirement(db, legacy); err != nil {
					t.Fatal(err)
				}
			} else {
				claim, err := jobstore.New(db).Claim(t.Context(), "legacy-test", []string{network.NodeRetirementHandler}, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if err := jobstore.New(db).Finish(t.Context(), claim, jobs.Failed); err != nil {
					t.Fatal(err)
				}
			}
			next, err := store.Start(t.Context(), 1, 1, "delete")
			if err != nil || next.RunID == first.RunID || next.Attempt != first.Attempt+1 {
				t.Fatal("new delete intent reused legacy completion", next, err)
			}
			remote := &nodeDeleteRemote{store: store}
			service := network.NodeRetirement{Repository: store, Remote: remote}
			if err := executeRetirement(db, service); err != nil {
				t.Fatal(err)
			}
			var dns int64
			if err := db.Model(&model.ManagedDNSRecord{}).Where("node_id = 1").Count(&dns).Error; err != nil || dns != 0 || remote.dns != 2 {
				t.Fatal("delete retained provider DNS", dns, remote.dns, err)
			}
			var certificate model.ManagedCertificate
			if err := db.First(&certificate, 9).Error; err != nil || certificate.Status != "deleted" || certificate.ProviderAccountID != nil || certificate.AutoRenew {
				t.Fatal("certificate management not deleted", certificate, err)
			}
			var history int64
			if err := db.Model(&model.CertificateOperation{}).Where("id = ?", operation.ID).Count(&history).Error; err != nil || history != 1 {
				t.Fatal("certificate history cascaded away", history, err)
			}
			done, err := store.Read(t.Context(), 1)
			if err != nil || done.State != "succeeded" || done.Mode != "delete" {
				t.Fatal(done, err)
			}
			for _, step := range done.Steps {
				if step.Kind == "dns" && step.State != "succeeded" {
					t.Fatal("DNS was skipped", step)
				}
				if step.Kind != "dns" && step.State != "unconfirmed" {
					t.Fatal("missing SSH was claimed cleaned", step)
				}
			}
		})
	}
}
func TestNodeDeleteCannotDowngradeToRetainingProviderResources(t *testing.T) {
	db, store, _, _ := retirementFixture(t)
	if _, err := store.Start(t.Context(), 1, 1, "delete"); err != nil {
		t.Fatal(err)
	}
	remote := &nodeDeleteRemote{store: store, denied: true}
	if err := executeRetirement(db, network.NodeRetirement{Repository: store, Remote: remote}); err == nil {
		t.Fatal("provider failure ignored")
	}
	if _, err := store.Start(t.Context(), 1, 1, "detach"); !errors.Is(err, jobs.ErrConflict) {
		t.Fatal("mandatory provider cleanup bypassed", err)
	}
	var dns int64
	db.Model(&model.ManagedDNSRecord{}).Where("node_id = 1").Count(&dns)
	if dns != 2 {
		t.Fatal("provider evidence was deleted before confirmation", dns)
	}
	remote.denied = false
	if _, err := store.Start(t.Context(), 1, 1, "delete"); err != nil {
		t.Fatal(err)
	}
	if err := executeRetirement(db, network.NodeRetirement{Repository: store, Remote: remote}); err != nil {
		t.Fatal("retry did not resume", err)
	}
}

// Keep the mandatory-delete contract in dynamically discovered MySQL CI coverage.
func TestMySQLNodeDeleteManagedResourcesAndLegacyUpgrade(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("requires disposable MySQL")
	}
	t.Run("legacy_upgrade", TestNodeDeleteUpgradesLegacyDetachAndNeverRetainsProviderDNS)
	t.Run("provider_retry", TestNodeDeleteCannotDowngradeToRetainingProviderResources)
}
