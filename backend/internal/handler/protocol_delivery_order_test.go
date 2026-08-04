package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gopkg.in/yaml.v2"
)

func TestProtocolEndpointOrderVersionIsStableAndTracksOrder(t *testing.T) {
	first := []model.ProtocolEndpoint{{ID: 2, SortOrder: 1}, {ID: 1, SortOrder: 0}}
	second := []model.ProtocolEndpoint{{ID: 1, SortOrder: 0}, {ID: 2, SortOrder: 1}}
	if protocolEndpointOrderVersion(first) != protocolEndpointOrderVersion(second) {
		t.Fatal("version must not depend on database row order")
	}
	second[1].SortOrder = 0
	if protocolEndpointOrderVersion(first) == protocolEndpointOrderVersion(second) {
		t.Fatal("version must change when a persisted delivery position changes")
	}
}

func TestValidateCompleteProtocolEndpointOrderRejectsPartialUnknownAndDuplicateScope(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{ID: 1}, {ID: 2}, {ID: 3}}
	for name, orderedIDs := range map[string][]uint{
		"partial":   {1, 2},
		"unknown":   {1, 2, 9},
		"duplicate": {1, 2, 2},
	} {
		t.Run(name, func(t *testing.T) {
			if duplicateID, invalid := duplicateOrZeroUintID(orderedIDs); invalid {
				if name != "duplicate" || duplicateID != 2 {
					t.Fatalf("unexpected duplicate validation: id=%d invalid=%t", duplicateID, invalid)
				}
				return
			}
			if err := validateCompleteProtocolEndpointOrder(endpoints, orderedIDs); err == nil {
				t.Fatal("invalid complete-scope order unexpectedly accepted")
			}
		})
	}
	if err := validateCompleteProtocolEndpointOrder(endpoints, []uint{3, 1, 2}); err != nil {
		t.Fatalf("valid complete-scope order rejected: %v", err)
	}
}

func TestOrderSubscriptionManifestNodesUsesGroupOrderBeforeGlobalOrder(t *testing.T) {
	subscriptions := []model.Subscription{{ID: 10, NodeGroupID: 100}}
	relations := []subscriptionDeliveryRelation{
		{NodeGroupID: 100, ProtocolEndpointID: 1, GroupSortOrder: 1, GlobalSortOrder: 0},
		{NodeGroupID: 100, ProtocolEndpointID: 2, GroupSortOrder: 0, GlobalSortOrder: 1},
	}
	nodes := []subscriptionManifestNode{
		{ID: 1, SubscriptionID: 10, CredentialID: "a"},
		{ID: 2, SubscriptionID: 10, CredentialID: "b"},
	}

	orderSubscriptionManifestNodes(subscriptions, relations, nodes)
	if got := manifestEndpointIDs(nodes); !reflect.DeepEqual(got, []uint{2, 1}) {
		t.Fatalf("ordered endpoints = %v, want group order [2 1]", got)
	}
}

func TestOrderSubscriptionManifestNodesFallsBackToGlobalOrderForLegacyGroupRows(t *testing.T) {
	subscriptions := []model.Subscription{{ID: 10, NodeGroupID: 100}}
	relations := []subscriptionDeliveryRelation{
		{NodeGroupID: 100, ProtocolEndpointID: 1, GroupSortOrder: 0, GlobalSortOrder: 20},
		{NodeGroupID: 100, ProtocolEndpointID: 2, GroupSortOrder: 0, GlobalSortOrder: 10},
	}
	nodes := []subscriptionManifestNode{{ID: 1}, {ID: 2}}

	orderSubscriptionManifestNodes(subscriptions, relations, nodes)
	if got := manifestEndpointIDs(nodes); !reflect.DeepEqual(got, []uint{2, 1}) {
		t.Fatalf("ordered endpoints = %v, want global fallback [2 1]", got)
	}
}

func TestOrderSubscriptionManifestNodesAggregatesMultipleSubscriptionsDeterministically(t *testing.T) {
	subscriptions := []model.Subscription{
		{ID: 10, NodeGroupID: 100},
		{ID: 20, NodeGroupID: 200},
	}
	relations := []subscriptionDeliveryRelation{
		{NodeGroupID: 100, ProtocolEndpointID: 1, GroupSortOrder: 0, GlobalSortOrder: 10},
		{NodeGroupID: 100, ProtocolEndpointID: 2, GroupSortOrder: 1, GlobalSortOrder: 20},
		{NodeGroupID: 200, ProtocolEndpointID: 3, GroupSortOrder: 0, GlobalSortOrder: 0},
		{NodeGroupID: 200, ProtocolEndpointID: 4, GroupSortOrder: 1, GlobalSortOrder: 30},
	}
	nodes := []subscriptionManifestNode{
		{ID: 4, SubscriptionID: 20, CredentialID: "d"},
		{ID: 3, CredentialID: "legacy"},
		{ID: 2, SubscriptionID: 10, CredentialID: "b"},
		{ID: 1, SubscriptionID: 10, CredentialID: "a"},
	}

	orderSubscriptionManifestNodes(subscriptions, relations, nodes)
	if got := manifestEndpointIDs(nodes); !reflect.DeepEqual(got, []uint{1, 2, 3, 4}) {
		t.Fatalf("ordered endpoints = %v, want subscription then group order [1 2 3 4]", got)
	}
}

func TestNormalizedDeliveryOrderFlowsThroughEveryNativeRenderer(t *testing.T) {
	data := subscriptionExporterTestData()
	data.ProtocolEndpoints = []subscriptionTemplateEndpoint{
		data.ProtocolEndpoints[1],
		data.ProtocolEndpoints[0],
		data.ProtocolEndpoints[2],
	}
	want := []string{"Tokyo VMess", "Hong Kong VLESS", "Singapore Trojan"}

	znet, err := renderZnetSinkSubscription(data, defaultSubscriptionCustomization(subscriptionRendererZnetSink))
	if err != nil {
		t.Fatal(err)
	}
	var znetDocument struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
		OutboundGroups []struct {
			Outbounds []string `json:"outbounds"`
		} `json:"outbound_groups"`
	}
	if err := json.Unmarshal([]byte(znet), &znetDocument); err != nil {
		t.Fatal(err)
	}
	znetTags := make([]string, 0, len(want))
	for _, outbound := range znetDocument.Outbounds {
		if outbound.Tag == "direct" || outbound.Tag == "block" || outbound.Tag == "DIRECT" || outbound.Tag == "REJECT" {
			continue
		}
		znetTags = append(znetTags, outbound.Tag)
	}
	if !reflect.DeepEqual(znetTags, want) {
		t.Fatalf("ZNet Sink order = %v, want %v", znetTags, want)
	}
	znetPolicyOrderFound := false
	for _, group := range znetDocument.OutboundGroups {
		if containsContiguousStringSequence(group.Outbounds, want) {
			znetPolicyOrderFound = true
			break
		}
	}
	if !znetPolicyOrderFound {
		t.Fatalf("ZNet Sink policy members do not preserve delivery order: %#v", znetDocument.OutboundGroups)
	}

	clash, err := renderClashSubscription(data, defaultSubscriptionCustomization(subscriptionRendererClash))
	if err != nil {
		t.Fatal(err)
	}
	var clashDocument struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(clash), &clashDocument); err != nil {
		t.Fatal(err)
	}
	clashNames := make([]string, 0, len(clashDocument.Proxies))
	for _, proxy := range clashDocument.Proxies {
		clashNames = append(clashNames, proxy.Name)
	}
	if !reflect.DeepEqual(clashNames, want) {
		t.Fatalf("Clash order = %v, want %v", clashNames, want)
	}
	clashPolicyOrderFound := false
	for _, group := range clashDocument.ProxyGroups {
		if containsContiguousStringSequence(group.Proxies, want) {
			clashPolicyOrderFound = true
			break
		}
	}
	if !clashPolicyOrderFound {
		t.Fatalf("Clash policy members do not preserve delivery order: %#v", clashDocument.ProxyGroups)
	}

	sing, err := renderSingBoxSubscription(data, defaultSubscriptionCustomization(subscriptionRendererSingBox))
	if err != nil {
		t.Fatal(err)
	}
	var singDocument struct {
		Outbounds []struct {
			Type      string   `json:"type"`
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(sing), &singDocument); err != nil {
		t.Fatal(err)
	}
	singTags := make([]string, 0, len(want))
	for _, outbound := range singDocument.Outbounds {
		if outbound.Type == "direct" || outbound.Type == "block" || outbound.Type == "selector" || outbound.Type == "urltest" {
			continue
		}
		singTags = append(singTags, outbound.Tag)
	}
	if !reflect.DeepEqual(singTags, want) {
		t.Fatalf("sing-box order = %v, want %v", singTags, want)
	}
	selectorFound := false
	for _, outbound := range singDocument.Outbounds {
		if outbound.Type == "selector" {
			selectorFound = true
			if !containsContiguousStringSequence(outbound.Outbounds, want) {
				t.Fatalf("sing-box policy members do not preserve delivery order: %#v", outbound.Outbounds)
			}
			break
		}
	}
	if !selectorFound {
		t.Fatal("sing-box default selector was not rendered")
	}

	if strings.Join(want, ",") == "" {
		t.Fatal("renderer order assertion is empty")
	}
}

func containsContiguousStringSequence(values, wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	for start := 0; start+len(wanted) <= len(values); start++ {
		if reflect.DeepEqual(values[start:start+len(wanted)], wanted) {
			return true
		}
	}
	return false
}

func manifestEndpointIDs(nodes []subscriptionManifestNode) []uint {
	ids := make([]uint, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}
