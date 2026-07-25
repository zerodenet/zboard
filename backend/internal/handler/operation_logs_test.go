package handler

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOperationLogSummaryOmitsOutputBodies(t *testing.T) {
	payload, err := json.Marshal(operationLogItem{
		ID: 1, Source: "protocol_publish", Status: "failed",
		HasOutput: true, HasError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"has_output":true`, `"has_error":true`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("operation log summary %s does not contain %s", text, expected)
		}
	}
	for _, forbidden := range []string{`"output":`, `"error":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("operation log summary leaked detail field %s: %s", forbidden, text)
		}
	}
}

func TestOperationLogGlobalHistoryOrder(t *testing.T) {
	at := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	items := []operationLogItem{
		{ID: 1, Source: "task", CreatedAt: at},
		{ID: 2, Source: "node_kernel", CreatedAt: at},
		{ID: 3, Source: "node_kernel", CreatedAt: at},
		{ID: 4, Source: "protocol_publish", CreatedAt: at.Add(time.Second)},
	}
	sort.SliceStable(items, func(i, j int) bool { return operationLogComesBefore(items[i], items[j]) })
	want := []struct {
		source string
		id     uint
	}{{"protocol_publish", 4}, {"node_kernel", 3}, {"node_kernel", 2}, {"task", 1}}
	for index, expected := range want {
		if items[index].Source != expected.source || items[index].ID != expected.id {
			t.Fatalf("item %d = %s:%d, want %s:%d", index, items[index].Source, items[index].ID, expected.source, expected.id)
		}
	}
}

func TestOperationLogDetailIncludesOutputBodies(t *testing.T) {
	payload, err := json.Marshal(operationLogItem{Output: "command output", Error: "command error"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"output":"command output"`, `"error":"command error"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("operation log detail %s does not contain %s", text, expected)
		}
	}
}
