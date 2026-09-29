package networkstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Always exercise the production file-backed SQLite driver, even when an
// optional MySQL compatibility DSN is present in the test environment.
func seedSQLiteServices(tb testing.TB, serviceCount int) *gorm.DB {
	tb.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(tb.TempDir(), "services.db"))
	if err != nil {
		tb.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = pool.Close() })
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	if err := datastore.RunMigrations(db); err != nil {
		tb.Fatal(err)
	}
	node := model.Node{Name: "SQLite server", Address: "127.0.0.1", Config: "{}", IsEnabled: true}
	group := model.NodeGroup{Name: "SQLite group", Code: "sqlite", IsEnabled: true}
	for _, row := range []any{&node, &group} {
		if err := db.Create(row).Error; err != nil {
			tb.Fatal(err)
		}
	}
	now := time.Now().UTC()
	endpoints := make([]model.ProtocolEndpoint, serviceCount/2)
	for i := range endpoints {
		endpoints[i] = model.ProtocolEndpoint{NodeID: node.ID, Name: fmt.Sprintf("Protocol %05d", i), RuntimeKey: fmt.Sprintf("protocol-%d", i), Protocol: "vless", Address: "parent.example.test", Port: 10000 + i, PublicPort: 10000 + i, IsActive: true, SortOrder: i}
	}
	if err := db.CreateInBatches(&endpoints, 250).Error; err != nil {
		tb.Fatal(err)
	}
	entries := make([]model.NetworkEntry, len(endpoints))
	direct := make([]model.NodeGroupEndpoint, len(endpoints))
	deployments := make([]model.ProtocolDeployment, 0, len(endpoints)*2)
	traffic := make([]model.ProtocolEndpointUsageDaily, 0, len(endpoints)*30)
	for i, endpoint := range endpoints {
		entries[i] = model.NetworkEntry{DeploymentMode: "external", EndpointID: endpoint.ID, Name: fmt.Sprintf("Forward %05d", i), Address: "entry.example.test", Port: 10000 + i, PublicPort: 10000 + i, Network: "tcp_udp", Enabled: true}
		direct[i] = model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID, SortOrder: i}
		for _, status := range []string{"failed", "succeeded"} {
			deployments = append(deployments, model.ProtocolDeployment{NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ConfigRevision: 1, Status: status})
		}
		for day := 0; day < 30; day++ {
			traffic = append(traffic, model.ProtocolEndpointUsageDaily{ProtocolEndpointID: endpoint.ID, UsageDate: now.AddDate(0, 0, -day).Format("2006-01-02"), UsedBytes: 1024, LastRecordAt: now, UpdatedAt: now})
		}
	}
	if err := db.Omit("node_id").CreateInBatches(&entries, 250).Error; err != nil {
		tb.Fatal(err)
	}
	fronts := make([]model.NodeGroupNetworkEntry, len(entries))
	for i, entry := range entries {
		fronts[i] = model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID, SortOrder: i}
	}
	for _, rows := range []any{&direct, &fronts, &deployments, &traffic} {
		if err := db.CreateInBatches(rows, 500).Error; err != nil {
			tb.Fatal(err)
		}
	}
	return db
}

func sqliteServiceInventory(tb testing.TB, writer *gorm.DB) Inventory {
	tb.Helper()
	reader, closeReader, err := datastore.OpenReadView(writer)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = closeReader() })
	return Inventory{DB: writer, ReadDatabase: func() *gorm.DB { return reader }}
}

func TestSQLiteProtocolServicesDoNotWaitOnTheWriterConnection(t *testing.T) {
	db := seedSQLiteServices(t, 100)
	inventory := sqliteServiceInventory(t, db)
	writer := db.Begin()
	if writer.Error != nil {
		t.Fatal(writer.Error)
	}
	defer writer.Rollback()
	if err := writer.Model(&model.Node{}).Where("id = 1").Update("name", "uncommitted").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	page, err := inventory.ListProtocolEndpoints(ctx, network.ProtocolEndpointInventoryQuery{ServiceKind: "all", Paged: true, Limit: 50, IncludeStatusFacets: true, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("read waited for the occupied writer: %v", err)
	}
	if len(page.Items) != 50 {
		t.Fatalf("items=%d", len(page.Items))
	}
	if page.Items[0].Node.Name != "SQLite server" {
		t.Fatalf("read did not retain the committed WAL view: %+v", page.Items[0].Node)
	}
}

type servicePageCounter struct {
	logger.Interface
	mu      sync.Mutex
	queries int
	maxRows int64
	slowest time.Duration
	slowSQL string
}

func (c *servicePageCounter) Trace(_ context.Context, begin time.Time, sql func() (string, int64), _ error) {
	statement, rows := sql()
	elapsed := time.Since(begin)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries++
	if elapsed > c.slowest {
		c.slowest, c.slowSQL = elapsed, statement
	}
	if rows > c.maxRows {
		c.maxRows = rows
	}
}

func TestSQLiteProtocolServicePagingRemainsBoundedAtScale(t *testing.T) {
	var expectedQueries int
	for _, size := range []int{100, 10000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db := seedSQLiteServices(t, size)
			for _, offset := range []int{0, size - 50} {
				counter := &servicePageCounter{Interface: logger.Discard}
				inventory := sqliteServiceInventory(t, db.Session(&gorm.Session{Logger: counter}))
				counter.mu.Lock()
				counter.queries, counter.maxRows = 0, 0
				counter.mu.Unlock()
				query := network.ProtocolEndpointInventoryQuery{ServiceKind: "all", GroupID: 1, Paged: true, Limit: 50, Offset: offset, IncludeStatusFacets: true, Now: time.Now().UTC()}
				samples := make([]time.Duration, 0, 5)
				for i := 0; i < 5; i++ {
					started := time.Now()
					page, err := inventory.ListProtocolEndpoints(context.Background(), query)
					samples = append(samples, time.Since(started))
					if err != nil {
						t.Fatal(err)
					}
					if page.Total != int64(size) || len(page.Items) != 50 || page.Facets.Succeeded != int64(size) {
						t.Fatalf("invalid page: total=%d items=%d facets=%+v", page.Total, len(page.Items), page.Facets)
					}
					keys := map[string]bool{}
					for _, item := range page.Items {
						key := fmt.Sprintf("protocol:%d", item.Endpoint.ID)
						if item.Forward != nil {
							key = fmt.Sprintf("forward:%d", item.Forward.ID)
						}
						if keys[key] {
							t.Fatalf("duplicate typed identity %s", key)
						}
						keys[key] = true
						if item.Usage.UsedBytesTotal != 30*1024 {
							t.Fatalf("traffic decoration lost: %+v", item.Usage)
						}
					}
				}
				queries := counter.queries / len(samples)
				if counter.maxRows > 50 || queries > 18 {
					t.Fatalf("unbounded read: queries=%d max rows=%d", queries, counter.maxRows)
				}
				if expectedQueries == 0 {
					expectedQueries = queries
				} else if queries != expectedQueries {
					t.Fatalf("queries grow with inventory: got=%d want=%d", queries, expectedQueries)
				}
				sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
				t.Logf("SQLite services=%d daily rows=%d offset=%d limit=50 queries/page=%d max rows/query=%d p50=%s max=%s (5 samples, local timing only)", size, size/2*30, offset, queries, counter.maxRows, samples[2], samples[4])
				if size == 10000 && offset == 0 {
					var plan []struct{ Detail string }
					if err := db.Raw("EXPLAIN QUERY PLAN " + counter.slowSQL).Scan(&plan).Error; err != nil {
						t.Fatal(err)
					}
					t.Logf("slowest query=%s duration=%s plan=%+v", counter.slowSQL[:min(90, len(counter.slowSQL))], counter.slowest, plan)
				}
			}
		})
	}
}

func TestSQLitePagedGroupsCountMembersWithoutMaterializingLinks(t *testing.T) {
	db := seedSQLiteServices(t, 10000)
	counter := &servicePageCounter{Interface: logger.Default.LogMode(logger.Silent)}
	store := Inventory{DB: db.Session(&gorm.Session{Logger: counter})}
	page, err := store.ListNodeGroups(context.Background(), network.NodeGroupInventoryQuery{Paged: true, Limit: 25})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("group page: %+v %v", page, err)
	}
	item := page.Items[0]
	if item.ProtocolEndpointCount != 5000 || item.NetworkEntryCount != 5000 || len(item.Group.ProtocolEndpointIDs) != 0 || len(item.Group.NetworkEntryIDs) != 0 {
		t.Fatalf("summary materialized members: %+v", item)
	}
	if counter.maxRows > 25 {
		t.Fatalf("group list read %d rows in one query", counter.maxRows)
	}
	ids, total, err := store.SelectProtocolEndpointIDs(context.Background(), network.ProtocolEndpointInventoryQuery{ServiceKind: "forward", GroupID: item.Group.ID})
	if err != nil || total != 5000 || len(ids) != 5000 {
		t.Fatalf("bounded forward snapshot: %d %d %v", total, len(ids), err)
	}
}

func BenchmarkSQLiteProtocolServicesPage(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			db := seedSQLiteServices(b, size)
			inventory := sqliteServiceInventory(b, db)
			for _, group := range []uint{0, 1} {
				for _, facets := range []bool{false, true} {
					b.Run(fmt.Sprintf("group=%d/facets=%t", group, facets), func(b *testing.B) {
						query := network.ProtocolEndpointInventoryQuery{ServiceKind: "all", GroupID: group, Paged: true, Limit: 50, IncludeStatusFacets: facets, Now: time.Now().UTC()}
						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							page, err := inventory.ListProtocolEndpoints(context.Background(), query)
							if err != nil || len(page.Items) != 50 {
								b.Fatalf("page=%d err=%v", len(page.Items), err)
							}
						}
					})
				}
			}
		})
	}
}
