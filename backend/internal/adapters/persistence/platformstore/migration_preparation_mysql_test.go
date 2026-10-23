package platformstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMySQLMigrationCopierLocksEmptyTargetRangesBeforeClear(t *testing.T) {
	f := newMigrationCopyFixture(t, false, true)
	receipt := failMigrationCopy(t, f)
	destination, writer := f.destination(t), f.destination(t)
	if err := writer.Exec("SET SESSION innodb_lock_wait_timeout = 1").Error; err != nil {
		t.Fatal(err)
	}
	err := datastore.WithCopyDestination(context.Background(), destination, func(tx *gorm.DB) error {
		if err := guardMigrationPreparation(tx, receipt); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() {
			done <- writer.Create(&model.User{Email: "late-external-writer@example.test", Password: "preserve", Status: "active"}).Error
		}()
		select {
		case err := <-done:
			var mysqlErr *mysqlDriver.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1205 {
				return fmt.Errorf("expected empty-range lock timeout, got %v", err)
			}
		case <-time.After(4 * time.Second):
			return errors.New("external writer did not return within its lock timeout")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Create(&model.User{Email: "late-external-writer@example.test", Password: "preserve", Status: "active"}).Error; err != nil {
		t.Fatal("guard did not release its range locks", err)
	}
	if err := verifyPreparationManifest(destination, receipt, false); err == nil {
		t.Fatal("external data committed after guard was ignored")
	}
	assertMigrationCopySourcePreserved(t, f)
}

func TestMySQLMigrationCopierRejectsReadCommittedBeforeClear(t *testing.T) {
	f := newMigrationCopyFixture(t, false, true)
	receipt := failMigrationCopy(t, f)
	destination := f.destination(t)
	if err := destination.Exec("SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED").Error; err != nil {
		t.Fatal(err)
	}
	err := datastore.WithCopyDestination(context.Background(), destination, func(tx *gorm.DB) error { return guardMigrationPreparation(tx, receipt) })
	if err == nil || !strings.Contains(err.Error(), "requires REPEATABLE READ or SERIALIZABLE") {
		t.Fatal("unprotected empty ranges were accepted", err)
	}
	if err := destination.Exec("SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; err != nil {
		t.Fatal(err)
	}
	if err := verifyPreparationManifest(destination, receipt, false); err != nil {
		t.Fatal("unsupported isolation changed destination", err)
	}
	assertMigrationCopySourcePreserved(t, f)
}
