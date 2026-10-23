package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"gorm.io/gorm"
)

// NewNodeAssetTag establishes only a future panel-asset label. Never use it to
// reconstruct the identity of an already accepted or replayed event.
func NewNodeAssetTag() (string, error) { return nodeAssetTag(rand.Reader) }

func nodeAssetTag(random io.Reader) (string, error) {
	var raw [32]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", fmt.Errorf("generate node asset label: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// New ORM assets always receive a server-generated label. Controlled map-based
// database copy deliberately bypasses this hook and preserves current labels.
func (node *Node) BeforeCreate(_ *gorm.DB) error {
	tag, err := NewNodeAssetTag()
	if err != nil {
		return err
	}
	node.AssetTag = tag
	return nil
}
