package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProxyPoolQueryUnavailable = errors.New("proxy pool query capability unavailable")
	ErrProxyPoolQueryPermission  = errors.New("proxy pool query requires current administrator")
)

type ProxyPoolRecord struct {
	ID                        uint       `json:"id"`
	NodeID                    uint       `json:"node_id"`
	Name                      string     `json:"name"`
	SubscriptionFormat        string     `json:"subscription_format,omitempty"`
	AutoSync                  bool       `json:"auto_sync"`
	SyncIntervalSeconds       int        `json:"sync_interval_seconds"`
	SubscriptionNodeCount     int        `json:"subscription_node_count"`
	LastSyncAt                *time.Time `json:"last_sync_at,omitempty"`
	NextSyncAt                *time.Time `json:"next_sync_at,omitempty"`
	LastSyncError             string     `json:"last_sync_error,omitempty"`
	Revision                  uint64     `json:"revision"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	ConfigCiphertext          string     `json:"-"`
	SubscriptionURLCiphertext string     `json:"-"`
	SubscriptionUserAgent     string     `json:"-"`
}

type ProxyPool struct {
	ProxyPoolRecord
	EntryCount             int64 `json:"entry_count"`
	SubscriptionConfigured bool  `json:"subscription_configured"`
}

type ProxyPoolQueryRepository interface {
	ListProxyPools(context.Context, uint, uint) ([]ProxyPool, error)
}
type ProxyPoolDetailRepository interface {
	GetProxyPool(context.Context, uint, uint) (ProxyPoolRecord, error)
}

func (q ProxyPoolQueries) Get(ctx context.Context, actor, id uint) (ProxyPoolRecord, error) {
	if q.Details == nil || actor == 0 || id == 0 {
		return ProxyPoolRecord{}, ErrProxyPoolQueryUnavailable
	}
	return q.Details.GetProxyPool(ctx, actor, id)
}

type ProxyPoolQueries struct {
	Repository ProxyPoolQueryRepository
	Details    ProxyPoolDetailRepository
}

func (q ProxyPoolQueries) List(ctx context.Context, actor, nodeID uint) ([]ProxyPool, error) {
	if q.Repository == nil || actor == 0 || nodeID == 0 {
		return nil, ErrProxyPoolQueryUnavailable
	}
	return q.Repository.ListProxyPools(ctx, actor, nodeID)
}
