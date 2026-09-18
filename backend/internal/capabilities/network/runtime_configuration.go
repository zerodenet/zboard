package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRuntimeConfigurationInvalid     = errors.New("runtime configuration source request is invalid")
	ErrRuntimeConfigurationUnavailable = errors.New("runtime configuration source unavailable")
)

type RuntimeConfigurationCredential struct {
	ID                    uint
	SubscriptionID        uint
	PrincipalKey          string
	Secret                string
	ListenPort            int
	PublicPort            int
	SubscriptionUpdatedAt time.Time
	SpeedLimitMbps        int
	DeviceLimit           int
	SoleActiveCredential  bool
}

type RuntimeConfigurationCertificate struct {
	Status   string
	CertPath string
	KeyPath  string
	NotAfter *time.Time
}

type RuntimeConfigurationEndpoint struct {
	ID                      uint
	NodeID                  uint
	Protocol                string
	Address                 string
	Port                    int
	PublicPort              int
	MieruPrincipalReady     bool
	ServerConfig            string
	EgressConfig            string
	ActiveSubscriptionCount int64
	Credentials             []RuntimeConfigurationCredential
	Certificate             *RuntimeConfigurationCertificate
}

type RuntimeConfigurationLanding struct {
	EndpointExists      bool
	NodeExists          bool
	Protocol            string
	Address             string
	Port                int
	PublicPort          int
	Active              bool
	NodeEnabled         bool
	NodeLifecycleStatus string
}

type RuntimeConfigurationProxyPool struct {
	ID     uint
	NodeID uint
	Config string
}

type RuntimeConfigurationNetworkEntry struct {
	ID          uint
	NodeID      uint
	EndpointID  uint
	Network     string
	Port        int
	ProxyPoolID *uint
	PathConfig  string
	Landing     RuntimeConfigurationLanding
	ProxyPool   *RuntimeConfigurationProxyPool
}

type RuntimeConfigurationSnapshot struct {
	SiteURL        string
	Endpoints      []RuntimeConfigurationEndpoint
	NetworkEntries []RuntimeConfigurationNetworkEntry
}

type RuntimeConfigurationRepository interface {
	LoadRuntimeConfiguration(context.Context, uint, time.Time, []string) (RuntimeConfigurationSnapshot, error)
}

type RuntimeConfigurationSource struct {
	Repository RuntimeConfigurationRepository
}

func (s RuntimeConfigurationSource) Load(ctx context.Context, nodeID uint, now time.Time, credentialProtocols []string) (RuntimeConfigurationSnapshot, error) {
	if nodeID == 0 || now.IsZero() || len(credentialProtocols) == 0 {
		return RuntimeConfigurationSnapshot{}, ErrRuntimeConfigurationInvalid
	}
	if s.Repository == nil {
		return RuntimeConfigurationSnapshot{}, ErrRuntimeConfigurationUnavailable
	}
	protocols := append([]string(nil), credentialProtocols...)
	return s.Repository.LoadRuntimeConfiguration(ctx, nodeID, now.UTC(), protocols)
}
