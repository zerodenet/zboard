package handler

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPrepareNodeGroupReconcileTaskOrdersCredentialPhaseBeforeNodes(t *testing.T) {
	task, items, err := prepareNodeGroupReconcileTask(
		authClaims{UserID: 7, Email: "admin@example.test", IsAdmin: true},
		12,
		4,
		[]nodeGroupPublishTarget{
			{NodeID: 31, EndpointID: 301},
			{NodeID: 18, EndpointID: 180},
			{NodeID: 31, EndpointID: 302},
			{},
		},
	)
	if err != nil {
		t.Fatalf("prepareNodeGroupReconcileTask() error = %v", err)
	}
	if task.Type != taskTypeNodeGroupSync || task.IdempotencyKey != "node-group-reconcile:12:4" {
		t.Fatalf("task identity = type %q key %q", task.Type, task.IdempotencyKey)
	}
	if task.Total != 3 || len(items) != 3 {
		t.Fatalf("task targets = total %d items %d, want 3", task.Total, len(items))
	}
	if items[0].TargetType != "node_group" || items[0].TargetID != "12" {
		t.Fatalf("first task item = %+v, want node-group credential reconciliation", items[0])
	}
	if items[1].TargetType != "node" || items[1].TargetID != "31" || items[2].TargetID != "18" {
		t.Fatalf("node task items = %+v, want deduplicated nodes in stable order", items[1:])
	}
	if isOperationTaskType(task.Type) {
		t.Fatal("node-group reconciliation must remain sequential so credentials finish before node publication")
	}

	var scope nodeGroupReconcileScope
	if err := json.Unmarshal([]byte(task.Scope), &scope); err != nil {
		t.Fatalf("decode scope: %v", err)
	}
	if scope.NodeGroupID != 12 || !reflect.DeepEqual(scope.NodeIDs, []uint{31, 18}) {
		t.Fatalf("scope = %+v", scope)
	}
	var content operationTaskContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if content.NodeGroupID != 12 || content.RequestedBy != 7 || content.Actor != "admin@example.test" {
		t.Fatalf("content = %+v", content)
	}
	if !reflect.DeepEqual(content.EndpointIDsByNode, map[string][]uint{"31": {302}, "18": {180}}) {
		t.Fatalf("endpoint trigger map = %+v", content.EndpointIDsByNode)
	}
}

func TestMergeNodeGroupPublishTargetsPreservesPreviousNodesAndPrefersCurrentTrigger(t *testing.T) {
	got := mergeNodeGroupPublishTargets(
		[]nodeGroupPublishTarget{{NodeID: 9, EndpointID: 90}, {NodeID: 4, EndpointID: 40}},
		[]nodeGroupPublishTarget{{NodeID: 4, EndpointID: 41}, {NodeID: 7, EndpointID: 70}},
	)
	want := []nodeGroupPublishTarget{{NodeID: 9, EndpointID: 90}, {NodeID: 4, EndpointID: 41}, {NodeID: 7, EndpointID: 70}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeNodeGroupPublishTargets() = %v, want %v", got, want)
	}
}
