package meteringstore

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"reflect"
	"testing"
	"time"
)

func TestColdIdentitiesPreserveOldReplayAndReopenFloor(t *testing.T) {
	db, accounting, reports, base := retentionReports(t)
	now := base.Add(5 * 24 * time.Hour)
	if _, e := (FlowCursorCompaction{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if _, e := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	cold := ColdIdentities{DB: db, Now: func() time.Time { return now }}
	for i := 0; i < 4; i++ {
		if _, e := cold.Drain(t.Context(), 32); e != nil {
			t.Fatal(e)
		}
	}
	for _, m := range []any{&model.MeteringReportReceipt{}, &model.FlowCursorCheckpoint{}, &model.MeteringLedgerTarget{}} {
		var n int64
		if e := db.Model(m).Count(&n).Error; e != nil || n != 0 {
			t.Fatalf("%T: %d %v", m, n, e)
		}
	}
	for _, r := range reports {
		if _, _, e := accounting.Complete(t.Context(), r); e != nil {
			t.Fatal(e)
		}
	}
	if resultTotal(t, db) != 10 {
		t.Fatal("cold replay changed charge")
	}
	now = now.Add(time.Hour)
	db.Config.NowFunc = func() time.Time { return now }
	late := reports[0]
	late.EventID = "unseen-late-cold"
	late.BytesUp = 6
	late.Sequence = 8
	late.Revision = 8
	for i := 0; i < 2; i++ {
		if _, _, e := accounting.Complete(t.Context(), late); e != nil {
			t.Fatal(e)
		}
	}
	if resultTotal(t, db) != 15 {
		t.Fatal("reopened floor lost previous cumulative counter")
	}
	var sub model.Subscription
	if e := db.First(&sub, 1).Error; e != nil || sub.FlowUsed != 15 {
		t.Fatal(sub.FlowUsed, e)
	}
	// Existing immutable identity must fail closed on a damaged compressed body.
	if e := db.Model(&model.MeteringColdBlock{}).Where("node_id=1 AND kind=?", coldReport).Update("payload", []byte("bad")).Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e := accounting.Complete(t.Context(), reports[1]); e == nil {
		t.Fatal("damaged identity treated as unseen")
	}
	if resultTotal(t, db) != 15 {
		t.Fatal("corruption charged again")
	}
}
func TestColdIdentityTransferRollsBackSourceAndPageTogether(t *testing.T) {
	db, _, _, base := retentionReports(t)
	now := base.Add(5 * 24 * time.Hour)
	if _, e := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("CREATE TRIGGER reject_cold_source_delete BEFORE DELETE ON metering_report_receipts BEGIN SELECT RAISE(ABORT,'delete failed'); END").Error; e != nil {
		t.Fatal(e)
	}
	if _, e := (ColdIdentities{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 32); e == nil {
		t.Fatal("delete failure swallowed")
	}
	var n int64
	db.Model(&model.MeteringColdBlock{}).Where("kind=?", coldReport).Count(&n)
	if n != 0 {
		t.Fatal("page committed without source transfer", n)
	}
	db.Model(&model.MeteringReportReceipt{}).Count(&n)
	if n != 2 {
		t.Fatal("original identity missing", n)
	}
}
func TestColdDirectorySplitsAndRetainsExactIdentity(t *testing.T) {
	db, _ := cohortDB(t)
	entries := []coldIdentity{}
	for i := 0; i < 600; i++ {
		key := fmt.Sprintf("identity-%04d", i)
		entries = append(entries, coldIdentity{Weight: []byte(key), Key: key, Numbers: []uint64{1, uint64(i + 1)}})
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return putColdMany(tx, 1, coldReport, entries) }); e != nil {
		t.Fatal(e)
	}
	for _, i := range []int{0, 30, 299, 599} {
		r, ok, e := coldReceipt(db, 1, entries[i].Key)
		if e != nil || !ok || r.TrafficRecordID != uint(i+1) {
			t.Fatal(i, r, ok, e)
		}
	}
	if _, ok, e := coldReceipt(db, 1, "absent"); e != nil || ok {
		t.Fatal(ok, e)
	}
	var blocks []model.MeteringColdBlock
	db.Find(&blocks)
	raw, packed := 0, 0
	for _, b := range blocks {
		got, e := decodeCold(b)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range got {
			if !bytes.Equal(r.Weight, []byte(r.Key)) {
				t.Fatal("identity changed")
			}
		}
		raw += int(b.RawBytes)
		packed += len(b.Payload)
	}
	t.Logf("600 exact identities: %d bounded pages; encoded=%d compressed=%d bytes (directory/index costs separate)", len(blocks), raw, packed)
}

func TestColdNonceAndEmptyLeafRoundTrip(t *testing.T) {
	db, _ := cohortDB(t)
	key := "nonce-with-no-numbers"
	e := db.Transaction(func(tx *gorm.DB) error {
		return putColdMany(tx, 1, coldNonce, []coldIdentity{{Weight: []byte(key), Key: key, Value: "original-report"}})
	})
	if e != nil {
		t.Fatal(e)
	}
	r, ok, e := lookupColdIdentity(db, 1, coldNonce, key, false)
	if e != nil || !ok || r.Value != "original-report" {
		t.Fatal(r, ok, e)
	}
	// An empty leaf is a valid sealed directory position, not a missing authority.
	if e = db.Transaction(func(tx *gorm.DB) error {
		return putColdMany(tx, 1, coldFloor, []coldIdentity{{Weight: []byte("flow"), Key: "flow", Numbers: []uint64{1, 1, 0, 0, 0, 0, 1}}})
	}); e != nil {
		t.Fatal(e)
	}
	if e = db.Transaction(func(tx *gorm.DB) error {
		found, e := takeColdFloor(tx, 1, "flow")
		if !found && e == nil {
			t.Fatal("floor missing")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := lookupColdIdentity(db, 1, coldFloor, "absent", false); e != nil || ok {
		t.Fatal(ok, e)
	}
	if e = db.Where("node_id=1 AND kind=?", coldFloor).Delete(&model.MeteringColdBlock{}).Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e = lookupColdIdentity(db, 1, coldFloor, "absent", false); e == nil {
		t.Fatal("missing sealed page treated as unseen identity")
	}
}

func TestColdCorruptNodeDoesNotBlockOtherNodesOrDiscardOriginals(t *testing.T) {
	db, _ := cohortDB(t)
	if e := db.Create(&model.Node{ID: 2, Name: "healthy"}).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Transaction(func(tx *gorm.DB) error {
		return putColdMany(tx, 1, coldFloor, []coldIdentity{{Weight: []byte("old"), Key: "old", Numbers: []uint64{1, 1, 1, 1, 0, 1, 1}}})
	}); e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&model.MeteringColdBlock{}).Where("node_id=1 AND kind=3").UpdateColumn("payload", []byte("bad")).Error; e != nil {
		t.Fatal(e)
	}
	for _, node := range []uint{1, 2} {
		if e := db.Create(&model.FlowCursorCheckpoint{NodeID: node, FlowID: "new", ProtocolCredentialID: 1, Revision: 1, RawBytes: 1, UsedBytes: 1, IsCompleted: true}).Error; e != nil {
			t.Fatal(e)
		}
	}
	if _, e := (ColdIdentities{DB: db}).Drain(t.Context(), 32); e == nil {
		t.Fatal("held corrupt page not reported")
	}
	var n int64
	db.Model(&model.FlowCursorCheckpoint{}).Where("node_id=1").Count(&n)
	if n != 1 {
		t.Fatal("corrupt-node original removed", n)
	}
	db.Model(&model.FlowCursorCheckpoint{}).Where("node_id=2").Count(&n)
	if n != 0 {
		t.Fatal("healthy node blocked", n)
	}
	var root model.MeteringColdRoot
	if e := db.Where("node_id=2 AND kind=3").First(&root).Error; e != nil || root.EntryCount != 1 {
		t.Fatal(root, e)
	}
}

func TestColdFilterCannotHideIdentityAfterHeaderCorruption(t *testing.T) {
	db, _ := cohortDB(t)
	key := "present"
	if e := db.Transaction(func(tx *gorm.DB) error {
		return putColdMany(tx, 1, coldReport, []coldIdentity{{Weight: []byte(key), Key: key, Numbers: []uint64{1, 1}}})
	}); e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&model.MeteringColdBlock{}).Where("node_id=1 AND kind=1").UpdateColumn("bloom", make([]byte, 1024)).Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e := coldReceipt(db, 1, key); e == nil {
		t.Fatal("damaged negative filter silently admitted an old identity")
	}
}

func TestPreparedColdSplitRollbackAndStalePageNeverLoseIdentity(t *testing.T) {
	db, _ := cohortDB(t)
	entries := make([]coldIdentity, 500)
	for i := range entries {
		key := fmt.Sprintf("prepared-%04d", i)
		entries[i] = coldIdentity{Key: key, Weight: []byte(key), Numbers: []uint64{1, uint64(i + 1)}}
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return putColdMany(tx, 1, coldReport, entries) }); e != nil {
		t.Fatal(e)
	}
	incoming := make([]coldIdentity, 32)
	for i := range incoming {
		key := fmt.Sprintf("added-%04d", i)
		incoming[i] = coldIdentity{Key: key, Weight: []byte(key), Numbers: []uint64{1, uint64(i + 501)}}
	}
	p, targets, e := snapshotColdPages(db, 1, coldReport, incoming)
	if e != nil {
		t.Fatal(e)
	}
	if e := p.prepare(1, coldReport, incoming, targets); e != nil {
		t.Fatal(e)
	}
	if len(p.writes) < 2 {
		t.Fatal("fixture did not split the bounded page")
	}
	if e := db.Exec("CREATE TRIGGER reject_prepared_root BEFORE UPDATE ON metering_cold_roots BEGIN SELECT RAISE(ABORT,'root rejected'); END").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return p.apply(tx, 1, coldReport) }); e == nil {
		t.Fatal("page split committed without its root")
	}
	var root model.MeteringColdRoot
	db.First(&root, "node_id=1 AND kind=?", coldReport)
	if root.PageCount != 1 || root.EntryCount != 500 {
		t.Fatal("failed split changed directory", root)
	}
	db.Exec("DROP TRIGGER reject_prepared_root")
	if e := db.Transaction(func(tx *gorm.DB) error { return p.apply(tx, 1, coldReport) }); e != nil {
		t.Fatal(e)
	}
	for _, entry := range append(entries, incoming...) {
		got, ok, e := lookupColdIdentity(db, 1, coldReport, entry.Key, false)
		if e != nil || !ok || !reflect.DeepEqual(got, entry) {
			t.Fatal(entry.Key, got, ok, e)
		}
	}
	next := coldIdentity{Key: "later-prepared", Weight: []byte("later-prepared"), Numbers: []uint64{1, 1000}}
	stale, targets, e := snapshotColdPages(db, 1, coldReport, []coldIdentity{next})
	if e != nil {
		t.Fatal(e)
	}
	if e := stale.prepare(1, coldReport, []coldIdentity{next}, targets); e != nil {
		t.Fatal(e)
	}
	// An online rewrite of the very same leaf invalidates the prepared bytes.
	other := coldIdentity{Key: "concurrent-authority", Weight: []byte("concurrent-authority"), Numbers: []uint64{1, 1001}}
	for coldPrefix(coldRoute(other.Weight), stale.writes[0].Depth) != stale.writes[0].Prefix {
		other.Key += "x"
		other.Weight = []byte(other.Key)
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return putColdMany(tx, 1, coldReport, []coldIdentity{other}) }); e != nil {
		t.Fatal(e)
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return stale.apply(tx, 1, coldReport) }); !errors.Is(e, errColdPlanStale) {
		t.Fatal("stale prepared page overwrote concurrent authority", e)
	}
	if _, ok, e := lookupColdIdentity(db, 1, coldReport, other.Key, false); e != nil || !ok {
		t.Fatal("concurrent identity disappeared", ok, e)
	}
}

func TestCredentialActivityFailureCannotRollBackAcceptedCharge(t *testing.T) {
	db, accounting, reports, _ := retentionReports(t)
	if e := db.Model(&model.ProtocolCredential{}).Where("id=1").UpdateColumn("last_used_at", nil).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("CREATE TRIGGER reject_activity_hint BEFORE UPDATE ON protocol_credentials WHEN NEW.last_used_at IS NOT NULL BEGIN SELECT RAISE(ABORT,'hint unavailable'); END").Error; e != nil {
		t.Fatal(e)
	}
	event := reports[0]
	event.EventID = "higher-despite-hint-failure"
	event.Sequence = 8
	event.Revision = 8
	event.BytesUp = 6
	for range 2 {
		if _, _, e := accounting.Complete(t.Context(), event); e != nil {
			t.Fatal("UI hint failure rejected a financial result", e)
		}
	}
	var cred model.ProtocolCredential
	if e := db.First(&cred, 1).Error; e != nil || cred.LastUsedAt != nil {
		t.Fatal("hint failure was not exercised", e)
	}
	var sub model.Subscription
	if e := db.First(&sub, 1).Error; e != nil || sub.FlowUsed != 15 || resultTotal(t, db) != 15 {
		t.Fatal("hint failure or replay changed billing", sub.FlowUsed, e)
	}
}
