package datastore

import (
	"context"

	"gorm.io/gorm"
)

type migrationPreparationProofKey struct{}
type migrationPreparationProof struct{ verify func(*gorm.DB) error }

// WithOwnedMigrationPreparation carries an in-process ownership check for the
// destination-local preparation receipt. It does not make initialized databases
// eligible for adoption: fresh baseline admission still requires that this one
// receipt is the only table besides schema_migrations.
//
// The caller must verify the receipt's random token, source task and actual
// destination identity. HTTP parameters or a table name cannot supply proof.
func WithOwnedMigrationPreparation(db *gorm.DB, verify func(*gorm.DB) error) *gorm.DB {
	if db == nil || verify == nil {
		return db
	}
	return db.WithContext(context.WithValue(db.Statement.Context, migrationPreparationProofKey{}, migrationPreparationProof{verify: verify}))
}

func ownedMigrationPreparationChecks(db *gorm.DB) []func() error {
	proof, ok := db.Statement.Context.Value(migrationPreparationProofKey{}).(migrationPreparationProof)
	if !ok || proof.verify == nil {
		return nil
	}
	return []func() error{func() error { return proof.verify(db) }}
}
