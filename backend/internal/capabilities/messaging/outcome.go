package messaging

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

var ErrAcceptanceUnknown = errors.New("邮件接收结果待核验；确认是否已投递后再重试")

// Acceptance describes provider acknowledgement, never mailbox delivery/read.
type Acceptance string

const (
	Accepted          Acceptance = "accepted"
	NotAccepted       Acceptance = "not_accepted"
	AcceptanceUnknown Acceptance = "unknown"
)

func AcceptanceFor(err error) Acceptance {
	if err == nil {
		return Accepted
	}
	if errors.Is(err, ErrAcceptanceUnknown) || errors.Is(err, jobs.ErrLeaseLost) {
		return AcceptanceUnknown
	}
	return NotAccepted
}
