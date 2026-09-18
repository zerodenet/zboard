package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestTaskDeclarationsRequireCapabilityAndBounds(t *testing.T) {
	m := Manifest{Capabilities: []string{ConfigCapability, TaskCapability}}
	m.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{"host": "plugin"}}
	m.Contributions.Tasks = []TaskDefinition{{ID: "sync", Title: "同步", IntervalSeconds: 60, TimeoutSeconds: 20}}
	if err := m.validateTasks(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []TaskDefinition{{ID: "../escape", Title: "x", IntervalSeconds: 60, TimeoutSeconds: 20}, {ID: "sync", Title: "x", IntervalSeconds: 1, TimeoutSeconds: 20}, {ID: "sync", Title: "x", IntervalSeconds: 60, TimeoutSeconds: 301}} {
		m.Contributions.Tasks = []TaskDefinition{bad}
		if m.validateTasks() == nil {
			t.Fatal("invalid task accepted", bad)
		}
	}
	m.Contributions.Tasks = []TaskDefinition{{ID: "sync", Title: "x", IntervalSeconds: 60, TimeoutSeconds: 20}}
	m.Capabilities = []string{ConfigCapability}
	if m.validateTasks() == nil {
		t.Fatal("missing capability accepted")
	}
}
func TestRealPluginTaskLifecycleAndObservation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "tasks")
	cmd := exec.Command("go", "build", "-o", binary, "./testdata/tasks")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	upgradedBinary := filepath.Join(t.TempDir(), "tasks-upgraded")
	cmd = exec.Command("go", "build", "-ldflags=-X main.pluginVersion=1.0.1", "-o", upgradedBinary, "./testdata/tasks")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build upgraded: %v %s", err, out)
	}
	upgradedPayload, err := os.ReadFile(upgradedBinary)
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[string][]byte{"1.0.0": payload, "1.0.1": upgradedPayload}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	taskPackage := func(version string) []byte {
		return fixtureSignedPackage(t, priv, pub, func(m *Manifest, files map[string][]byte) {
			m.ID = "example.tasks"
			m.Version = version
			m.Capabilities = []string{ConfigCapability, TaskCapability}
			m.Surfaces = nil
			m.Contributions.Pages = nil
			m.Components.UI = nil
			m.Components.Server = &struct {
				Executables map[string]string `json:"executables"`
			}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
			m.Contributions.Tasks = []TaskDefinition{{ID: "slow", Title: "升级中断任务", IntervalSeconds: 60, TimeoutSeconds: 5}, {ID: "slow-disable", Title: "停用中断任务", IntervalSeconds: 60, TimeoutSeconds: 5}, {ID: "success", Title: "成功任务", IntervalSeconds: 60, TimeoutSeconds: 5}, {ID: "failure", Title: "失败任务", IntervalSeconds: 60, TimeoutSeconds: 5}}
			m.Contributions.Tasks = append(m.Contributions.Tasks, TaskDefinition{ID: "host", Title: "宿主任务入口", IntervalSeconds: 60, TimeoutSeconds: 5})
			files["runtimes/host/plugin"] = payloads[version]
		})
	}
	raw := taskPackage("1.0.0")
	m, _, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	views, err := m.TaskSnapshots(context.Background())
	if err != nil || len(views) != 5 || views[0].State != "disabled" {
		t.Fatal("disabled tasks missing", views, err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "test", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}

	verifyDurableTaskBridge(t, m)
	rt := jobs.NewRuntime(jobstore.New(m.db), "registered-test", nil, func(err error) { t.Log(err) })
	t.Cleanup(rt.Close)
	m.SetTaskRuntime(rt)
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("task state did not converge")
	}
	wait(func() bool {
		var n int64
		m.db.Model(&jobstore.Schedule{}).Where("owner = ?", "plugin:example.tasks").Count(&n)
		return n == 5
	})
	due := func(id string) {
		t.Helper()
		if err := m.db.Model(&jobstore.Schedule{}).Where("id = ?", "plugin:example.tasks:"+id).Update("next_at", time.Now().Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
	}
	result := func(id string, state jobs.State) bool {
		rows, err := m.TaskSnapshots(context.Background())
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.TaskID == id {
				return row.LastResult == string(state)
			}
		}
		return false
	}
	due("success")
	wait(func() bool { return result("success", jobs.Succeeded) })
	due("host")
	wait(func() bool { return result("host", jobs.Succeeded) })
	due("failure")
	wait(func() bool { return result("failure", jobs.Failed) })
	due("slow-disable")
	wait(func() bool {
		rows, err := m.TaskSnapshots(context.Background())
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.TaskID == "slow-disable" {
				return row.Running == 1
			}
		}
		return false
	})
	v, err = m.Import(taskPackage("1.0.1"), "test-upgrade")
	if err != nil {
		t.Fatal(err)
	}
	m.syncTaskRegistrations(context.Background())
	wait(func() bool {
		var row jobstore.Record
		err := m.db.Where("handler = ?", "plugin:example.tasks:slow-disable").Order("created_at DESC").First(&row).Error
		return err == nil && row.State == string(jobs.Unknown)
	})
	var interrupted jobstore.Record
	if err := m.db.Where("handler = ?", "plugin:example.tasks:slow-disable").Order("created_at DESC").First(&interrupted).Error; err != nil {
		t.Fatal(err)
	}
	if err := jobstore.New(m.db).Resolve(context.Background(), interrupted.ID, jobs.Failed); err != nil {
		t.Fatal("operator resolution did not release upgraded plugin resource", err)
	}
	if v.Version != "1.0.1" || !v.Enabled || v.State != "active" {
		t.Fatal("upgrade did not replace the active plugin", v)
	}
	due("success")
	wait(func() bool { return result("success", jobs.Succeeded) })
	due("slow")
	wait(func() bool {
		rows, err := m.TaskSnapshots(context.Background())
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.TaskID == "slow" {
				return row.Running == 1
			}
		}
		return false
	})
	v, err = m.Action(context.Background(), v.ID, "disable", "test", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	m.syncTaskRegistrations(context.Background())
	wait(func() bool {
		var row jobstore.Record
		err := m.db.Where("handler = ?", "plugin:example.tasks:slow").Order("created_at DESC").First(&row).Error
		return err == nil && row.State == string(jobs.Unknown)
	})
	views, err = m.TaskSnapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.State != "disabled" {
			t.Fatal(view)
		}
	}
}

func verifyDurableTaskBridge(t *testing.T, m *Manager) {
	t.Helper()
	ctx := context.Background()
	if err := m.db.AutoMigrate(&jobstore.Budget{}, &jobstore.Record{}, &jobstore.Attempt{}, &jobstore.Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := m.db.Model(&jobstore.Budget{}).Where("id = 1").Update("capacity", 1).Error; err != nil {
		t.Fatal(err)
	}
	handler, err := m.TaskExecutor(ctx, "example.tasks", "success")
	if err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(m.db)
	for _, in := range []jobs.Submission{
		{Owner: "plugin:example.tasks", Key: "durable-plugin", Handler: "plugin:example.tasks:success", Resource: "plugin:example.tasks", Payload: `{}`},
		{Owner: "system", Key: "durable-core", Handler: "core.fixture", Payload: `{}`},
	} {
		if _, err := store.Submit(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	coreCalled := false
	executor := jobs.Executor{Store: jobstore.New(m.db), Worker: "native-bridge-test", Timeout: 10 * time.Second, Handlers: map[string]jobs.Handler{
		"plugin:example.tasks:success": handler,
		"core.fixture":                 func(context.Context, jobs.Run) error { coreCalled = true; return nil },
	}}
	for i := 0; i < 2; i++ {
		if err := executor.RunOne(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !coreCalled {
		t.Fatal("core executor not invoked")
	}
	for _, owner := range []string{"system", "plugin:example.tasks"} {
		rows, err := jobstore.New(m.db).List(ctx, owner, 25, 0)
		if err != nil || len(rows) != 1 || rows[0].State != jobs.Succeeded {
			t.Fatal(rows, err)
		}
	}
	if err := handler(ctx, jobs.Run{Submission: jobs.Submission{Owner: "plugin:other", Handler: "plugin:example.tasks:success"}}); err != ErrPermission {
		t.Fatal("foreign owner allowed", err)
	}
}
