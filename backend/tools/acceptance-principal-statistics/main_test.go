package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestIsolatedConfigRejectsBusinessSchemaRemoteAliasAndMultipleStatements(t *testing.T) {
	for _, dsn := range []string{"tester:secret@tcp(127.0.0.1:3306)/business", "tester:secret@tcp(192.0.2.1:3306)/", "tester:secret@tcp(localhost:3306)/", "tester:secret@unix(/tmp/mysql.sock)/", "tester:secret@tcp(127.0.0.1:3306)/?multiStatements=true", "not-a-dsn"} {
		cfg, err := isolatedConfig(dsn)
		if err == nil || cfg != nil {
			t.Fatal("unsafe connection admitted", dsn)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("credential exposed in admission error")
		}
	}
	for _, dsn := range []string{"tester:secret@tcp(127.0.0.1:3306)/", "tester:secret@tcp([::1]:3306)/"} {
		cfg, err := isolatedConfig(dsn)
		if err != nil || cfg == nil || cfg.DBName != "" {
			t.Fatal("valid explicit loopback rejected", err)
		}
	}
}
func TestSameFactsIgnoresPhysicalAllocationAndRetirementMarkersOnly(t *testing.T) {
	a := snapshot{StatisticsState: "complete", PointsSHA: "point", HourlySHA: "hour", TrendsSHA: "trend", Counts: map[string]int64{"point": 1000, "hour": 32, "raw": 1000}, Physical: map[string]any{"pages": 100}}
	b := snapshot{StatisticsState: "complete", PointsSHA: "point", HourlySHA: "hour", TrendsSHA: "trend", Counts: map[string]int64{"point": 1000, "hour": 32, "raw": 2}, Physical: map[string]any{"pages": 100}}
	if !sameFacts(a, b) {
		t.Fatal("logical retention incorrectly requires physical file shrink")
	}
	b.PointsSHA = "changed"
	if sameFacts(a, b) {
		t.Fatal("lost/changed thin evidence accepted")
	}
	b.PointsSHA = a.PointsSHA
	b.Counts["point"]--
	if sameFacts(a, b) {
		t.Fatal("point loss accepted")
	}
}

func TestBuildProvenanceRejectsOldDirtyUnstampedAndWrongModule(t *testing.T) {
	head := strings.Repeat("a", 40)
	good := func() *debug.BuildInfo {
		return &debug.BuildInfo{GoVersion: "go1.26.8", Path: "github.com/zerodenet/zboard/backend/tools/acceptance-principal-statistics", Main: debug.Module{Path: "github.com/zerodenet/zboard/backend"}, Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: head}, {Key: "vcs.modified", Value: "false"}}}
	}
	if err := validateBuild(good(), head); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*debug.BuildInfo){"wrong_go": func(i *debug.BuildInfo) { i.GoVersion = "go1.26.9" }, "old": func(i *debug.BuildInfo) { i.Settings[1].Value = strings.Repeat("b", 40) }, "dirty": func(i *debug.BuildInfo) { i.Settings[2].Value = "true" }, "unstamped": func(i *debug.BuildInfo) { i.Settings = nil }, "foreign_module": func(i *debug.BuildInfo) { i.Main.Path = "elsewhere" }, "foreign_program": func(i *debug.BuildInfo) { i.Path = "different-program" }} {
		info := good()
		mutate(info)
		if err := validateBuild(info, head); err == nil {
			t.Fatal("unproved binary accepted", name)
		}
	}
	if err := validateBuild(nil, head); err == nil {
		t.Fatal("missing buildinfo accepted")
	}
}

func TestSameFactsNeverCertifiesMatchingPendingUnknownSnapshots(t *testing.T) {
	a := snapshot{StatisticsState: "pending", PointsSHA: "same", HourlySHA: "same", TrendsSHA: "unknown", Counts: map[string]int64{"point": 0, "hour": 0}}
	b := a
	if sameFacts(a, b) {
		t.Fatal("pending/unknown observations certified as complete facts")
	}
}
