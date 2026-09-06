package handler

import (
	"encoding/json"
	"fmt"
	"testing"
)

func BenchmarkManagedRulePreparation(b *testing.B) {
	document := managedRuleDocument{Version: 1, Rules: make([]managedRule, 10_000)}
	for i := range document.Rules {
		document.Rules[i] = managedRule{Type: managedRuleTypeDomainExact, Value: fmt.Sprintf("host-%06d.example.test", i)}
	}
	content, err := json.Marshal(document)
	if err != nil {
		b.Fatal(err)
	}
	source := string(content)
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for b.Loop() {
		req := subscriptionRuleSetWriteReq{Name: "Benchmark", Tag: "benchmark", Content: &source,
			SourceFormat: managedRuleSourceZeroRuleIR, SyncInterval: 3600}
		prepared, err := prepareSubscriptionRuleSet(&req)
		if err != nil {
			b.Fatal(err)
		}
		if prepared == nil || len(encodeManagedCanonicalSource(*prepared)) == 0 {
			b.Fatal("empty canonical source")
		}
	}
}

func TestPrepareManagedRuleSetReturnsCanonicalInlineContent(t *testing.T) {
	content := `{"version":1,"rules":[
		{"type":"domain_exact","value":"API.Example.COM."},
		{"type":"domain_suffix","value":"example.com"},
		{"type":"domain_suffix","value":"example.com"}]}`
	req := subscriptionRuleSetWriteReq{Name: "Inline", Tag: "inline", Content: &content}
	document, err := prepareSubscriptionRuleSet(&req)
	if err != nil || document == nil {
		t.Fatalf("prepare inline rules: %v, %v", document, err)
	}
	if document.Version != 1 || len(document.Rules) != 1 || document.Rules[0] != (managedRule{Type: managedRuleTypeDomainSuffix, Value: "example.com"}) {
		t.Fatalf("inline rules were not normalized and deduplicated: %+v", document)
	}
}
