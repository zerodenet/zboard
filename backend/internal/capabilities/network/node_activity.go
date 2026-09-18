package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNodeActivityUnavailable = errors.New("node activity unavailable")
	ErrNodeActivityCredential  = errors.New("node activity credential is no longer active")
	ErrNodeActivityInvalid     = errors.New("invalid node activity")
)

type NodeActivityUpdate struct {
	At            time.Time
	Online        bool
	ConnectorSeen bool
	Version       *string
	UptimeSeconds *uint64
	ActiveFlows   *uint64
	BytesUp       *uint64
	BytesDown     *uint64
}

type NodeActivityRepository interface {
	RecordNodeActivity(context.Context, uint, string, NodeActivityUpdate) error
}

type NodeActivity struct{ Repository NodeActivityRepository }

func (s NodeActivity) Record(ctx context.Context, nodeID uint, credential string, update NodeActivityUpdate) error {
	if s.Repository == nil {
		return ErrNodeActivityUnavailable
	}
	if nodeID == 0 || strings.TrimSpace(credential) == "" || update.At.IsZero() {
		return ErrNodeActivityInvalid
	}
	if update.Version != nil {
		value := strings.TrimSpace(*update.Version)
		if len(value) > 64 {
			return ErrNodeActivityInvalid
		}
		update.Version = &value
	}
	return s.Repository.RecordNodeActivity(ctx, nodeID, credential, update)
}
