package commerce

import (
	"errors"
	"testing"
)

func TestPlanPatchPreservesOmittedPolicyAndNormalizesIdentity(t *testing.T) {
	current := Plan{ID: 9, Name: "Original", Slug: "original", Revision: 4, TrafficBytes: 1024, DeviceLimit: 3, IsRenewable: true, IsActive: true}
	name, slug, description := " Edited ", " MIXED-Slug ", " "
	zero := 0
	no := false
	next, _, err := ApplyPlanUpdate(current, PlanUpdateRequest{Name: &name, Slug: &slug, Description: &description, SortOrder: &zero, IsRenewable: &no, ExpectedRevision: &current.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if next.Name != "Edited" || next.Slug != "mixed-slug" || next.Description != "" || next.IsRenewable || next.TrafficBytes != 1024 || next.DeviceLimit != 3 || !next.IsActive || next.Revision != 5 {
		t.Fatalf("patch semantics: %+v", next)
	}
	if current.Name != "Original" || !current.IsRenewable || name != " Edited " {
		t.Fatal("patch mutated input")
	}
}
func TestPlanPatchRejectsInvalidPolicyAndAbsentChanges(t *testing.T) {
	current := Plan{Revision: 1}
	zero := int64(0)
	negative := -1
	mode := int16(3)
	empty := " "
	for _, test := range []struct {
		request PlanUpdateRequest
		field   string
	}{
		{PlanUpdateRequest{TrafficBytes: &zero}, "traffic_bytes"},
		{PlanUpdateRequest{DeviceLimit: &negative}, "device_limit"},
		{PlanUpdateRequest{SpeedLimitMbps: &negative}, "speed_limit_mbps"},
		{PlanUpdateRequest{TrafficCalcMode: &mode}, "traffic_calc_mode"},
		{PlanUpdateRequest{Name: &empty}, "name"},
		{PlanUpdateRequest{}, ""},
	} {
		test.request.ExpectedRevision = &current.Revision
		_, _, err := ApplyPlanUpdate(current, test.request)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || (test.field != "" && invalid.Fields[test.field] == "") {
			t.Fatalf("%s validation: %v", test.field, err)
		}
	}
}
