package main

import "testing"

func TestObservationIntentOperationsRouteOwnership(t *testing.T) {
	if got := routeOwner("/api/v1/admin/node-observation-intents"); got != "resources" {
		t.Fatal(got)
	}
	if got := routeOwner("/api/v1/admin/node-observation-intents-other"); got != "unassigned" {
		t.Fatal("neighbor route silently acquired authority", got)
	}
}
