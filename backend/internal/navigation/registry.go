// Package navigation defines the host menu contract and pure tree operations.
// Persistence and installation transactions are owned by the menu store.
package navigation

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var ErrConflict = errors.New("menu changed; refresh and retry")
var ErrInvalid = errors.New("invalid menu")
var ErrPermission = errors.New("menu operation requires a current administrator")
var nodeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:._/-]{0,190}$`)
var iconPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{0,64}$`)

type Node struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"`
	Surface   string `json:"surface"`
	Label     string `json:"label"`
	Icon      string `json:"icon"`
	Path      string `json:"path"`
	Position  int    `json:"position"`
	Hidden    bool   `json:"hidden"`
	Owner     string `json:"owner"`
	PluginID  string `json:"plugin_id"`
	PageID    string `json:"page_id"`
	Condition string `json:"condition"`
}

type Snapshot struct {
	Revision uint64 `json:"revision"`
	Nodes    []Node `json:"nodes"`
}

func SurfaceValid(surface string) bool {
	return surface == "public" || surface == "account" || surface == "admin"
}

// Visible prunes hidden/inaccessible nodes, their descendants and empty groups.
// allow receives source metadata, so authentication and capability checks are
// supplied by the host rather than invented by the menu registry.
func Visible(nodes []Node, allow func(Node) bool) []Node {
	children := map[string][]Node{}
	for _, n := range nodes {
		children[n.ParentID] = append(children[n.ParentID], n)
	}
	var walk func(string, int) []Node
	walk = func(parent string, depth int) []Node {
		out := []Node{}
		if depth > 8 {
			return out
		}
		for _, n := range children[parent] {
			if n.Hidden || !allow(n) {
				continue
			}
			below := walk(n.ID, depth+1)
			if n.Path == "" && len(below) == 0 {
				continue
			}
			out = append(out, n)
			out = append(out, below...)
		}
		return out
	}
	return walk("", 0)
}

func ValidPath(value string) bool {
	u, err := url.Parse(value)
	return err == nil && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\\\r\n\x00") && u.IsAbs() == false && u.Host == "" && !strings.Contains(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n\x00")
}

func Validate(surface string, nodes []Node) error {
	if len(nodes) > 512 {
		return fmt.Errorf("%w: too many nodes", ErrInvalid)
	}
	byID := map[string]Node{}
	for _, n := range nodes {
		if !nodeIDPattern.MatchString(n.ID) || n.Surface != surface || strings.TrimSpace(n.Label) == "" || len(n.Label) > 160 || !iconPattern.MatchString(n.Icon) || (n.Path != "" && (!ValidPath(n.Path) || len(n.Path) > 512)) || n.Position < -100000 || n.Position > 100000 {
			return fmt.Errorf("%w: invalid node %s", ErrInvalid, n.ID)
		}
		if _, ok := byID[n.ID]; ok {
			return fmt.Errorf("%w: duplicate id", ErrInvalid)
		}
		byID[n.ID] = n
	}
	for _, n := range nodes {
		parent := n.ParentID
		for depth := 0; parent != ""; depth++ {
			p, ok := byID[parent]
			if !ok || p.Path != "" || parent == n.ID || depth >= 8 {
				return fmt.Errorf("%w: invalid parent or cycle for %s", ErrInvalid, n.ID)
			}
			parent = p.ParentID
		}
	}
	return nil
}
