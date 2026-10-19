package network

import "errors"

// An unknown result is a read/reconciliation obligation, never permission to
// repeat a configuration mutation. Keep the underlying cancellation error.
var ErrConfigurationOutcomeUnknown = errors.New("配置应用结果待核对；保留当前运行配置和凭据，暂停重复下发")

func ConfigurationOutcomeUnknown(cause error) error {
	return errors.Join(ErrConfigurationOutcomeUnknown, cause)
}
