package network

import (
	"context"
	"errors"
)

var ErrEventCredentialUnavailable = errors.New("Zero event credential is unavailable")

type EventNode struct {
	ID         uint
	Credential string
}

type EventCredentialRepository interface {
	EventNode(context.Context, uint) (EventNode, error)
}

type EventCredentials struct{ Repository EventCredentialRepository }

func (s EventCredentials) Load(ctx context.Context, nodeID uint) (EventNode, error) {
	if s.Repository == nil || nodeID == 0 {
		return EventNode{}, ErrEventCredentialUnavailable
	}
	node, err := s.Repository.EventNode(ctx, nodeID)
	if err != nil || node.ID == 0 || node.Credential == "" {
		if err == nil {
			err = ErrEventCredentialUnavailable
		}
		return EventNode{}, err
	}
	return node, nil
}
