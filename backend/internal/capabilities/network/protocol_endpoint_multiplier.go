package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProtocolEndpointMultiplierUnavailable = errors.New("protocol endpoint multiplier capability unavailable")
	ErrProtocolEndpointMultiplierPermission  = errors.New("protocol endpoint multiplier requires current administrator")
	ErrProtocolEndpointNotFound              = errors.New("protocol endpoint not found")
)

type ProtocolEndpointMultiplierValidation struct {
	Fields map[string]string
}

func (e *ProtocolEndpointMultiplierValidation) Error() string { return "协议倍率校验失败。" }

type ProtocolEndpoint struct {
	ID                    uint      `json:"id"`
	NodeID                uint      `json:"node_id"`
	Name                  string    `json:"name"`
	Protocol              string    `json:"protocol"`
	Address               string    `json:"address"`
	Port                  int       `json:"port"`
	PublicPort            int       `json:"public_port"`
	Cipher                int16     `json:"cipher"`
	ParentProtocolID      *uint     `json:"parent_protocol_id"`
	MultiplierMilli       int64     `json:"multiplier_milli"`
	ManagedPrincipalReady bool      `json:"managed_principal_ready"`
	MieruPrincipalReady   bool      `json:"mieru_principal_ready"`
	ClientConfig          string    `json:"client_config"`
	OptionalConfig        string    `json:"optional_config"`
	Tags                  string    `json:"tags"`
	IsActive              bool      `json:"is_active"`
	SortOrder             int       `json:"sort_order"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ProtocolEndpointMultiplierRepository interface {
	UpdateProtocolEndpointMultiplier(context.Context, uint, uint, int64) (ProtocolEndpoint, error)
}

type ProtocolEndpointMultiplier struct {
	Repository ProtocolEndpointMultiplierRepository
}

func (s ProtocolEndpointMultiplier) Update(ctx context.Context, actor, endpointID uint, multiplierMilli int64) (ProtocolEndpoint, error) {
	if s.Repository == nil || actor == 0 || endpointID == 0 {
		return ProtocolEndpoint{}, ErrProtocolEndpointMultiplierUnavailable
	}
	if multiplierMilli < 1 || multiplierMilli > 100000 {
		return ProtocolEndpoint{}, &ProtocolEndpointMultiplierValidation{Fields: map[string]string{
			"multiplier_milli": "multiplier_milli must be between 1 and 100000 (1000 means 1x)",
		}}
	}
	return s.Repository.UpdateProtocolEndpointMultiplier(ctx, actor, endpointID, multiplierMilli)
}
