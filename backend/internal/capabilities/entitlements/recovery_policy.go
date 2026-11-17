package entitlements

import (
	"errors"
	"strconv"
	"time"
)

const RecoveryGraceDaysKey = "subscription_renewal_grace_days"
const MaximumRecoveryGraceDays = 3650

func ParseRecoveryGraceDays(value string) (time.Duration, error) {
	days, err := strconv.ParseInt(value, 10, 32)
	if err != nil || days < 0 || days > MaximumRecoveryGraceDays {
		return 0, errors.New("续费恢复窗口必须为0至3650之间的整数天")
	}
	return time.Duration(days) * 24 * time.Hour, nil
}
