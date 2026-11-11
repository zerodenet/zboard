package navigation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
)

func TestWorkspaceLayoutPreservesAllowedPagesWhenPrimaryIsHiddenOrMoved(t *testing.T) {
	nodes := []navigation.Node{
		{ID: "settings", Surface: "admin"},
		{ID: "settings.extensions", ParentID: "settings", Surface: "admin"},
		{ID: "market", ParentID: "settings.extensions", Surface: "admin", Path: "/admin/plugin-market"},
	}
	for _, moved := range []bool{false, true} {
		input := append([]navigation.Node(nil), nodes...)
		if moved {
			input = append(input, navigation.Node{ID: "plugins", ParentID: "settings", Surface: "admin", Path: "/admin/plugins"})
		}
		p := navigation.PresentationFor("admin", input)
		if len(p.Workspaces) != 1 || p.Workspaces[0].Kind != "hub" || p.Workspaces[0].EntryPath != "/admin/workspaces/settings.extensions" {
			t.Fatalf("missing or moved primary reopened: %+v", p)
		}
	}
	if p := navigation.PresentationFor("admin", nil); len(p.Workspaces) != 0 {
		t.Fatalf("unavailable groups leaked: %+v", p)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/admin/workspaces/settings.extensions", true},
		{"/admin/workspaces/settings.extensions?q=x", true},
		{"/admin/workspaces/settings.site", false},
		{"/admin/workspaces/settings.extensions/unknown", false},
	} {
		available, recognized := navigation.WorkspaceAvailable("admin", nodes, tc.path)
		if !recognized || available != tc.want {
			t.Fatalf("%s: %v %v", tc.path, available, recognized)
		}
	}
}

func TestWorkspaceUpgradePreservesOperatorEditsAndAdvancesEachSurfaceOnce(t *testing.T) {
	db := fixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "adapters", "persistence", "menustore", "workspace_legacy_defaults.json"))
	if err != nil {
		t.Fatal(err)
	}
	var old []model.MenuNode
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1=1").Delete(&model.MenuNode{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MenuNode{}).Where("id = ?", "admin:/admin/settings/email").Update("label", "我的邮件配置").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MenuNode{}).Where("id = ?", "account:/account/wallet").Update("position", 99).Error; err != nil {
		t.Fatal(err)
	}
	before := map[string]uint64{}
	for _, surface := range []string{"admin", "account", "public"} {
		before[surface] = read(t, db, surface).Revision
	}
	if err := menustore.Seed(db); err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{"admin", "account", "public"} {
		want := before[surface]
		if surface != "public" {
			want++
		}
		if read(t, db, surface).Revision != want {
			t.Fatalf("%s revision did not converge once", surface)
		}
	}
	var email, wallet model.MenuNode
	db.First(&email, "id = ?", "admin:/admin/settings/email")
	db.First(&wallet, "id = ?", "account:/account/wallet")
	if email.Label != "我的邮件配置" || wallet.Position != 99 || wallet.ParentID != "" {
		t.Fatalf("operator edit overwritten: %+v %+v", email, wallet)
	}
	var migrated model.MenuNode
	db.First(&migrated, "id = ?", "account:/account/orders")
	if migrated.ParentID != "account.billing" {
		t.Fatalf("default not migrated: %+v", migrated)
	}
	if err := menustore.Seed(db); err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{"admin", "account", "public"} {
		want := before[surface]
		if surface != "public" {
			want++
		}
		if read(t, db, surface).Revision != want {
			t.Fatalf("%s revision bumped again", surface)
		}
	}
}

func TestFooterPlacementUsesOnlyVisibleUnmovedNode(t *testing.T) {
	about := navigation.Node{ID: "admin:/admin/about", ParentID: "settings.system", Surface: "admin", Path: "/admin/about", Label: "实例信息", Position: 20}
	p := navigation.PresentationFor("admin", []navigation.Node{about})
	if len(p.FooterIDs) != 1 || p.FooterIDs[0] != about.ID {
		t.Fatalf("missing footer placement: %+v", p)
	}
	if p := navigation.PresentationFor("admin", nil); len(p.FooterIDs) != 0 {
		t.Fatalf("hidden page reopened: %+v", p)
	}
	about.Position = 99
	if p := navigation.PresentationFor("admin", []navigation.Node{about}); len(p.FooterIDs) != 0 {
		t.Fatalf("operator order ignored: %+v", p)
	}
	about.Position = 20
	about.ParentID = "operator.group"
	if p := navigation.PresentationFor("admin", []navigation.Node{about}); len(p.FooterIDs) != 0 {
		t.Fatalf("operator placement ignored: %+v", p)
	}
	about.ParentID, about.Surface = "settings.system", "account"
	if p := navigation.PresentationFor("admin", []navigation.Node{about}); len(p.FooterIDs) != 0 {
		t.Fatalf("cross-surface link: %+v", p)
	}
}
