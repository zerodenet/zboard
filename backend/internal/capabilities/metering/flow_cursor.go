package metering

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const RuntimeFlowUsagePrefix = "v2:"

func FlowUsageKey(instance, flow string) string {
	flow = strings.TrimSpace(flow)
	if flow == "" {
		return ""
	}
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return flow
	}
	digest := sha256.Sum256([]byte(instance + "\x00" + flow))
	return RuntimeFlowUsagePrefix + hex.EncodeToString(digest[:])
}
func RuntimeScopedFlow(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), RuntimeFlowUsagePrefix)
}

type FlowCursor struct {
	CredentialID uint
	Revision     uint64
	Status       string
	Counters     FlowCounters
}

func ContinuesLegacyFlow(cursor FlowCursor, credential uint, revision uint64, current FlowCounters) bool {
	return cursor.Status == "active" && cursor.CredentialID == credential && cursor.Counters.Raw <= current.Raw && cursor.Counters.Upload <= current.Upload && cursor.Counters.Download <= current.Download && (revision == 0 || cursor.Revision <= revision)
}
