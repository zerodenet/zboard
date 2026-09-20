package plugins

import (
	"context"
	"encoding/json"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type HostServices interface {
	CallPluginHost(context.Context, string, string, string, json.RawMessage) (json.RawMessage, *pluginv1.HostCallError)
}

func isHostServiceCapability(capability string) bool {
	switch capability {
	case AccountAssertionCapability, SubscriptionProjectionCapability, MessageProjectionCapability:
		return true
	default:
		return false
	}
}

func requiresHostCallback(v Installation) bool {
	if hasCapability(v, StorageCapability) {
		return true
	}
	for _, capability := range v.Manifest.Capabilities {
		if hasCapability(v, capability) && isHostServiceCapability(capability) {
			return true
		}
	}
	return false
}
