package metering

import "time"

const MinuteDetailHorizon = 24 * time.Hour
const HourlyHistoryHorizon = 30 * 24 * time.Hour
const DailyHistoryHorizon = 365 * 24 * time.Hour

// HistoryCoverage describes display precision, never the accounting lifetime.
// Expired graph history does not mean zero settled usage.
type HistoryCoverage struct {
	Expired      bool      `json:"expired"`
	Timezone     string    `json:"timezone,omitempty"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	RetainedFrom time.Time `json:"retained_from"`
	Bucket       string    `json:"bucket"`
	Truncated    bool      `json:"truncated"`
}
