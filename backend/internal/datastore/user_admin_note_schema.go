package datastore

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const userAdminNoteVersion = "0080_user_admin_notes.up.sql"

// A single additive column. Resume safely if MySQL committed the DDL before
// its migration journal could be written; never replay or replace user data.
func reconcileUserAdminNoteSchema(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&model.User{}, "AdminNote") {
		if err := db.Migrator().AddColumn(&model.User{}, "AdminNote"); err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: userAdminNoteVersion, AppliedAt: time.Now().UTC()}).Error
}
