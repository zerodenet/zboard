package network

// DeletionCheck describes current dependencies, not an authorization token.
// A mutation must repeat this check inside its locking transaction.
type DeletionCheck struct {
	ID             uint              `json:"id"`
	Name           string            `json:"name"`
	AlreadyDeleted bool              `json:"already_deleted"`
	Blockers       []DeletionBlocker `json:"blockers"`
	Warnings       []DeletionBlocker `json:"warnings"`
	Effects        []string          `json:"effects"`
}
type DeletionBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Href    string `json:"href,omitempty"`
}
type DeletionBlocked struct {
	Check DeletionCheck
	Cause error
}

func (e *DeletionBlocked) Error() string {
	return "删除前置校验未通过，请先处理列出的依赖。"
}
func (e *DeletionBlocked) Unwrap() error { return e.Cause }
