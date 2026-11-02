package network

import "errors"

// An unknown result is a read/reconciliation obligation, never permission to
// repeat a configuration mutation. Keep the underlying cancellation error.
var ErrConfigurationOutcomeUnknown = errors.New("配置应用结果待核对；保留当前运行配置和凭据，暂停重复下发")

// A former acknowledgement no longer belongs to this runtime. Re-enter the
// configuration phase through the owned queue generation, never inline while
// verifying existing connections.
var ErrConfigurationReapplyRequired = errors.New("节点运行实例已变化，需重新核对并同步当前配置后再验证连接")

func ConfigurationOutcomeUnknown(cause error) error {
	return errors.Join(ErrConfigurationOutcomeUnknown, cause)
}

// Acknowledging configuration completes configuration work only; a separate
// binary upgrade/repair recommendation must remain visible.
func ConfirmedConfigurationAction(action string) string {
	if action == "reconcile_configuration" || action == "configure" {
		return "none"
	}
	return action
}
