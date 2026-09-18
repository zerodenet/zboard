package entitlements

import (
	"context"
	"time"
)

type NetworkEntryProjectionSubscription struct {
	ID, NodeGroupID uint
}

type NetworkEntryProjectionEntry struct {
	ID, NodeGroupID, NodeID, EndpointID uint
	Name, Address, Network              string
	Port, PublicPort                    int
}

type NetworkEntryProjectionEndpoint struct {
	ID, NodeID                                 uint
	Name, Protocol, Address, ServerConfig      string
	ClientConfig, Tags                         string
	Port, PublicPort                           int
	MultiplierMilli                            int64
	ManagedPrincipalReady, MieruPrincipalReady bool
}

type NetworkEntryProjectionNode struct {
	ID              uint
	Region          string
	IsEnabled       bool
	LifecycleStatus string
	LastSeenAt      *time.Time
}

type NetworkEntryProjectionCredential struct {
	SubscriptionID, ProtocolEndpointID uint
	CredentialID, SecretCiphertext     string
	ListenPort, PublicPort             int
}

type NetworkEntryProjectionData struct {
	Entries        []NetworkEntryProjectionEntry
	Endpoints      []NetworkEntryProjectionEndpoint
	Nodes          []NetworkEntryProjectionNode
	PendingNodeIDs []uint
	Credentials    []NetworkEntryProjectionCredential
}

type NetworkEntryProjectionRepository interface {
	LoadNetworkEntryProjection(context.Context, []NetworkEntryProjectionSubscription, time.Time) (NetworkEntryProjectionData, error)
}

type NetworkEntryProjection struct {
	Repository NetworkEntryProjectionRepository
}

func (s NetworkEntryProjection) Load(ctx context.Context, subscriptions []NetworkEntryProjectionSubscription, now time.Time) (NetworkEntryProjectionData, error) {
	if len(subscriptions) == 0 {
		return NetworkEntryProjectionData{}, nil
	}
	return s.Repository.LoadNetworkEntryProjection(ctx, subscriptions, now)
}
