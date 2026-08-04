package handler

import (
	"reflect"
	"strings"
	"testing"
)

func protocolEffectFixture() protocolEndpointEffectSnapshot {
	return protocolEndpointEffectSnapshot{
		NodeID:               10,
		Name:                 "Tokyo VLESS",
		Protocol:             "vless",
		Address:              "edge.example.com",
		Port:                 443,
		PublicPort:           443,
		MultiplierMilli:      1000,
		ServerConfig:         `{"type":"vless","users":[]}`,
		ClientConfig:         `{"type":"vless","server":"edge.example.com","port":443}`,
		OptionalConfig:       `{}`,
		Tags:                 `[]`,
		IsActive:             true,
		SortOrder:            10,
		ManagedCertificateID: 7,
	}
}

func TestProtocolEndpointNonRuntimeChangesDoNotPublish(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*protocolEndpointEffectSnapshot)
		effect protocolEndpointEffect
	}{
		{name: "name", mutate: func(item *protocolEndpointEffectSnapshot) { item.Name = "Tokyo Premium" }, effect: protocolEndpointEffectDelivery},
		{name: "address", mutate: func(item *protocolEndpointEffectSnapshot) { item.Address = "new.example.com" }, effect: protocolEndpointEffectDelivery},
		{name: "public port", mutate: func(item *protocolEndpointEffectSnapshot) { item.PublicPort = 8443 }, effect: protocolEndpointEffectDelivery},
		{name: "client config", mutate: func(item *protocolEndpointEffectSnapshot) {
			item.ClientConfig = `{"type":"vless","server":"new.example.com","port":443}`
		}, effect: protocolEndpointEffectDelivery},
		{name: "sort order", mutate: func(item *protocolEndpointEffectSnapshot) { item.SortOrder = 20 }, effect: protocolEndpointEffectDelivery},
		{name: "multiplier", mutate: func(item *protocolEndpointEffectSnapshot) { item.MultiplierMilli = 1500 }, effect: protocolEndpointEffectBilling},
		{name: "tags", mutate: func(item *protocolEndpointEffectSnapshot) { item.Tags = `["premium"]` }, effect: protocolEndpointEffectManagement},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := protocolEffectFixture()
			after := before
			test.mutate(&after)
			result := classifyProtocolEndpointChange(&before, after)
			if result.Effect != test.effect {
				t.Fatalf("effect = %q, want %q", result.Effect, test.effect)
			}
			if result.PublishStatus != protocolEndpointPublishNotRequired {
				t.Fatalf("publish status = %q, want %q", result.PublishStatus, protocolEndpointPublishNotRequired)
			}
			if len(result.AffectedNodeIDs) != 0 {
				t.Fatalf("affected nodes = %v, want none", result.AffectedNodeIDs)
			}
		})
	}
}

func TestProtocolEndpointRuntimeChangesPublishCurrentNode(t *testing.T) {
	parentID := uint(99)
	tests := []struct {
		name   string
		mutate func(*protocolEndpointEffectSnapshot)
	}{
		{name: "protocol", mutate: func(item *protocolEndpointEffectSnapshot) { item.Protocol = "vmess" }},
		{name: "listen port", mutate: func(item *protocolEndpointEffectSnapshot) { item.Port = 8443 }},
		{name: "cipher", mutate: func(item *protocolEndpointEffectSnapshot) { item.Cipher = 1 }},
		{name: "parent", mutate: func(item *protocolEndpointEffectSnapshot) { item.ParentProtocolID = &parentID }},
		{name: "active", mutate: func(item *protocolEndpointEffectSnapshot) { item.IsActive = false }},
		{name: "server config", mutate: func(item *protocolEndpointEffectSnapshot) {
			item.ServerConfig = `{"type":"vless","users":[],"ws":{"path":"/proxy"}}`
		}},
		{name: "optional config", mutate: func(item *protocolEndpointEffectSnapshot) { item.OptionalConfig = `{"tcp_fast_open":true}` }},
		{name: "certificate", mutate: func(item *protocolEndpointEffectSnapshot) { item.ManagedCertificateID = 8 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := protocolEffectFixture()
			after := before
			test.mutate(&after)
			result := classifyProtocolEndpointChange(&before, after)
			if result.Effect != protocolEndpointEffectRuntime {
				t.Fatalf("effect = %q, want runtime", result.Effect)
			}
			if result.PublishStatus != protocolEndpointPublishQueued {
				t.Fatalf("publish status = %q, want queued", result.PublishStatus)
			}
			if !reflect.DeepEqual(result.AffectedNodeIDs, []uint{after.NodeID}) {
				t.Fatalf("affected nodes = %v, want [%d]", result.AffectedNodeIDs, after.NodeID)
			}
		})
	}
}

func TestProtocolEndpointNodeMovePublishesOnlyOldAndNewNodes(t *testing.T) {
	before := protocolEffectFixture()
	after := before
	after.NodeID = 20

	result := classifyProtocolEndpointChange(&before, after)
	if result.Effect != protocolEndpointEffectCredentialPlacement {
		t.Fatalf("effect = %q, want credential placement", result.Effect)
	}
	if result.PublishStatus != protocolEndpointPublishQueued {
		t.Fatalf("publish status = %q, want queued", result.PublishStatus)
	}
	if !reflect.DeepEqual(result.AffectedNodeIDs, []uint{10, 20}) {
		t.Fatalf("affected nodes = %v, want [10 20]", result.AffectedNodeIDs)
	}
}

func TestProtocolEndpointCanonicalJSONAvoidsFalseRuntimeChange(t *testing.T) {
	before := protocolEffectFixture()
	after := before
	after.ServerConfig = "{\n  \"users\": [],\n  \"type\": \"vless\"\n}"
	after.ClientConfig = "{\n  \"port\": 443,\n  \"server\": \"edge.example.com\",\n  \"type\": \"vless\"\n}"

	result := classifyProtocolEndpointChange(&before, after)
	if result.Effect != protocolEndpointEffectNone {
		t.Fatalf("effect = %q, want none", result.Effect)
	}
	if result.PublishStatus != protocolEndpointPublishNotRequired {
		t.Fatalf("publish status = %q, want not required", result.PublishStatus)
	}
}

func TestProtocolEndpointCreatePublishesOnlyWhenActive(t *testing.T) {
	active := protocolEffectFixture()
	activeResult := classifyProtocolEndpointChange(nil, active)
	if activeResult.Effect != protocolEndpointEffectRuntime || activeResult.PublishStatus != protocolEndpointPublishQueued {
		t.Fatalf("active create result = %+v", activeResult)
	}

	inactive := active
	inactive.IsActive = false
	inactiveResult := classifyProtocolEndpointChange(nil, inactive)
	if inactiveResult.Effect != protocolEndpointEffectManagement || inactiveResult.PublishStatus != protocolEndpointPublishNotRequired {
		t.Fatalf("inactive create result = %+v", inactiveResult)
	}
}

func TestProtocolEndpointRuntimeOrderUsesStableIdentity(t *testing.T) {
	if protocolEndpointRuntimeOrder != "id asc" {
		t.Fatalf("runtime order = %q, want endpoint identity order", protocolEndpointRuntimeOrder)
	}
	if strings.Contains(protocolEndpointRuntimeOrder, "sort_order") {
		t.Fatalf("runtime order must not depend on delivery sort: %q", protocolEndpointRuntimeOrder)
	}
}
