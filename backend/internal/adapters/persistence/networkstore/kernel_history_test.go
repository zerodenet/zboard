package networkstore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestKernelHistoryRechecksAuthorityAndReturnsBoundedNewestOperations(t *testing.T) {
	store, admin, node := kernelDetectionFixture(t)
	for index := 0; index < 3; index++ {
		row := model.NodeOperation{NodeID: node.ID, OperationType: "detect", Status: "succeeded", Phase: "completed", RequestedBy: admin.ID}
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	history := KernelHistory{DB: store.DB}
	result, err := history.ReadKernelHistory(context.Background(), network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, 2)
	if err != nil || len(result.Operations) != 2 || result.Operations[0].ID <= result.Operations[1].ID || result.State.NodeID != node.ID {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := store.DB.Model(&model.User{}).Where("id = ?", admin.ID).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := history.ReadKernelHistory(context.Background(), network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, 2); !errors.Is(err, network.ErrKernelPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
}
