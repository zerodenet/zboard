package metering

import "errors"

const ReportReceiptBackfillLimit = 200

type ReportReceiptBackfillResult struct {
	AfterID            uint `json:"after_id"`
	ThroughID          uint `json:"through_id"`
	NextAfterID        uint `json:"next_after_id"`
	Scanned            int  `json:"scanned"`
	IdentitiesVerified int  `json:"identities_verified"`
	Unidentified       int  `json:"unidentified"`
	RangeExhausted     bool `json:"range_exhausted"`
}

func ValidateReportReceiptBackfill(after, through uint, limit int) error {
	if through == 0 || after >= through || limit < 1 || limit > ReportReceiptBackfillLimit {
		return errors.New("report receipt backfill requires an explicit increasing ID range and a batch between 1 and 200")
	}
	return nil
}
