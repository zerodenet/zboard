package entitlements

import (
	"errors"
	"time"
)

var PerpetualEnd = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

func NextTrafficReset(base time.Time, policy int16) *time.Time {
	base = base.UTC()
	var next time.Time
	switch policy {
	case 1:
		next = time.Date(base.Year(), base.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	case 2:
		next = AddCalendarMonths(base, 1)
	case 3:
		next = time.Date(base.Year()+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	case 4:
		next = base.AddDate(1, 0, 0)
	default:
		return nil
	}
	return &next
}

func EffectiveResetPolicy(billingUnit string, planPolicy int16) int16 {
	if billingUnit == "once" {
		return 5
	}
	return planPolicy
}

func IsPerpetualEnd(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= PerpetualEnd.Year()
}

func AddBillingPeriod(base time.Time, unit string, value int) (time.Time, error) {
	if value <= 0 {
		return time.Time{}, errors.New("billing value must be positive")
	}
	switch unit {
	case "day":
		return base.AddDate(0, 0, value), nil
	case "month":
		return AddCalendarMonths(base, value), nil
	case "year":
		return AddCalendarMonths(base, value*12), nil
	case "once":
		return PerpetualEnd, nil
	default:
		return time.Time{}, errors.New("unsupported billing unit")
	}
}

func AddCalendarMonths(base time.Time, months int) time.Time {
	monthIndex := int(base.Month()) - 1 + months
	year := base.Year() + monthIndex/12
	monthIndex %= 12
	if monthIndex < 0 {
		monthIndex += 12
		year--
	}
	month := time.Month(monthIndex + 1)
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, base.Location()).Day()
	day := base.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), base.Location())
}
