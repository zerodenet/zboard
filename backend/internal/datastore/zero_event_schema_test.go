package datastore

import (
	"strings"
	"testing"
)

func TestZeroEventNodeCursorTableNameIsStable(t *testing.T) {
	if strings.TrimSpace(zeroEventNodeCursorsTable) != "zero_event_node_cursors" {
		t.Fatalf("unexpected Zero event cursor table %q", zeroEventNodeCursorsTable)
	}
}
