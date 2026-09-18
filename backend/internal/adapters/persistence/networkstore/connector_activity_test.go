package networkstore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestConnectorActivityReadsCurrentNodeObservation(t *testing.T) {
	db, _ := administrationFixture(t)
	observed := time.Now().UTC().Truncate(time.Millisecond)
	node := model.Node{ID: 1, Name: "connector-observation", LifecycleStatus: "active", ConnectorLastSeenAt: &observed}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	actual, err := (ConnectorActivity{DB: db}).ReadConnectorLastSeen(context.Background(), node.ID)
	if err != nil || actual == nil || !actual.Equal(observed) {
		t.Fatalf("actual=%v err=%v", actual, err)
	}
}
