package metering

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticCollectionPolicyNeedsBoundedExplicitWindow(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, value := range []string{
		``, `{}`, `false`, `"null"`, `[]`,
		`{"until":"2026-10-26T01:00:00Z","reason":""}`,
		`{"until":"2026-10-26T01:00:00Z","reason":"   "}`,
		`{"until":"2026-10-26T01:00:00Z","reason":"test","extra":true}`,
		`{"until":"2026-10-26T00:00:00Z","reason":"test"}`,
		`{"until":"2026-10-27T00:00:00.000000001Z","reason":"test"}`,
		`{"until":"2026-10-26T01:00:00Z","reason":"test"} {}`,
		fmt.Sprintf(`{"until":"2026-10-26T01:00:00Z","reason":%q}`, strings.Repeat("诊", 200)),
		strings.Repeat(" ", 1025),
	} {
		if err := ValidateDiagnosticCollection(value, now); err == nil {
			t.Fatalf("unsafe capture policy accepted: bytes=%d", len(value))
		}
	}
	for _, value := range []string{`null`, `{"until":"2026-10-26T01:00:00Z","reason":"support investigation"}`, `{"until":"2026-10-27T00:00:00Z","reason":"24h maximum"}`} {
		if err := ValidateDiagnosticCollection(value, now); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := ParseDiagnosticCollection(`{"until":"2026-10-26T01:00:00Z","reason":"test"}`)
	if err != nil || policy == nil || !policy.Active(now, now) {
		t.Fatal(policy, err)
	}
	if policy.Active(policy.Until, now) || policy.Active(now, time.Time{}) || policy.Active(now, now.Add(time.Second)) || policy.Active(now, now.Add(-25*time.Hour)) {
		t.Fatal("expired, future or unproven saved window accepted")
	}
}
