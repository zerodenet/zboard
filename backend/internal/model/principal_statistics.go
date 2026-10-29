package model

import "time"

// Legacy bounds are captured only while old writers are stopped. New sources
// own a same-transaction pending row, so an out-of-order commit cannot be lost
// merely because another source has a greater auto-increment ID.
type PrincipalStatisticsState struct {
	ID                  uint `gorm:"primaryKey;autoIncrement:false"`
	LegacyUpperID       uint64
	LegacyAfterID       uint64
	LegacyExpectedRows  uint64
	LegacyProjectedRows uint64
	ProjectedHead       uint64
	Revision            uint64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (PrincipalStatisticsState) TableName() string { return "principal_statistics_state" }

type PrincipalStatisticsPending struct {
	ObservationID uint64 `gorm:"primaryKey;autoIncrement:false"`
	ScopeType     string
	ScopeID       uint
	ObservedAt    time.Time
	CreatedAt     time.Time
}

func (PrincipalStatisticsPending) TableName() string { return "principal_statistics_pending" }

// This is thin numeric and bounded identity/replay evidence, not the raw core
// diagnostic body. Identifiers retain the source database contract.
// Exact observation times preserve arbitrary calendar boundary slices forever;
// complete UTC hours below accelerate their interior. Rows remain linear in
// accepted scope events; no whole-database constant-size promise is implied.
type PrincipalStatisticsSample struct {
	ObservationID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	ScopeType       string
	ScopeID         uint
	EventID         string
	ObservedAt      time.Time
	ActiveFlows     uint64
	SourceDigest    string
	SourceCreatedAt time.Time
	ReviewAt        time.Time
	RawRetiredAt    *time.Time
}

func (PrincipalStatisticsSample) TableName() string { return "principal_statistics_samples" }

type PrincipalStatisticsHourly struct {
	ScopeType   string    `gorm:"primaryKey"`
	ScopeID     uint      `gorm:"primaryKey;autoIncrement:false"`
	HourAt      time.Time `gorm:"primaryKey"`
	Peak        uint64
	SampleCount uint64
}

func (PrincipalStatisticsHourly) TableName() string { return "principal_statistics_hourly" }
