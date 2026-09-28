package navigation_test

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/navigation"
)

func TestPageAvailabilityFollowsHiddenTreeAndDetailRoutes(t *testing.T) {
	nodes := []navigation.Node{
		{ID: "resources", Owner: "core", Hidden: true},
		{ID: "nodes", Owner: "core", ParentID: "resources", Path: "/admin/nodes"},
		{ID: "templates", Owner: "core", Path: "/admin/templates", Hidden: true},
		{ID: "rules", Owner: "core", Path: "/admin/templates/rules"},
		{ID: "alias", Owner: "custom", Path: "/admin/nodes"},
		{ID: "home", Owner: "core", Path: "/"},
	}
	visible := navigation.Visible(nodes, func(n navigation.Node) bool { return true })
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/admin/nodes", false}, {"/admin/nodes/42", false}, {"/admin/nodes/", false},
		{"/admin/%6eodes", false}, {"/admin/nodes?tab=details", false},
		{"/admin/nodes-other", true}, {"/admin/templates", false},
		{"/admin/templates/rules", true}, {"/", true}, {"/login", true},
	} {
		if got := navigation.PageAvailable(nodes, visible, tc.path); got != tc.want {
			t.Errorf("PageAvailable(%q) = %v; want %v", tc.path, got, tc.want)
		}
	}
}
