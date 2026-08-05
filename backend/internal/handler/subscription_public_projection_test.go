package handler

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gopkg.in/yaml.v2"
)

func TestParseSubscriptionProjectionFilterNormalizesAndCombinesValues(t *testing.T) {
	values := url.Values{
		"plan":        {"Pro,starter"},
		"sku":         {"pro-annual"},
		"node_group":  {"jp-premium"},
		"protocol":    {"VLESS", "hysteria2"},
		"region":      {" JP , 香港 "},
		"tag":         {"Premium,streaming"},
		"exclude_tag": {"maintenance"},
		"q":           {" 日本 "},
	}
	filter, err := parseSubscriptionProjectionFilter(values, func(protocol string) bool {
		return protocol == "vless" || protocol == "hysteria2"
	})
	if err != nil {
		t.Fatal(err)
	}
	for field, values := range map[string]map[string]struct{}{
		"plans": filter.Plans, "skus": filter.SKUs, "groups": filter.NodeGroups,
		"protocols": filter.Protocols, "regions": filter.Regions, "tags": filter.Tags,
		"exclude_tags": filter.ExcludeTags,
	} {
		if len(values) == 0 {
			t.Fatalf("%s was not parsed", field)
		}
	}
	if filter.Query != "日本" {
		t.Fatalf("query = %q, want 日本", filter.Query)
	}
	if _, exists := filter.Protocols["vless"]; !exists {
		t.Fatalf("protocols = %#v, want normalized vless", filter.Protocols)
	}
	if _, exists := filter.Regions["香港"]; !exists {
		t.Fatalf("regions = %#v, want normalized Unicode region", filter.Regions)
	}
}

func TestParseSubscriptionProjectionFilterRejectsUnsupportedProtocolAndInvalidCode(t *testing.T) {
	for name, values := range map[string]url.Values{
		"protocol": {"protocol": {"unknown"}},
		"code":     {"plan": {"../../admin"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSubscriptionProjectionFilter(values, func(protocol string) bool { return protocol == "vless" }); err == nil {
				t.Fatal("invalid filter unexpectedly accepted")
			}
		})
	}
}

func TestSubscriptionProjectionUsesAndAcrossDimensionsAndOrWithinDimension(t *testing.T) {
	filter := subscriptionProjectionFilter{
		Plans:      map[string]struct{}{"pro": {}, "starter": {}},
		SKUs:       map[string]struct{}{"pro-annual": {}},
		NodeGroups: map[string]struct{}{"jp-premium": {}},
		Protocols:  map[string]struct{}{"vless": {}, "hysteria2": {}},
		Regions:    map[string]struct{}{"jp": {}},
		Tags:       map[string]struct{}{"premium": {}, "streaming": {}},
		ExcludeTags: map[string]struct{}{
			"maintenance": {},
		},
		Query: "tokyo",
	}
	if !filter.matchesSource(subscriptionProjectionSource{PlanSlug: "pro", SKUCode: "pro-annual", NodeGroupCode: "jp-premium"}) {
		t.Fatal("matching source was rejected")
	}
	if filter.matchesSource(subscriptionProjectionSource{PlanSlug: "pro", SKUCode: "monthly", NodeGroupCode: "jp-premium"}) {
		t.Fatal("different dimensions must use AND semantics")
	}
	endpoint := model.ProtocolEndpoint{Name: "Tokyo Premium", Protocol: "vless", Tags: `["premium","streaming"]`}
	node := model.Node{Region: "JP"}
	if !filter.matchesEndpoint(endpoint, node) {
		t.Fatal("matching endpoint was rejected")
	}
	endpoint.Tags = `["premium","maintenance"]`
	if filter.matchesEndpoint(endpoint, node) {
		t.Fatal("exclude_tag must remove an otherwise matching endpoint")
	}
}

func TestFilterSubscriptionsForProjectionPreservesSourceOrder(t *testing.T) {
	subscriptions := []model.Subscription{{ID: 3}, {ID: 1}, {ID: 2}}
	sources := map[uint]subscriptionProjectionSource{
		1: {PlanSlug: "starter"},
		2: {PlanSlug: "pro"},
		3: {PlanSlug: "pro"},
	}
	filtered := filterSubscriptionsForProjection(subscriptions, sources, subscriptionProjectionFilter{Plans: map[string]struct{}{"pro": {}}})
	got := make([]uint, 0, len(filtered))
	for _, subscription := range filtered {
		got = append(got, subscription.ID)
	}
	if !reflect.DeepEqual(got, []uint{3, 2}) {
		t.Fatalf("filtered subscriptions = %v, want source order [3 2]", got)
	}
}

func TestProjectionWithUnknownStableCodeReturnsEmptySubset(t *testing.T) {
	subscriptions := []model.Subscription{{ID: 1}, {ID: 2}}
	sources := map[uint]subscriptionProjectionSource{
		1: {PlanSlug: "starter"},
		2: {PlanSlug: "pro"},
	}
	filtered := filterSubscriptionsForProjection(subscriptions, sources, subscriptionProjectionFilter{Plans: map[string]struct{}{"missing": {}}})
	if len(filtered) != 0 {
		t.Fatalf("filtered subscriptions = %#v, want a valid empty subset", filtered)
	}
}

func TestEmptyProjectionRendersValidNativeFormats(t *testing.T) {
	data := sampleSubscriptionTemplateData()
	data.ProtocolEndpoints = []subscriptionTemplateEndpoint{}
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererClash, subscriptionRendererSingBox} {
		t.Run(renderer, func(t *testing.T) {
			customization := defaultSubscriptionCustomization(renderer)
			customization.PolicyGroups = nil
			customization.RuleSets = nil
			customization.Final = subscriptionTargetDirect
			customization.AdvancedSource = ""
			definition, ok := subscriptionRenderer(renderer)
			if !ok {
				t.Fatalf("renderer %q is not registered", renderer)
			}
			rendered, err := definition.render(data, customization)
			if err != nil {
				t.Fatal(err)
			}
			switch renderer {
			case subscriptionRendererClash:
				var document map[interface{}]interface{}
				if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
					t.Fatal(err)
				}
			case subscriptionRendererZnetSink, subscriptionRendererSingBox:
				var document map[string]interface{}
				if err := json.Unmarshal([]byte(rendered), &document); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
