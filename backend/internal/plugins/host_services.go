package plugins

import (
	"context"
	"encoding/json"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type HostServices interface {
	CallPluginHost(context.Context, string, string, string, json.RawMessage) (json.RawMessage, *pluginv1.HostCallError)
}

// PluginPrincipalIssuer is optional so existing host-service implementations
// remain compatible. Production uses it to give page actions the same opaque,
// plugin-bound principal returned by account assertion.
type PluginPrincipalIssuer interface {
	PluginPrincipal(context.Context, string, uint) (string, error)
}

func isHostServiceCapability(capability string) bool {
	switch capability {
	case AccountAssertionCapability, SubscriptionProjectionCapability, MessageProjectionCapability,
		AccountSelfReadCapability, AccountAdminReadCapability, SubscriptionReadCapability, SubscriptionConfigReadCapability, SubscriptionAdminReadCapability,
		SubscriptionQuotaWriteCapability, SubscriptionTermWriteCapability, SubscriptionStatusWriteCapability,
		MessageReadCapability, MessageAckCapability:
		return true
	case HostDiscoveryCapability:
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
