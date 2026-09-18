package messaging

import "time"

type Template struct {
	ID              uint      `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Category        string    `json:"category"`
	TriggerKey      *string   `json:"trigger_key,omitempty"`
	SubjectTemplate string    `json:"subject_template"`
	BodyTemplate    string    `json:"body_template"`
	IsActive        bool      `json:"is_active"`
	SortOrder       int       `json:"sort_order"`
	Revision        uint64    `json:"revision"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
