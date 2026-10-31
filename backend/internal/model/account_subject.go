package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AccountSubject is an immutable numeric accounting identity, not an account.
// It contains no login, password, contact information or account status. Its ID
// survives physical account removal so accounting and late reports stay valid.
type AccountSubject struct {
	ID uint `gorm:"primaryKey;autoIncrement:true"`
}

// One bounded scan cursor for the opt-in cleanup job, independent of history.
type AccountCleanupState struct {
	ID         uint `gorm:"primaryKey;autoIncrement:false"`
	LastUserID uint `gorm:"not null;default:0"`
}

// Allocation is part of the same account-creation transaction on both engines.
// No database trigger or global MySQL privilege is required.
// Minimal identity-only schemas must include AccountSubject alongside User.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID != 0 {
		return nil
	}
	subject := AccountSubject{}
	if err := tx.Create(&subject).Error; err != nil {
		return err
	}
	u.ID = subject.ID
	return nil
}

func (u *User) AfterCreate(tx *gorm.DB) error {
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&AccountSubject{ID: u.ID}).Error
}
