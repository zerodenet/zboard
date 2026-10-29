package handler

import (
	"reflect"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/testsupport/mysqlseeds"
)

func TestMySQLBusinessOwnedSingletonCaptureRejectsActiveStateWithoutClearing(t *testing.T) {
	for _, table := range []string{"node_observation_material_budget", "principal_statistics_state", "flow_start_statistics_state"} {
		if businessSeedTables[table] {
			t.Fatal("strict singleton became whole-table seed whitelist", table)
		}
	}
	db := businessDatabaseForTest(t, mysqlBusinessConfig(t)).DB
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_observation_material_budget", "principal_statistics_state", "flow_start_statistics_state"} {
		columns, data, err := captureBusinessSeeds(db, table)
		if err != nil || len(data) != 1 {
			t.Fatal("real fresh initializer rejected", table, err)
		}
		if err := mysqlseeds.Validate(table, columns, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.NodeObservationMaterialBudget{}).Where("id=?", 1).Update("retained_bytes", uint64(19)).Error; err != nil {
		t.Fatal(err)
	}
	var beforeBudget model.NodeObservationMaterialBudget
	if err := db.First(&beforeBudget, 1).Error; err != nil || beforeBudget.RetainedBytes != 19 || beforeBudget.Revision != 1 {
		t.Fatal("material budget fixture did not advance", beforeBudget, err)
	}
	if _, _, err := captureBusinessSeeds(db, "node_observation_material_budget"); err == nil {
		t.Fatal("active material budget accepted as initializer")
	}
	var budget model.NodeObservationMaterialBudget
	if err := db.First(&budget, 1).Error; err != nil || !reflect.DeepEqual(beforeBudget, budget) {
		t.Fatal("rejected capture changed material authority", budget, err)
	}
	var state model.PrincipalStatisticsState
	if err := db.First(&state, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PrincipalStatisticsState{}).Where("id=?", 1).Update("projected_head", uint64(7)).Error; err != nil {
		t.Fatal(err)
	}
	// Snapshot after the deliberate mutation: GORM also updates UpdatedAt.
	// Every field must then remain unchanged by the rejected capture itself.
	if err := db.First(&state, 1).Error; err != nil || state.ProjectedHead != 7 {
		t.Fatal("projection fixture did not advance", state, err)
	}
	if _, _, err := captureBusinessSeeds(db, "principal_statistics_state"); err == nil {
		t.Fatal("progressed projection accepted as empty initializer")
	}
	var retained model.PrincipalStatisticsState
	if err := db.First(&retained, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, retained) {
		t.Fatal("rejected capture modified projection state", state, retained)
	}
	var flowState model.FlowStartStatisticsState
	if err := db.First(&flowState, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.FlowStartStatisticsState{}).Where("id=?", 1).Update("projected_head", uint64(9)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&flowState, 1).Error; err != nil || flowState.ProjectedHead != 9 {
		t.Fatal("flow statistics fixture did not advance", flowState, err)
	}
	if _, _, err := captureBusinessSeeds(db, "flow_start_statistics_state"); err == nil {
		t.Fatal("progressed flow statistics accepted as empty initializer")
	}
	var flowRetained model.FlowStartStatisticsState
	if err := db.First(&flowRetained, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flowState, flowRetained) {
		t.Fatal("rejected capture modified flow state")
	}
	if err := db.Create(&model.User{ID: 901, Email: "not-seed@example.test", Password: "unused", Status: userStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureBusinessSeeds(db, "users"); err == nil {
		t.Fatal("nonowned business row accepted")
	}
	var users int64
	if err := db.Model(&model.User{}).Count(&users).Error; err != nil || users != 1 {
		t.Fatal("rejection cleared nonowned user", users, err)
	}
}
