package handler

import (
	"sort"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/model"
)

var errProtocolCredentialLockTimeout = application.ErrCredentialLockTimeout

func orderedProtocolCredentialSubscriptions(subscriptions []model.Subscription) []model.Subscription {
	ordered := append([]model.Subscription(nil), subscriptions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}
