package navigation

import (
	"crypto/sha256"
	"fmt"
)

// Placement is optional page registration metadata, not a UI slot name.
type Placement struct {
	ParentID string `json:"parent_id,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Position int    `json:"position,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

func (p Placement) Valid() bool {
	return (p.ParentID == "" || nodeIDPattern.MatchString(p.ParentID)) && iconPattern.MatchString(p.Icon) && p.Position >= -100000 && p.Position <= 100000
}

type PageRegistration struct {
	ID, Surface, Title string
	Menu               *Placement
}

func PluginNodeID(plugin, surface, page string) string {
	return fmt.Sprintf("plugin:%x", sha256.Sum256([]byte(plugin+":"+surface+":"+page)))
}
