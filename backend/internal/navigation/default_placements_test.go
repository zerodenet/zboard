package navigation_test

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
)

func TestDefaultAdminModulesConvergeWithoutRemovingCorePages(t *testing.T) {
	db := fixture(t)
	snapshot := read(t, db, "admin")
	visible := navigation.Visible(snapshot.Nodes, func(n navigation.Node) bool { return true })
	roots := map[string]string{}
	paths := map[string]bool{}
	for _, node := range visible {
		if node.ParentID == "" {
			roots[node.ID] = node.Label
		}
		if node.Path != "" {
			paths[node.Path] = true
		}
	}
	for id, label := range map[string]string{"customers": "用户系统", "commerce": "订阅系统", "infrastructure": "节点系统", "settings": "服务系统"} {
		if roots[id] != label {
			t.Fatalf("module %s = %q", id, roots[id])
		}
	}
	if len(roots) != 5 {
		t.Fatalf("expected overview and four modules: %+v", roots)
	}
	for _, path := range []string{"/admin/users", "/admin/subscriptions", "/admin/plans", "/admin/orders", "/admin/protocols", "/admin/node-groups", "/admin/providers", "/admin/dns-records", "/admin/certificates", "/admin/tasks", "/admin/runtime-jobs"} {
		if !paths[path] {
			t.Fatalf("lost existing page %s", path)
		}
	}
}

func TestDefaultPlacementUpgradePreservesOperatorAndPluginMenus(t *testing.T) {
	db := fixture(t)
	// Simulate an existing installation with untouched old defaults plus edits.
	for id, fields := range map[string]map[string]any{
		"customers":                  {"label": "用户与订阅"},
		"commerce":                   {"label": "商品与订单"},
		"admin:/admin/subscriptions": {"parent_id": "customers.service", "position": 10},
		"admin:/admin/protocols":     {"label": "我的协议", "hidden": true},
		"settings":                   {"label": "设置", "position": 99},
	} {
		if err := db.Model(&model.MenuNode{}).Where("id = ?", id).Updates(fields).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.MenuNode{ID: "plugin:retained", Surface: "admin", Owner: "plugin", ParentID: "customers.service", Label: "插件页", Path: "/admin/extensions/example/home", PluginID: "example", PageID: "home"}).Error; err != nil {
		t.Fatal(err)
	}
	previous := read(t, db, "admin").Revision
	if err := menustore.Seed(db); err != nil {
		t.Fatal(err)
	}
	snapshot := read(t, db, "admin")
	if snapshot.Revision != previous+1 {
		t.Fatalf("upgrade revision=%d", snapshot.Revision)
	}
	byID := map[string]navigation.Node{}
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	if byID["customers"].Label != "用户系统" || byID["admin:/admin/subscriptions"].ParentID != "commerce.subscriptions" {
		t.Fatal("default placements did not converge")
	}
	if byID["admin:/admin/protocols"].Label != "我的协议" || !byID["admin:/admin/protocols"].Hidden || byID["settings"].Position != 99 || byID["settings"].Label != "设置" {
		t.Fatal("operator edits overwritten")
	}
	if byID["plugin:retained"].ParentID != "customers.service" {
		t.Fatal("plugin placement changed")
	}
	if err := menustore.Seed(db); err != nil {
		t.Fatal(err)
	}
	if read(t, db, "admin").Revision != snapshot.Revision {
		t.Fatal("restart repeated upgrade")
	}
}

func TestPlanMenuRenamePreservesPresentationEdits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits map[string]any
		want  string
	}{
		{"untouched", nil, "套餐管理"},
		{"custom label", map[string]any{"label": "我的商品"}, "我的商品"},
		{"hidden", map[string]any{"hidden": true}, "商品与套餐"},
		{"moved", map[string]any{"position": 99}, "商品与套餐"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := fixture(t)
			fields := map[string]any{"label": "商品与套餐"}
			for key, value := range tc.edits {
				fields[key] = value
			}
			if err := db.Model(&model.MenuNode{}).Where("id = ?", "admin:/admin/plans").Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			before := read(t, db, "admin").Revision
			if err := menustore.Seed(db); err != nil {
				t.Fatal(err)
			}
			var node model.MenuNode
			if err := db.First(&node, "id = ?", "admin:/admin/plans").Error; err != nil {
				t.Fatal(err)
			}
			if node.Label != tc.want || node.Path != "/admin/plans" || node.ID != "admin:/admin/plans" {
				t.Fatalf("unexpected menu %+v", node)
			}
			if tc.name == "hidden" && !node.Hidden {
				t.Fatal("visibility overwritten")
			}
			if tc.name == "moved" && node.Position != 99 {
				t.Fatal("position overwritten")
			}
			wantRevision := before
			if tc.edits == nil {
				wantRevision++
			}
			if read(t, db, "admin").Revision != wantRevision {
				t.Fatal("unexpected revision")
			}
			if err := menustore.Seed(db); err != nil {
				t.Fatal(err)
			}
			if read(t, db, "admin").Revision != wantRevision {
				t.Fatal("rename repeated on restart")
			}
		})
	}
}
