package navigation

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
)

// Workspace changes presentation only. Its entries are drawn from the same
// permission-filtered menu tree; it never grants page or plugin access.
type Workspace struct {
	GroupID   string `json:"group_id"`
	EntryPath string `json:"entry_path,omitempty"`
	Kind      string `json:"kind"`
}

type Presentation struct {
	Workspaces []Workspace `json:"workspaces"`
	FooterIDs  []string    `json:"footer_ids,omitempty"`
}

//go:embed presentation.json
var presentationDefaultsJSON []byte

type footerPlacement struct {
	NodeID   string `json:"node_id"`
	ParentID string `json:"parent_id"`
	Position int    `json:"position"`
}
type presentationDefinition struct {
	Workspaces []Workspace       `json:"workspaces"`
	Footer     []footerPlacement `json:"footer"`
}

var presentationDefaults = func() map[string]presentationDefinition {
	var out map[string]presentationDefinition
	if err := json.Unmarshal(presentationDefaultsJSON, &out); err != nil {
		panic(err)
	}
	return out
}()

func WorkspacePath(surface, id string) string {
	return "/" + surface + "/workspaces/" + url.PathEscape(id)
}

// PresentationFor is server-owned layout metadata. Operator names, placement,
// ordering and authorized plugin contributions still come from visible nodes.
func PresentationFor(surface string, visible []Node) Presentation {
	definition := presentationDefaults[surface]
	byID := map[string]Node{}
	for _, n := range visible {
		byID[n.ID] = n
	}
	out := Presentation{Workspaces: []Workspace{}}
	for _, placement := range definition.Footer {
		if n, ok := byID[placement.NodeID]; ok && n.Surface == surface && n.Path != "" && n.ParentID == placement.ParentID && n.Position == placement.Position {
			out.FooterIDs = append(out.FooterIDs, n.ID)
		}
	}
	for _, d := range definition.Workspaces {
		group, exists := byID[d.GroupID]
		if !exists || group.Path != "" {
			continue
		}
		found := d.Kind == "hub" || d.EntryPath == ""
		for _, n := range visible {
			if n.Path == d.EntryPath && descendsFrom(n, d.GroupID, byID) {
				found = true
				break
			}
		}
		if !found {
			// Hiding or moving the former entry must not reopen it, or hide the
			// remaining authorized pages in this workspace.
			if surface == "admin" {
				d.EntryPath, d.Kind = WorkspacePath(surface, d.GroupID), "hub"
			} else {
				d.EntryPath, d.Kind = "", "menu"
			}
		}
		out.Workspaces = append(out.Workspaces, d)
	}
	return out
}

func descendsFrom(n Node, parent string, nodes map[string]Node) bool {
	for depth := 0; n.ParentID != "" && depth < 8; depth++ {
		if n.ParentID == parent {
			return true
		}
		var found bool
		n, found = nodes[n.ParentID]
		if !found {
			return false
		}
	}
	return false
}

// WorkspaceAvailable recognizes only an exact visible group. Unknown/deleted
// groups fail closed rather than inheriting unregistered-route availability.
func WorkspaceAvailable(surface string, visible []Node, path string) (bool, bool) {
	parsed, err := url.Parse(path)
	if err != nil {
		return false, true
	}
	path = parsed.Path
	prefix := "/" + surface + "/workspaces/"
	if !strings.HasPrefix(path, prefix) {
		return false, false
	}
	id, err := url.PathUnescape(strings.TrimPrefix(path, prefix))
	if err != nil || !nodeIDPattern.MatchString(id) {
		return false, true
	}
	for _, n := range visible {
		if n.ID == id && n.Path == "" && n.Surface == surface {
			return true, true
		}
	}
	return false, true
}
