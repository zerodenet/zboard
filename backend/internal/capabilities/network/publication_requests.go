package network

import (
	"context"
	"errors"
	"sort"
)

var ErrPublicationRequestUnavailable = errors.New("node publication request capability unavailable")

type PublicationRequest struct {
	NodeID            uint
	TriggerEndpointID uint
	RequestedBy       uint
}

type PublicationRequestRepository interface {
	QueuePublicationRequests(context.Context, []PublicationRequest) error
}

type PublicationRequests struct{ Repository PublicationRequestRepository }

func (s PublicationRequests) Queue(ctx context.Context, requests []PublicationRequest) error {
	if s.Repository == nil {
		return ErrPublicationRequestUnavailable
	}
	byNode := make(map[uint]PublicationRequest, len(requests))
	for _, request := range requests {
		if request.NodeID == 0 {
			continue
		}
		if _, exists := byNode[request.NodeID]; !exists {
			byNode[request.NodeID] = request
		}
	}
	ordered := make([]PublicationRequest, 0, len(byNode))
	for _, request := range byNode {
		ordered = append(ordered, request)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NodeID < ordered[j].NodeID })
	if len(ordered) == 0 {
		return nil
	}
	return s.Repository.QueuePublicationRequests(ctx, ordered)
}
