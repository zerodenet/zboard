package meteringstore

import (
	"fmt"
	"time"
)

// Bucket SQL yields DATETIME on MySQL and text on SQLite. Keep the wire value
// a UTC timestamp on both drivers instead of relying on driver type inference.
type BucketTime struct{ time.Time }

func (t *BucketTime) Scan(value any) error {
	if stamp, ok := value.(time.Time); ok {
		t.Time = stamp.UTC()
		return nil
	}
	var raw string
	switch value := value.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("invalid traffic bucket timestamp type %T", value)
	}
	stamp, err := time.Parse("2006-01-02 15:04:05", raw)
	if err != nil {
		return err
	}
	t.Time = stamp.UTC()
	return nil
}

// GORM treats this scanner as a scalar time column, not an embedded model.
func (BucketTime) GormDataType() string { return "time" }
