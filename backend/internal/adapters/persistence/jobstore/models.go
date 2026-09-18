// Package jobstore implements the jobs port. Schema installation is owned by
// deployment migrations; constructing a Store never mutates the schema.
package jobstore

import "time"

type Budget struct {
	ID                  uint   `gorm:"primaryKey;autoIncrement:false"`
	Capacity            int    `gorm:"not null"`
	Revision            uint64 `gorm:"not null"`
	DispatchVirtualTime uint64 `gorm:"not null;default:0"`
}

func (Budget) TableName() string { return "job_execution_budget" }

type ExecutionGroup struct {
	ID       string `gorm:"primaryKey;size:160"`
	Capacity int    `gorm:"not null"`
}

func (ExecutionGroup) TableName() string { return "job_execution_groups" }

type DispatchLane struct {
	Lane          string `gorm:"primaryKey;size:200"`
	VirtualFinish uint64 `gorm:"not null"`
}

func (DispatchLane) TableName() string { return "job_dispatch_lanes" }

type Record struct {
	TimeoutMS      int64     `gorm:"not null;default:0"`
	AttemptCount   int       `gorm:"not null;default:0"`
	MaxAttempts    int       `gorm:"not null;default:1"`
	RetryBackoffMS int64     `gorm:"not null;default:0"`
	ExecutionGroup string    `gorm:"size:160;not null;default:'';index"`
	DispatchLane   string    `gorm:"size:200;not null;default:'core';index:job_dispatch_ready,priority:1"`
	ID             string    `gorm:"primaryKey;size:36"`
	Owner          string    `gorm:"size:160;not null;uniqueIndex:job_intent,priority:1"`
	Key            string    `gorm:"size:160;not null;uniqueIndex:job_intent,priority:2"`
	Handler        string    `gorm:"size:160;not null"`
	Resource       string    `gorm:"size:160;not null;index"`
	Payload        string    `gorm:"type:text;not null"`
	Fingerprint    string    `gorm:"size:64;not null"`
	State          string    `gorm:"size:24;not null;index:job_ready,priority:1;index:job_dispatch_ready,priority:2"`
	NotBefore      time.Time `gorm:"not null;index:job_ready,priority:2;index:job_dispatch_ready,priority:3"`
	CreatedAt      time.Time `gorm:"not null"`
	FinishedAt     *time.Time
	Token          string `gorm:"size:36;not null"`
	Worker         string `gorm:"size:160;not null"`
	ExpiresAt      *time.Time
	ScheduleID     string     `gorm:"size:320;not null;default:'';index;uniqueIndex:job_schedule_planned,priority:1"`
	PlannedAt      *time.Time `gorm:"uniqueIndex:job_schedule_planned,priority:2"`
}

func (Record) TableName() string { return "job_runs" }

type Attempt struct {
	Token      string    `gorm:"primaryKey;size:36"`
	RunID      string    `gorm:"size:36;not null;uniqueIndex:job_attempt_run_number,priority:1"`
	Number     int       `gorm:"column:attempt_number;not null;uniqueIndex:job_attempt_run_number,priority:2"`
	Worker     string    `gorm:"size:160;not null"`
	State      string    `gorm:"size:24;not null"`
	StartedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null"`
	FinishedAt *time.Time
}

func (Attempt) TableName() string { return "job_attempts" }

type Schedule struct {
	ID             string `gorm:"primaryKey;size:160"`
	Owner          string `gorm:"size:160;not null"`
	Name           string `gorm:"size:160;not null"`
	Handler        string `gorm:"size:160;not null"`
	Resource       string `gorm:"size:160;not null"`
	ExecutionGroup string `gorm:"size:160;not null;default:''"`
	Revision       string `gorm:"size:160;not null"`
	IntervalMS     int64  `gorm:"not null"`
	TimeoutMS      int64  `gorm:"not null"`
	MaxAttempts    int    `gorm:"not null;default:1"`
	RetryBackoffMS int64  `gorm:"not null;default:0"`
	TimeZone       string `gorm:"column:timezone;size:64;not null;default:'UTC'"`
	MisfirePolicy  string `gorm:"size:24;not null;default:'fire_once'"`
	DispatchLane   string `gorm:"size:200;not null;default:'core'"`
	AnchorAt       *time.Time
	NextAt         time.Time `gorm:"not null"`
	Sequence       uint64    `gorm:"not null"`
	RunID          string    `gorm:"size:36;not null"`
	Runs           uint64    `gorm:"not null"`
	Failures       uint64    `gorm:"not null"`
	MissedRuns     uint64    `gorm:"not null;default:0"`
	State          string    `gorm:"size:24;not null;default:''"`
	LastState      string    `gorm:"size:24;not null"`
	LastStartedAt  *time.Time
	LastFinishedAt *time.Time
}

func (Schedule) TableName() string { return "job_schedules" }
