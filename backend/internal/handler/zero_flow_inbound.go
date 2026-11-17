package handler

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Both active and final Zero payloads have existed in flat and record-wrapped
// forms. Preserve their exact ingress identity when reducing a durable event;
// historical events without one remain eligible for legacy attribution.
func zeroFlowInboundTag(payload, record map[string]interface{}) (string, error) {
	var tag string
	for _, object := range []map[string]interface{}{payload, record} {
		values := []interface{}{object["inbound_tag"]}
		if inbound, ok := object["inbound"].(map[string]interface{}); ok {
			values = append(values, inbound["tag"])
		}
		for _, value := range values {
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok || !utf8.ValidString(text) || len(text) > 128 {
				return "", errors.New("Zero flow event has invalid inbound identity")
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if tag != "" && tag != text {
				return "", errors.New("Zero flow event has conflicting inbound identities")
			}
			tag = text
		}
	}
	return tag, nil
}
