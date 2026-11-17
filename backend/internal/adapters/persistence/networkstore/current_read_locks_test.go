package networkstore

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestCurrentResourceReadClassifiesOnlyImmediateLockConflict(t *testing.T) {
	if got := currentResourceReadError(fmt.Errorf("read topology: %w", &mysql.MySQLError{Number: 3572, Message: "NOWAIT lock"})); !errors.Is(got, network.ErrResourcesBusy) {
		t.Fatal("NOWAIT conflict not exposed", got)
	}
	for _, code := range []uint16{1213, 1205, 1062} {
		failure := &mysql.MySQLError{Number: code, Message: "preserve original failure"}
		if got := currentResourceReadError(failure); got != failure || errors.Is(got, network.ErrResourcesBusy) {
			t.Fatalf("non-NOWAIT error %d disguised: %v", code, got)
		}
	}
	if currentResourceReadError(nil) != nil {
		t.Fatal("successful read became a conflict")
	}
}
