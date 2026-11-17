package readtiming

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTimingsAreRequestLocalBoundedAndDoNotAcceptRequestValues(t *testing.T) {
	ctx, r := NewContext(context.Background())
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func() { defer group.Done(); Record(ctx, "sql", time.Millisecond) }()
	}
	group.Wait()
	if r.Header() != "sql;dur=40.000" {
		t.Fatal(r.Header())
	}
	for _, name := range []string{"header-secret=value", "sql\r\nHeader", "/users/42", "user_123", strings.Repeat("a", 41)} {
		Record(ctx, name, time.Second)
	}
	for i := 0; i < 30; i++ {
		Record(ctx, fmt.Sprintf("read_%c", 'a'+rune(i)), time.Millisecond)
	}
	if len(strings.Split(r.Header(), ", ")) != 16 {
		t.Fatal(r.Header())
	}
	_, other := NewContext(context.Background())
	if other.Header() != "" {
		t.Fatal("cross-request data")
	}
	finish := Start(context.Background(), "sql")
	finish()
}
func TestOperationFinishIsIdempotent(t *testing.T) {
	ctx, r := NewContext(context.Background())
	finish := Start(ctx, "quota")
	finish()
	before := r.Header()
	finish()
	if r.Header() != before {
		t.Fatal("double counted")
	}
}
