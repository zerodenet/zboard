package network

import (
	"context"
	"testing"
)

type proxyPoolQueryRepositoryFunc func(context.Context, uint, uint) ([]ProxyPool, error)

func (f proxyPoolQueryRepositoryFunc) ListProxyPools(ctx context.Context, actor, nodeID uint) ([]ProxyPool, error) {
	return f(ctx, actor, nodeID)
}

func TestProxyPoolQueriesValidateAndDelegate(t *testing.T) {
	queries := ProxyPoolQueries{Repository: proxyPoolQueryRepositoryFunc(func(_ context.Context, actor, nodeID uint) ([]ProxyPool, error) {
		if actor != 5 || nodeID != 9 {
			t.Fatalf("actor=%d node=%d", actor, nodeID)
		}
		return []ProxyPool{{ProxyPoolRecord: ProxyPoolRecord{ID: 3, NodeID: nodeID}}}, nil
	})}
	items, err := queries.List(context.Background(), 5, 9)
	if err != nil || len(items) != 1 || items[0].ID != 3 {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	if _, err := queries.List(context.Background(), 0, 9); err != ErrProxyPoolQueryUnavailable {
		t.Fatalf("missing actor error=%v", err)
	}
}
