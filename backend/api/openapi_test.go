package api

import (
	"bytes"
	"os"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestOpenAPIIsValidYAMLAndContainsCoreCommercialPaths(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[interface{}]interface{}
	if err := yaml.UnmarshalStrict(payload, &document); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	paths, ok := document["paths"].(map[interface{}]interface{})
	if !ok {
		t.Fatal("OpenAPI paths object is missing")
	}
	for _, path := range []string{
		"/api/v1/setup/install",
		"/api/v1/admin/protocol-endpoints",
		"/api/v1/admin/provider-definitions",
		"/api/v1/admin/provider-accounts",
		"/api/v1/admin/provider-accounts/{id}/verify",
		"/api/v1/admin/dns-records",
		"/api/v1/admin/dns-records/{id}/sync",
		"/api/v1/admin/protocol-endpoints/selection",
		"/api/v1/admin/protocol-endpoints/{id}",
		"/api/v1/admin/protocol-endpoints/{id}/deploy",
		"/api/v1/admin/protocol-deployments",
		"/api/v1/nodes/{id}",
		"/api/v1/nodes/{id}/ssh",
		"/api/v1/nodes/{id}/ssh/host-key/reset",
		"/api/v1/nodes/{id}/ssh/terminal-ticket",
		"/api/v1/nodes/{id}/ssh/terminal",
		"/api/v1/nodes/{id}/kernel",
		"/api/v1/nodes/{id}/kernel/detect",
		"/api/v1/nodes/{id}/kernel/reconcile",
		"/api/v1/admin/kernel/releases/latest",
		"/api/v1/nodes/{id}/connector-credential",
		"/api/v1/nodes/{id}/heartbeat",
		"/api/v1/nodes/{id}/commands",
		"/api/v1/plans/{id}",
		"/api/v1/plans/{id}/skus",
		"/api/v1/admin/plans/{id}",
		"/api/v1/admin/plans/{id}/skus",
		"/api/v1/admin/plan-skus/{id}",
		"/api/v1/admin/node-groups",
		"/api/v1/admin/system-configs/{key}",
		"/api/v1/admin/tasks",
		"/api/v1/admin/tasks/{id}",
		"/api/v1/admin/tasks/{id}/items",
		"/api/v1/admin/tasks/{id}/run",
		"/api/v1/admin/node-operations",
		"/api/v1/admin/protocol-deployments/batch",
		"/api/v1/admin/protocol-endpoints/batch",
		"/api/v1/tickets",
		"/api/v1/tickets/{id}/messages",
		"/api/v1/admin/tickets",
		"/api/v1/admin/tickets/{id}/messages",
		"/api/v1/admin/tickets/{id}/status",
		"/api/v1/admin/subscription-templates/preview",
		"/api/v1/admin/subscription-templates/{id}",
		"/api/v1/orders",
		"/api/v1/admin/orders",
		"/api/v1/admin/orders/{id}",
		"/api/v1/admin/orders/{id}/payment-events",
		"/api/v1/admin/subscriptions",
		"/api/v1/admin/subscriptions/{id}",
		"/api/v1/admin/users/{id}",
		"/api/v1/admin/traffic/records",
		"/api/zero/events",
		"/api/v1/traffic/report",
	} {
		if _, ok := paths[path]; !ok {
			t.Errorf("OpenAPI path %s is missing", path)
		}
	}
	for _, removed := range []string{
		"/api/v1/protocol-endpoints",
		"/api/v1/nodes/protocol/config",
		"/api/v1/admin/access-groups",
		"/api/v1/admin/plans/{id}/protocol-endpoints",
	} {
		if _, ok := paths[removed]; ok {
			t.Errorf("removed OpenAPI path %s is still present", removed)
		}
	}
	for _, removedField := range [][]byte{
		[]byte("access_group_id:"),
		[]byte("traffic_multiplier_milli:"),
		[]byte("subscription_multiplier_milli:"),
		[]byte("ssh_host_key_policy:"),
		[]byte("account_type:"),
	} {
		if bytes.Contains(payload, removedField) {
			t.Errorf("removed OpenAPI field %s is still present", removedField)
		}
	}
}

func TestOpenAPIDocumentsCanonicalAdminPageEnvelope(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[interface{}]interface{}
	if err := yaml.UnmarshalStrict(payload, &document); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	components := document["components"].(map[interface{}]interface{})
	schemas := components["schemas"].(map[interface{}]interface{})
	for _, name := range []string{"ApiErrorDetail", "PageMetadata", "PageEnvelope", "UserPage", "AdminUserListItem", "AdminUserDetail", "AdminTaskPage", "AdminTaskItemPage", "NodeBatchOperationRequest", "ProtocolBatchScopeRequest", "ProtocolBatchActiveRequest", "NodeSummary", "NodeDetail", "NodePage", "ProtocolEndpointSummary", "ProtocolEndpointSelectionSnapshot", "ProtocolEndpointPage", "PlanNodeGroupSummary", "PlanSummary", "PlanDetail", "PlanCatalogItem", "PlanPage", "PlanCatalogPage", "PlanSKUPage", "NodeGroupSummary", "NodeGroup", "NodeGroupMutationResponse", "NodeGroupPage", "OrderPage", "AdminOrderDetail", "PaymentEventSummary", "PaymentEventPage", "SubscriptionPage", "AdminSubscriptionDetail", "TicketPage", "TicketDetail", "SubscriptionTemplatePage", "SubscriptionTemplatePreviewRequest", "SubscriptionTemplatePreview", "TrafficRecordSummary", "TrafficRecordAggregates", "TrafficRecordPage", "TrafficReconciliationAggregates", "TrafficReconciliationPage", "AuditLogSummary", "AuditLogDetail", "AuditLogPage", "OperationLogPage", "OperationLogDetail"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("OpenAPI schema %s is missing", name)
		}
	}
	apiResponse := schemas["ApiResponse"].(map[interface{}]interface{})
	apiResponseProperties := apiResponse["properties"].(map[interface{}]interface{})
	if _, ok := apiResponseProperties["error"]; !ok {
		t.Error("OpenAPI ApiResponse does not document the versioned error detail")
	}

	paths := document["paths"].(map[interface{}]interface{})
	planDetail := schemas["PlanDetail"].(map[interface{}]interface{})
	planDetailAllOf := planDetail["allOf"].([]interface{})
	planDetailProperties := planDetailAllOf[1].(map[interface{}]interface{})["properties"].(map[interface{}]interface{})
	if _, ok := planDetailProperties["skus"]; ok {
		t.Error("OpenAPI PlanDetail still embeds the unbounded SKU collection")
	}
	planSKUList := paths["/api/v1/admin/plans/{id}/skus"].(map[interface{}]interface{})
	if _, ok := planSKUList["get"]; !ok {
		t.Error("OpenAPI plan SKU list GET operation is missing")
	}
	planSKUDetail := paths["/api/v1/admin/plan-skus/{id}"].(map[interface{}]interface{})
	if _, ok := planSKUDetail["get"]; !ok {
		t.Error("OpenAPI plan SKU detail GET operation is missing")
	}
	planCatalogItem := schemas["PlanCatalogItem"].(map[interface{}]interface{})
	planCatalogAllOf := planCatalogItem["allOf"].([]interface{})
	planCatalogProperties := planCatalogAllOf[1].(map[interface{}]interface{})["properties"].(map[interface{}]interface{})
	if _, ok := planCatalogProperties["skus"]; ok {
		t.Error("OpenAPI PlanCatalogItem embeds an unbounded SKU collection")
	}
	if _, ok := planCatalogProperties["primary_sku"]; !ok {
		t.Error("OpenAPI PlanCatalogItem omits its bounded primary SKU")
	}
	for _, path := range []string{"/api/v1/plans", "/api/v1/admin/users", "/api/v1/admin/tasks", "/api/v1/nodes", "/api/v1/orders", "/api/v1/admin/orders", "/api/v1/subscriptions", "/api/v1/admin/subscriptions", "/api/v1/admin/protocol-endpoints", "/api/v1/subscription-templates", "/api/v1/admin/subscription-templates"} {
		operation := paths[path].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
		parameters, _ := operation["parameters"].([]interface{})
		found := false
		for _, raw := range parameters {
			parameter, _ := raw.(map[interface{}]interface{})
			if parameter["name"] == "paged" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("OpenAPI path %s does not document the paged query parameter", path)
		}
	}
	for path, names := range map[string][]string{
		"/api/v1/admin/users":         {"q", "status", "is_admin", "sort", "direction"},
		"/api/v1/admin/orders":        {"q", "status", "order_type", "user_id", "created_from", "created_to"},
		"/api/v1/admin/subscriptions": {"q", "status", "quota", "user_id", "expires_from", "expires_to"},
		"/api/v1/admin/tickets":       {"q", "status", "category"},
	} {
		operation := paths[path].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
		parameters, _ := operation["parameters"].([]interface{})
		documented := map[string]bool{}
		for _, raw := range parameters {
			parameter, _ := raw.(map[interface{}]interface{})
			if name, ok := parameter["name"].(string); ok {
				documented[name] = true
			}
		}
		for _, name := range names {
			if !documented[name] {
				t.Errorf("OpenAPI business path %s does not document %s", path, name)
			}
		}
	}
	for _, path := range []string{"/api/v1/admin/orders", "/api/v1/admin/tickets"} {
		operation := paths[path].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
		parameters, _ := operation["parameters"].([]interface{})
		attentionDocumented := false
		for _, raw := range parameters {
			parameter, _ := raw.(map[interface{}]interface{})
			if parameter["name"] != "status" {
				continue
			}
			schema, _ := parameter["schema"].(map[interface{}]interface{})
			values, _ := schema["enum"].([]interface{})
			for _, value := range values {
				if value == "attention" {
					attentionDocumented = true
					break
				}
			}
		}
		if !attentionDocumented {
			t.Errorf("OpenAPI administrator work queue %s does not document the attention status", path)
		}
	}
	if _, ok := paths["/api/v1/admin/operation-logs/{source}/{id}"]; !ok {
		t.Error("OpenAPI operation log detail path is missing")
	}
	if _, ok := paths["/api/v1/admin/audit-logs/{id}"]; !ok {
		t.Error("OpenAPI audit log detail path is missing")
	}
	nodeGroupDetail := paths["/api/v1/admin/node-groups/{id}"].(map[interface{}]interface{})
	if _, ok := nodeGroupDetail["get"]; !ok {
		t.Error("OpenAPI node-group detail GET operation is missing")
	}
	nodeGroupUpdate := nodeGroupDetail["put"].(map[interface{}]interface{})
	nodeGroupUpdateResponses := nodeGroupUpdate["responses"].(map[interface{}]interface{})
	for _, status := range []string{"409", "428"} {
		if _, ok := nodeGroupUpdateResponses[status]; !ok {
			t.Errorf("OpenAPI node-group update response %s is missing", status)
		}
	}
	nodeGroupUpdateSchema := schemas["NodeGroupUpdateRequest"].(map[interface{}]interface{})
	nodeGroupUpdateRequired := nodeGroupUpdateSchema["required"].([]interface{})
	expectedRevisionRequired := false
	for _, field := range nodeGroupUpdateRequired {
		if field == "expected_revision" {
			expectedRevisionRequired = true
		}
	}
	if !expectedRevisionRequired {
		t.Error("OpenAPI NodeGroupUpdateRequest does not require expected_revision")
	}
	nodeGroupSummarySchema := schemas["NodeGroupSummary"].(map[interface{}]interface{})
	nodeGroupSummaryRequired := nodeGroupSummarySchema["required"].([]interface{})
	revisionRequired := false
	for _, field := range nodeGroupSummaryRequired {
		if field == "revision" {
			revisionRequired = true
		}
	}
	if !revisionRequired {
		t.Error("OpenAPI NodeGroupSummary does not require revision")
	}
	planDetailPath := paths["/api/v1/admin/plans/{id}"].(map[interface{}]interface{})
	planUpdate := planDetailPath["put"].(map[interface{}]interface{})
	planUpdateResponses := planUpdate["responses"].(map[interface{}]interface{})
	for _, status := range []string{"409", "428"} {
		if _, ok := planUpdateResponses[status]; !ok {
			t.Errorf("OpenAPI plan update response %s is missing", status)
		}
	}
	planUpdateSchema := schemas["PlanUpdateRequest"].(map[interface{}]interface{})
	planUpdateRequired := planUpdateSchema["required"].([]interface{})
	planExpectedRevisionRequired := false
	for _, field := range planUpdateRequired {
		if field == "expected_revision" {
			planExpectedRevisionRequired = true
		}
	}
	if !planExpectedRevisionRequired {
		t.Error("OpenAPI PlanUpdateRequest does not require expected_revision")
	}
	planSummarySchema := schemas["PlanSummary"].(map[interface{}]interface{})
	planSummaryRequired := planSummarySchema["required"].([]interface{})
	planRevisionRequired := false
	for _, field := range planSummaryRequired {
		if field == "revision" {
			planRevisionRequired = true
		}
	}
	if !planRevisionRequired {
		t.Error("OpenAPI PlanSummary does not require revision")
	}
	selectionSchema := schemas["ProtocolEndpointSelectionSnapshot"].(map[interface{}]interface{})
	selectionProperties := selectionSchema["properties"].(map[interface{}]interface{})
	selectionIDs := selectionProperties["ids"].(map[interface{}]interface{})
	if selectionIDs["maxItems"] != 10000 {
		t.Errorf("ProtocolEndpointSelectionSnapshot ids maxItems = %#v, want 10000", selectionIDs["maxItems"])
	}
	if _, ok := paths["/api/v1/admin/protocol-endpoints/selection"]; !ok {
		t.Error("OpenAPI endpoint selection snapshot path is missing")
	}
	for _, path := range []string{"/api/v1/traffic/records", "/api/v1/admin/traffic/records", "/api/v1/admin/audit-logs", "/api/v1/admin/operation-logs"} {
		operation := paths[path].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
		parameters, _ := operation["parameters"].([]interface{})
		documented := map[string]bool{}
		for _, raw := range parameters {
			parameter, _ := raw.(map[interface{}]interface{})
			if name, ok := parameter["name"].(string); ok {
				documented[name] = true
			}
		}
		for _, name := range []string{"cursor", "from", "to", "limit"} {
			if !documented[name] {
				t.Errorf("OpenAPI history path %s does not document %s", path, name)
			}
		}
	}
	trafficPage := schemas["TrafficRecordPage"].(map[interface{}]interface{})
	trafficPageAllOf := trafficPage["allOf"].([]interface{})
	trafficPageProperties := trafficPageAllOf[1].(map[interface{}]interface{})["properties"].(map[interface{}]interface{})
	trafficAggregates := trafficPageProperties["aggregates"].(map[interface{}]interface{})
	if trafficAggregates["$ref"] != "#/components/schemas/TrafficRecordAggregates" {
		t.Errorf("TrafficRecordPage aggregates = %#v, want TrafficRecordAggregates reference", trafficAggregates["$ref"])
	}
	trafficItems := trafficPageProperties["items"].(map[interface{}]interface{})["items"].(map[interface{}]interface{})
	if trafficItems["$ref"] != "#/components/schemas/TrafficRecordSummary" {
		t.Errorf("TrafficRecordPage items = %#v, want TrafficRecordSummary reference", trafficItems["$ref"])
	}
	trafficSummary := schemas["TrafficRecordSummary"].(map[interface{}]interface{})
	trafficSummaryProperties := trafficSummary["properties"].(map[interface{}]interface{})
	for _, field := range []string{"upload_bytes", "download_bytes"} {
		if _, ok := trafficSummaryProperties[field]; !ok {
			t.Errorf("TrafficRecordSummary does not document %s", field)
		}
	}
	reconciliationPage := schemas["TrafficReconciliationPage"].(map[interface{}]interface{})
	reconciliationPageAllOf := reconciliationPage["allOf"].([]interface{})
	reconciliationPageProperties := reconciliationPageAllOf[1].(map[interface{}]interface{})["properties"].(map[interface{}]interface{})
	reconciliationAggregates := reconciliationPageProperties["aggregates"].(map[interface{}]interface{})
	if reconciliationAggregates["$ref"] != "#/components/schemas/TrafficReconciliationAggregates" {
		t.Errorf("TrafficReconciliationPage aggregates = %#v, want TrafficReconciliationAggregates reference", reconciliationAggregates["$ref"])
	}
	reconciliationItems := reconciliationPageProperties["items"].(map[interface{}]interface{})["items"].(map[interface{}]interface{})
	if reconciliationItems["$ref"] != "#/components/schemas/TrafficReconciliationItem" {
		t.Errorf("TrafficReconciliationPage items = %#v, want TrafficReconciliationItem reference", reconciliationItems["$ref"])
	}
	pageMetadata := schemas["PageMetadata"].(map[interface{}]interface{})
	pageProperties := pageMetadata["properties"].(map[interface{}]interface{})
	if _, ok := pageProperties["previous_cursor"]; !ok {
		t.Error("OpenAPI PageMetadata does not document previous_cursor")
	}

	protocolOperation := paths["/api/v1/admin/protocol-endpoints"].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
	protocolParameters, _ := protocolOperation["parameters"].([]interface{})
	documented := map[string]bool{}
	for _, raw := range protocolParameters {
		parameter, _ := raw.(map[interface{}]interface{})
		if name, ok := parameter["name"].(string); ok {
			documented[name] = true
		}
	}
	for _, name := range []string{"sort", "direction"} {
		if !documented[name] {
			t.Errorf("OpenAPI protocol endpoint list does not document %s", name)
		}
	}
}
