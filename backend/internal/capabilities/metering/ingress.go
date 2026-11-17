package metering

import (
	"errors"
	"strconv"
	"strings"
)

var ErrFlowIngressMismatch = errors.New("flow ingress does not match its authenticated credential")

// IngressEndpoint identifies the panel-owned inbound spelling. An absent or
// historical custom tag cannot supply endpoint authority on its own.
func IngressEndpoint(tag string) (uint, bool) {
	if !strings.HasPrefix(tag, "endpoint-") {
		return 0, false
	}
	raw := strings.TrimPrefix(tag, "endpoint-")
	id, err := strconv.ParseUint(raw, 10, strconv.IntSize)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		return 0, false
	}
	return uint(id), true
}

// Legacy events can still resolve through their endpoint-owned credential.
// When an explicit managed ingress is present it cannot contradict that owner.
func ValidateCredentialIngress(tag string, endpoint uint) error {
	if id, managed := IngressEndpoint(tag); managed && id != endpoint {
		return ErrFlowIngressMismatch
	}
	return nil
}
