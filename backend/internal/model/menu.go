package model

// MenuNode is host-owned navigation metadata. An empty Path is a group.
// Owner and plugin references cannot be changed by menu editors.
type MenuNode struct {
	ID        string `gorm:"primaryKey;size:191" json:"id"`
	ParentID  string `gorm:"size:191;not null" json:"parent_id"`
	Surface   string `gorm:"size:16;not null;index" json:"surface"`
	Label     string `gorm:"size:160;not null" json:"label"`
	Icon      string `gorm:"size:64;not null" json:"icon"`
	Path      string `gorm:"size:512;not null" json:"path"`
	Position  int    `gorm:"not null" json:"position"`
	Hidden    bool   `gorm:"not null" json:"hidden"`
	Owner     string `gorm:"size:16;not null" json:"owner"`
	PluginID  string `gorm:"size:160;not null;index" json:"plugin_id"`
	PageID    string `gorm:"size:64;not null" json:"page_id"`
	Condition string `gorm:"size:24;not null" json:"condition"`
}

type MenuRevision struct {
	Surface  string `gorm:"primaryKey;size:16" json:"surface"`
	Revision uint64 `gorm:"not null" json:"revision"`
}
