package jobs

import "time"

type BatchSubmissionActor struct {
	ID    uint
	Email string
}

type BatchSubmissionTarget struct {
	Type string
	ID   uint
}

type BatchSubmission struct {
	Type           string
	Scope          string
	Content        string
	IdempotencyKey string
	MaxAttempts    int
	ScheduledAt    time.Time
	Targets        []BatchSubmissionTarget
}

type BatchSubmissionReceipt struct {
	BatchReceipt
}
