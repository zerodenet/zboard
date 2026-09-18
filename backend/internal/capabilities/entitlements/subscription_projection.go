package entitlements

import (
	"context"
	"time"
)

// SubscriptionProjection is the bounded persistence snapshot used to render a
// public subscription. It deliberately carries encrypted credentials as opaque
// values; only the delivery adapter may decrypt them.
type SubscriptionProjectionSource struct {
	SubscriptionID uint
	PlanSlug       string
	SKUCode        string
	NodeGroupCode  string
}

type SubscriptionProjectionEndpoint struct {
	ID, NodeID                                                uint
	Name, Protocol, Address, ServerConfig, ClientConfig, Tags string
	Port, PublicPort, SortOrder                               int
	MultiplierMilli                                           int64
	ManagedPrincipalReady, MieruPrincipalReady                bool
}

type SubscriptionProjectionNode struct {
	ID         uint
	Region     string
	IsEnabled  bool
	LastSeenAt *time.Time
}

type SubscriptionProjectionCredential struct {
	ID, SubscriptionID, UserID, ProtocolEndpointID, NodeID uint
	CredentialID, PrincipalKey, SecretCiphertext, Status   string
	ListenPort, PublicPort                                 int
	ExpiresAt                                              time.Time
	LastUsedAt, RevokedAt                                  *time.Time
	CreatedAt, UpdatedAt                                   time.Time
}

type SubscriptionProjectionData struct {
	Memberships map[uint][]uint
	Credentials []SubscriptionProjectionCredential
	Endpoints   []SubscriptionProjectionEndpoint
	Nodes       map[uint]SubscriptionProjectionNode
}

type SubscriptionProjectionRepository interface {
	ProjectionSources(context.Context, []uint) ([]SubscriptionProjectionSource, error)
	ProjectionData(context.Context, []uint, []uint, time.Time) (SubscriptionProjectionData, error)
}

type SubscriptionProjection struct {
	Repository SubscriptionProjectionRepository
}

func (s SubscriptionProjection) Sources(ctx context.Context, ids []uint) ([]SubscriptionProjectionSource, error) {
	if len(ids) == 0 {
		return []SubscriptionProjectionSource{}, nil
	}
	return s.Repository.ProjectionSources(ctx, ids)
}

func (s SubscriptionProjection) Load(ctx context.Context, subscriptionIDs, nodeGroupIDs []uint, now time.Time) (SubscriptionProjectionData, error) {
	if len(subscriptionIDs) == 0 || len(nodeGroupIDs) == 0 {
		return SubscriptionProjectionData{Memberships: map[uint][]uint{}, Nodes: map[uint]SubscriptionProjectionNode{}}, nil
	}
	return s.Repository.ProjectionData(ctx, subscriptionIDs, nodeGroupIDs, now)
}
