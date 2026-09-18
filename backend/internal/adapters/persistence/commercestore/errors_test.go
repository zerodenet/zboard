package commercestore

import (
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"gorm.io/gorm"
)

func TestIdentifierErrorsUseDriverClassification(t *testing.T) {
	for _, err := range []error{gorm.ErrDuplicatedKey, &mysql.MySQLError{Number: 1062, Message: "opaque diagnostic"}} {
		if !errors.Is(skuWriteError(err), commerce.ErrSKUCodeConflict) {
			t.Fatalf("unique classification: %v", err)
		}
	}
	for _, err := range []error{errors.New("duplicate entry in a diagnostic"), &mysql.MySQLError{Number: 1452, Message: "foreign key failure"}} {
		if skuWriteError(err) != err {
			t.Fatalf("non-unique failure reclassified: %v", err)
		}
	}
}

func TestPlanDuplicateConstraintIdentity(t *testing.T) {
	for _, test := range []struct {
		message string
		want    error
	}{
		{"Duplicate entry 'x' for key 'plans.name'", commerce.ErrPlanNameConflict},
		{"Duplicate entry 'x' for key 'name'", commerce.ErrPlanNameConflict},
		{"Duplicate entry 'x for key 'plans.name'' for key 'plans.uk_plans_slug'", commerce.ErrPlanSlugConflict},
	} {
		if err := planWriteError(&mysql.MySQLError{Number: 1062, Message: test.message}); !errors.Is(err, test.want) {
			t.Fatalf("constraint mapping: %v", err)
		}
	}
	unknown := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'unrecognized'"}
	if planWriteError(unknown) != unknown {
		t.Fatal("unknown constraint fabricated field conflict")
	}
}
