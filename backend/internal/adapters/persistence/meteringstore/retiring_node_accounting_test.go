package meteringstore

import (
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRetiringNodeCannotRecreateAccountingAfterCursorCleanup(t *testing.T) {
	db, credential := cohortDB(t)
	if err := db.Model(&model.Node{}).Where("id = 1").Update("is_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Ordinary disabling must not discard late settlement.
	if err := applyCohort(db, []metering.FlowSample{cohortSample(credential, "flow", "core", 1, 5)}); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 5)
	if err := db.Model(&model.Node{}).Where("id = 1").Update("lifecycle_status", "deleting").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("node_id = 1").Delete(&model.FlowUsage{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := applyCohort(db, []metering.FlowSample{cohortSample(credential, "flow", "core", 2, 10)}); !errors.Is(err, network.ErrNodeAdministrationDeleting) {
		t.Fatal("retiring cursor was recreated", err)
	}
	cohortTotal(t, db, 5)
	var count int64
	if err := db.Model(&model.FlowUsage{}).Where("node_id = 1").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("retiring node regained a cumulative cursor", count, err)
	}
}
