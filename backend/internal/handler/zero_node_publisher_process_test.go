package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
)

// This helper is executed in a separate OS process only by the isolated runner.
// It deliberately has no request producer: startup must find persisted work.
func TestRealZeroPublisherProcess(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_ZERO_PUBLISHER_CHILD") != "1" {
		t.Skip("isolated recovery subprocess only")
	}
	realZeroAddresses(t)
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, os.Getenv("ZBOARD_TEST_ZERO_PUBLISHER_DB"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	h, err := NewHandlers(db, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	h.StartNodePublishWorker()
	defer h.CloseNodePublishWorker()
	select {} // The parent intentionally crashes this process, bypassing defers.
}

func startRealZeroPublisherProcess(t *testing.T, f orderFixture, name string) (int, func()) {
	t.Helper()
	var databases []struct{ Name, File string }
	if err := f.h.db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, db := range databases {
		if db.Name == "main" {
			path = db.File
		}
	}
	if path == "" {
		t.Fatal("recovery requires a persistent SQLite file")
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(filepath.Dir(os.Getenv("ZBOARD_TEST_ZERO_SSH_KEY")), name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-test.run", "^TestRealZeroPublisherProcess$", "-test.v", "-test.timeout", "3m")
	cmd.Env = append(os.Environ(), "ZBOARD_TEST_ZERO_PUBLISHER_CHILD=1", "ZBOARD_TEST_ZERO_PUBLISHER_DB="+path)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		t.Helper()
		if stopped {
			return
		}
		stopped = true
		killErr := cmd.Process.Kill()
		waitErr := cmd.Wait()
		log.Close()
		if killErr != nil || waitErr == nil || cmd.ProcessState == nil || cmd.ProcessState.Exited() {
			t.Errorf("publisher was not terminated by the requested crash: kill=%v wait=%v", killErr, waitErr)
		} else {
			t.Logf("publisher crash confirmed: pid=%d state=%s", cmd.Process.Pid, cmd.ProcessState)
		}
	}
	t.Cleanup(stop)
	return cmd.Process.Pid, stop
}
