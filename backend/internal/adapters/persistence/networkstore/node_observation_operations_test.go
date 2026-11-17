package networkstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestObservationIntentOperationsHeadersOnly(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	observationIntentOperationsHeaders(t)
}
func TestMySQLObservationIntentOperationsHeadersOnly(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	observationIntentOperationsHeaders(t)
}
func observationIntentOperationsHeaders(t *testing.T) {
	a, b := administrationFixture(t)
	seedNodeAdministrationAdmin(t, a)
	ctx := context.Background()
	service := network.ObservationIntentOperations{Repository: ObservationIntentOperations{DB: a}}
	node := model.Node{Name: "current-node-is-not-historical-source-name", Config: "{}", IsEnabled: true, ActiveFlows: 71, BytesUp: 72, BytesDown: 73}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 1, 2, 3, 456000000, time.UTC)
	var ids []string
	for i, core := range []string{"normal", strings.Repeat("wire-core-private", 60000), "large-private", "corrupt-private"} {
		source := observationSource(node, string(rune('a'+i)), core, uint64(i+1), false)
		source.PrincipalKey = "private-principal"
		source.FlowID = "private-flow"
		source.Observation.Latest.SourceID = "private-source"
		source.Payload = []byte("private-payload")
		if i == 2 {
			source.Payload = bytes.Repeat([]byte("x"), int(observationstore.MaxApplyBytes)+1)
		}
		id := saveObservation(t, a, source)
		ids = append(ids, id)
		if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"status": "held", "reason": "invalid_material", "available_at": at, "token": "private-claim-token"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Model(&model.NodeObservationPart{}).Where("intent_id = ? AND ordinal = 0", ids[3]).Update("data", []byte("bad retained bytes")).Error; err != nil {
		t.Fatal(err)
	}
	// All metadata relations, including deleted/reused numeric IDs, remain visible.
	relations := map[string]string{}
	relations[ids[0]] = "same_asset"
	for _, kind := range []string{"node_missing", "different_asset", "asset_deleted", "unknown"} {
		asset := model.Node{Name: kind, Config: "{}", IsEnabled: true}
		if err := a.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		source := observationSource(asset, kind, "runtime", 1, false)
		if kind == "unknown" {
			source.Observation.Latest.AssetTag = ""
		}
		id := saveObservation(t, a, source)
		relations[id] = kind
		ids = append(ids, id)
		if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"status": "held", "available_at": at}).Error; err != nil {
			t.Fatal(err)
		}
		if kind != "unknown" {
			if kind == "asset_deleted" {
				if err := a.Transaction(func(tx *gorm.DB) error { return observationstore.Tombstone(tx, asset.ID, asset.AssetTag) }); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Delete(&asset).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "different_asset" {
				fresh := model.Node{ID: asset.ID, Name: "replacement-not-original", Config: "{}", IsEnabled: true}
				if err := a.Create(&fresh).Error; err != nil {
					t.Fatal(err)
				}
				if fresh.AssetTag == asset.AssetTag {
					t.Fatal("ID reuse did not change asset")
				}
			}
		}
	}
	// Controlled retained-header corruption: all strings fit VARCHAR(64),
	// and explicit stored-byte assertions forbid a truncation/padding fixture.
	// The endpoint reports metadata only; it must not borrow a different key's
	// deletion marker, including when ordinary MySQL collation ignores padding.
	for _, kind := range []string{"case_changed", "padding_changed"} {
		asset := model.Node{Name: kind, Config: "{}", IsEnabled: true}
		if err := a.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		id := saveObservation(t, a, observationSource(asset, kind, "runtime", 1, false))
		headerTag, otherTag := strings.Repeat("a", 64), strings.Repeat("A", 64)
		if asset.AssetTag == headerTag {
			headerTag = strings.Repeat("b", 64)
			otherTag = strings.Repeat("B", 64)
		}
		if kind == "padding_changed" {
			headerTag = strings.Repeat("a", 63) + " "
			otherTag = strings.Repeat("a", 63)
		}
		if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"asset_tag": headerTag, "status": "held", "available_at": at}).Error; err != nil {
			t.Fatal(err)
		}
		wrong := model.NodeObservationState{NodeID: asset.ID, AssetTag: otherTag, Deleted: true, CurrentSource: "deleted", UpdatedAt: at}
		if err := a.Create(&wrong).Error; err != nil {
			t.Fatal(err)
		}
		var savedHeader model.NodeObservationIntent
		var savedState model.NodeObservationState
		if err := b.First(&savedHeader, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if err := b.Where("node_id = ? AND asset_tag = ?", asset.ID, otherTag).First(&savedState).Error; err != nil {
			t.Fatal(err)
		}
		if savedHeader.AssetTag != headerTag || savedState.AssetTag != otherTag {
			t.Fatal("fixture storage changed identity bytes")
		}
		ids = append(ids, id)
		relations[id] = "different_asset"
	}
	// These are large/corrupt HEADER diagnostics, not a claim of material bytes
	// validated or fetched. Physical SQL scalars must survive the DTO unchanged.
	diagnostic := map[string]string{ids[0]: "9007199254740993"}
	if a.Dialector.Name() == "mysql" {
		diagnostic[ids[1]] = "18446744073709551615"
	}
	for id, text := range diagnostic {
		if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"attempts": text, "material_bytes": text, "part_count": text}).Error; err != nil {
			t.Fatal(err)
		}
		var persisted model.NodeObservationIntent
		if err := b.First(&persisted, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if strconv.FormatUint(persisted.Attempts, 10) != text || strconv.FormatUint(persisted.MaterialBytes, 10) != text || strconv.FormatUint(persisted.PartCount, 10) != text {
			t.Fatal("physical diagnostic scalar changed")
		}
	}
	pendingID := saveObservation(t, a, observationSource(node, "pending", "runtime", 9, false))
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", pendingID).Update("available_at", at).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"claimed", "applied"} {
		id := saveObservation(t, a, observationSource(node, status, "runtime", 11, false))
		if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"status": status, "available_at": at, "lease_until": at.Add(time.Hour), "token": "private-claim-token"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	account := model.WalletAccount{UserID: 1, Currency: "CNY", BalanceMinor: 12345, Revision: 1}
	if err := a.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := func(db *gorm.DB) []byte {
		var headers []model.NodeObservationIntent
		var parts []model.NodeObservationPart
		var states []model.NodeObservationState
		var nodes []model.Node
		var wallets []model.WalletAccount
		for _, entry := range []struct {
			dest  any
			order string
		}{{&headers, "id"}, {&parts, "intent_id,ordinal"}, {&states, "node_id,asset_tag"}, {&nodes, "id"}, {&wallets, "user_id,currency"}} {
			if err := db.Order(entry.order).Find(entry.dest).Error; err != nil {
				t.Fatal(err)
			}
		}
		counts := map[string]int64{}
		for _, table := range []string{"orders", "subscriptions", "traffic_records", "wallet_accounts", "wallet_entries", "audit_logs"} {
			var n int64
			if err := db.Table(table).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			counts[table] = n
		}
		raw, err := json.Marshal([]any{headers, parts, states, nodes, wallets, counts})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot(b)
	var observed []string
	const hook = "test:observation_ops_headers"
	inspect := func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		observed = append(observed, sql)
		if strings.Contains(strings.ToLower(sql), "node_observation_parts") {
			t.Error("metadata read queried retained parts")
		}
	}
	if err := a.Callback().Row().After("gorm:row").Register(hook, inspect); err != nil {
		t.Fatal(err)
	}
	if err := a.Callback().Query().After("gorm:query").Register(hook, inspect); err != nil {
		t.Fatal(err)
	}
	removed := false
	remove := func() {
		if removed {
			return
		}
		removed = true
		_ = a.Callback().Row().Remove(hook)
		_ = a.Callback().Query().Remove(hook)
	}
	defer remove()
	var got []string
	cursor := ""
	for {
		page, err := service.List(ctx, 1, network.ObservationIntentQuery{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if page.Status != "held" || len(page.Items) > 2 {
			t.Fatal(page)
		}
		raw, _ := json.Marshal(page)
		for _, secret := range []string{"private-claim-token", node.AssetTag, "private-principal", "private-flow", "private-source", "private-payload", "wire-core-private", "replacement-not-original"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("metadata response leaked retained/private source")
			}
		}
		for _, item := range page.Items {
			got = append(got, item.ID)
			if text, ok := diagnostic[item.ID]; ok && (item.Attempts != text || item.MaterialBytes != text || item.PartCount != text) {
				t.Fatal("header diagnostics lost decimal precision", item)
			}
			if item.MaterialVerified || item.LeaseUntil != nil || !item.AvailableAt.Equal(at) {
				t.Fatal("metadata claimed material verification or lost scheduling precision", item)
			}
			if relation, ok := relations[item.ID]; ok && item.AssetRelation != relation {
				t.Fatal(item.AssetRelation, relation)
			}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("terminal cursor")
			}
			break
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			t.Fatal("cursor did not progress")
		}
		cursor = page.NextCursor
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(got, ids) {
		t.Fatal("same-time keyset omitted/duplicated sources", got, ids)
	}
	pending, err := service.List(ctx, 1, network.ObservationIntentQuery{Status: "pending", Limit: 100})
	if err != nil || len(pending.Items) != 1 || pending.Items[0].ID != pendingID {
		t.Fatal(pending, err)
	}
	claimed, err := service.List(ctx, 1, network.ObservationIntentQuery{Status: "claimed"})
	if err != nil || len(claimed.Items) != 1 || claimed.Items[0].LeaseUntil == nil || !claimed.Items[0].LeaseUntil.Equal(at.Add(time.Hour)) {
		t.Fatal("claimed lease metadata", claimed, err)
	}
	applied, err := service.List(ctx, 1, network.ObservationIntentQuery{Status: "applied"})
	if err != nil || len(applied.Items) != 1 || applied.Items[0].LeaseUntil != nil {
		t.Fatal("applied metadata", applied, err)
	}
	if len(observed) == 0 {
		t.Fatal("SQL observation hook never ran")
	}
	for _, sql := range observed {
		if strings.Contains(strings.ToUpper(sql), "COUNT(") || strings.Contains(strings.ToUpper(sql), "OFFSET") {
			t.Fatal("unbounded pagination metadata", sql)
		}
	}
	remove()
	if after := snapshot(b); !bytes.Equal(before, after) {
		t.Fatal("queue read changed retained materials, states, node counters or financial records")
	}
	// A material corruption is a retained fact; listing it is not replay/repair.
	for _, change := range []map[string]any{{"is_admin": false}, {"is_admin": true, "status": "suspended"}} {
		if err := b.Model(&model.User{}).Where("id = 1").Updates(change).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.List(ctx, 1, network.ObservationIntentQuery{}); !errors.Is(err, network.ErrNodeAdministrationPermission) {
			t.Fatal("stale administrator accepted", err)
		}
	}
	if err := b.Model(&model.User{}).Where("id = 1").Updates(map[string]any{"is_admin": true, "status": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("observation metadata unavailable")
	const failure = "test:observation_ops_query_failure"
	if err := a.Callback().Row().Before("gorm:row").Register(failure, func(tx *gorm.DB) {
		if tx.Statement.TableExpr != nil && strings.Contains(tx.Statement.TableExpr.SQL, "node_observation_intents") {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer a.Callback().Row().Remove(failure)
	if page, err := service.List(ctx, 1, network.ObservationIntentQuery{}); !errors.Is(err, injected) || len(page.Items) != 0 {
		t.Fatal("SQL error became an empty successful page", page, err)
	}
}
